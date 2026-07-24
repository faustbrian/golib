// Package proxy derives effective request information only across explicitly
// trusted proxy address boundaries. It never mutates the request target.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Mode selects one forwarding syntax.
type Mode uint8

const (
	// Forwarded selects the RFC 7239 Forwarded field.
	Forwarded Mode = iota
	// XForwarded selects explicitly configured X-Forwarded fields.
	XForwarded
)

// Provenance describes whether forwarding data influenced the result.
type Provenance string

const (
	// Direct means only direct connection information was used.
	Direct Provenance = "direct"
	// TrustedForwarded means trusted forwarding metadata was used.
	TrustedForwarded Provenance = "trusted_forwarded"
)

// Info contains independently derived effective values.
type Info struct {
	ClientIP   netip.Addr
	Host       string
	Scheme     string
	Prefix     string
	Provenance Provenance
}

// Policy is copied during construction. Trusted accepts at most 256 valid
// prefixes; prefixes are stored in canonical masked form.
type Policy struct {
	Trusted        []netip.Prefix
	Mode           Mode
	MaxHops        int
	MaxHeaderBytes int
}

// ErrInvalidPolicy identifies invalid trusted proxy configuration.
var ErrInvalidPolicy = errors.New("proxy: invalid policy")

// ConfigError reports an invalid trusted proxy policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("proxy: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

type contextKey struct{}

const maximumTrustedPrefixes = 256

// New constructs trusted-proxy middleware. Malformed forwarding fields fail
// closed to direct connection information.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.Mode > XForwarded {
		return nil, &ConfigError{Field: "mode"}
	}
	if policy.MaxHops == 0 {
		policy.MaxHops = 16
	}
	if policy.MaxHeaderBytes == 0 {
		policy.MaxHeaderBytes = 8192
	}
	if policy.MaxHops < 1 || policy.MaxHops > 128 {
		return nil, &ConfigError{Field: "maximum hops"}
	}
	if policy.MaxHeaderBytes < 1 || policy.MaxHeaderBytes > 1<<20 {
		return nil, &ConfigError{Field: "maximum header bytes"}
	}
	if len(policy.Trusted) > maximumTrustedPrefixes {
		return nil, &ConfigError{Field: "trusted prefixes"}
	}
	trusted := make([]netip.Prefix, len(policy.Trusted))
	for index, prefix := range policy.Trusted {
		if !prefix.IsValid() {
			return nil, &ConfigError{Field: "trusted prefix"}
		}
		trusted[index] = prefix.Masked()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := directInfo(r)
			if isTrusted(info.ClientIP, trusted) {
				if forwarded, ok := forwardedInfo(r, info, policy.Mode, policy.MaxHops, policy.MaxHeaderBytes, trusted); ok {
					info = forwarded
				}
			}
			ctx := context.WithValue(r.Context(), contextKey{}, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}

// FromContext returns derived data, or its zero value when middleware was absent.
func FromContext(ctx context.Context) Info { value, _ := ctx.Value(contextKey{}).(Info); return value }

func directInfo(r *http.Request) Info {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	client, _ := netip.ParseAddr(strings.Trim(host, "[]"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return Info{ClientIP: client.Unmap(), Host: r.Host, Scheme: scheme, Provenance: Direct}
}

func forwardedInfo(r *http.Request, direct Info, mode Mode, maxHops, maxBytes int, trusted []netip.Prefix) (Info, bool) {
	if mode == Forwarded {
		return parseForwarded(r.Header.Values("Forwarded"), direct, maxHops, maxBytes, trusted)
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) != 1 {
		return Info{}, false
	}
	parts, valid := httpx.SplitDelimited(values[0], ',', maxBytes, maxHops)
	if !valid {
		return Info{}, false
	}
	hops := make([]netip.Addr, len(parts))
	for index, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return Info{}, false
		}
		hops[index] = address.Unmap()
	}
	client := selectClient(direct.ClientIP, hops, trusted)
	scheme, host, prefix := direct.Scheme, direct.Host, ""
	if value, present, valid := forwardedField(r.Header, "X-Forwarded-Proto", maxHops, maxBytes); !valid {
		return Info{}, false
	} else if present {
		value = strings.ToLower(value)
		if value != "http" && value != "https" {
			return Info{}, false
		}
		scheme = value
	}
	if value, present, valid := forwardedField(r.Header, "X-Forwarded-Host", maxHops, maxBytes); !valid {
		return Info{}, false
	} else if present {
		if !validHost(value) {
			return Info{}, false
		}
		host = value
	}
	if value, present, valid := forwardedField(r.Header, "X-Forwarded-Prefix", maxHops, maxBytes); !valid {
		return Info{}, false
	} else if present {
		if !validPrefix(value) {
			return Info{}, false
		}
		prefix = value
	}
	return Info{ClientIP: client, Host: host, Scheme: scheme, Prefix: prefix, Provenance: TrustedForwarded}, true
}

func parseForwarded(values []string, direct Info, maxHops, maxBytes int, trusted []netip.Prefix) (Info, bool) {
	if len(values) != 1 {
		return Info{}, false
	}
	elements, valid := httpx.SplitDelimited(values[0], ',', maxBytes, maxHops)
	if !valid {
		return Info{}, false
	}
	hops := make([]netip.Addr, 0, len(elements))
	result := direct
	for index, element := range elements {
		var found bool
		seen := make(map[string]bool)
		parameters, valid := httpx.SplitDelimited(element, ';', len(element), 32)
		if !valid {
			return Info{}, false
		}
		for _, parameter := range parameters {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok {
				return Info{}, false
			}
			key = strings.ToLower(strings.TrimSpace(key))
			if !validParameterName(key) || seen[key] {
				return Info{}, false
			}
			seen[key] = true
			value, ok = forwardedValue(value)
			if !ok {
				return Info{}, false
			}
			switch key {
			case "for":
				address, ok := parseNode(value)
				if !ok {
					return Info{}, false
				}
				hops = append(hops, address)
				found = true
			case "proto":
				if index == len(elements)-1 {
					value = strings.ToLower(value)
					if value != "http" && value != "https" {
						return Info{}, false
					}
					result.Scheme = value
				}
			case "host":
				if index == len(elements)-1 {
					if !validHost(value) {
						return Info{}, false
					}
					result.Host = value
				}
			}
		}
		if !found {
			return Info{}, false
		}
	}
	result.ClientIP = selectClient(direct.ClientIP, hops, trusted)
	result.Provenance = TrustedForwarded
	return result, true
}

func validParameterName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyz", character) {
			return false
		}
	}
	return true
}

func parseNode(value string) (netip.Addr, bool) {
	if strings.HasPrefix(value, "_") || strings.EqualFold(value, "unknown") {
		return netip.Addr{}, false
	}
	if address, err := netip.ParseAddr(strings.Trim(value, "[]")); err == nil {
		return address.Unmap(), true
	}
	addressPort, err := netip.ParseAddrPort(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addressPort.Addr().Unmap(), true
}

func selectClient(peer netip.Addr, hops []netip.Addr, trusted []netip.Prefix) netip.Addr {
	current := peer
	for index := len(hops) - 1; index >= 0 && isTrusted(current, trusted); index-- {
		current = hops[index]
	}
	return current
}
func isTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
func forwardedField(header http.Header, name string, maximum, maxBytes int) (string, bool, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 {
		return "", true, false
	}
	parts, valid := httpx.SplitDelimited(values[0], ',', maxBytes, maximum)
	if !valid {
		return "", true, false
	}
	value := strings.TrimSpace(parts[len(parts)-1])
	if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return "", true, false
	}
	return value, true, true
}
func validHost(value string) bool {
	if value == "" || len(value) > 255 || !httpx.ValidFieldValue(value, 255) || strings.ContainsAny(value, "/\\@") {
		return false
	}
	parsed, err := url.Parse("http://" + value)
	if err != nil || parsed.Host != value || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return false
		}
	}
	return true
}
func validPrefix(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || len(value) > 256 || !httpx.ValidFieldValue(value, 256) || strings.ContainsAny(value, "\\?#") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed != nil && parsed.RawQuery == "" && parsed.Fragment == "" && path.Clean(value) == value
}

func forwardedValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "\"") {
		return value, value != "" && !strings.Contains(value, "\"")
	}
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", false
	}
	var result strings.Builder
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if character == '\\' {
			index++
			if index >= len(value)-1 {
				return "", false
			}
			character = value[index]
		}
		if character == '"' || character == '\r' || character == '\n' || character == 0 {
			return "", false
		}
		result.WriteByte(character)
	}
	return result.String(), true
}
