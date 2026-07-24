// Package cors implements the server-facing portion of the Fetch CORS
// protocol. CORS is neither authentication nor CSRF protection.
package cors

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// ErrInvalidPolicy identifies invalid CORS policy configuration.
var ErrInvalidPolicy = errors.New("cors: invalid policy")

// ConfigError reports an invalid CORS policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("cors: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// Policy is normalized and copied during construction.
type Policy struct {
	AllowedOrigins      []string
	AllowOrigin         func(context.Context, string) (bool, error)
	AllowedMethods      []string
	AllowedHeaders      []string
	ExposedHeaders      []string
	AllowCredentials    bool
	AllowPrivateNetwork bool
	MaxAgeSeconds       int
	PassPreflight       bool
	MaxHeaderValues     int
	MaxHeaderBytes      int
}

type compiled struct {
	origins         map[string]bool
	wildcard        bool
	dynamic         func(context.Context, string) (bool, error)
	methods         map[string]bool
	methodsWildcard bool
	headers         map[string]bool
	headersWildcard bool
	exposed         string
	credentials     bool
	privateNetwork  bool
	maxAge          int
	pass            bool
	maxValues       int
	maxBytes        int
}

// New validates wildcard and credential combinations and constructs middleware.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	configuration, err := compile(policy)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if corsHeaderBytes(r.Header) > configuration.maxBytes {
				if r.Method == http.MethodOptions && len(r.Header.Values("Access-Control-Request-Method")) > 0 {
					httpx.SafeError(w, http.StatusBadRequest, "invalid CORS preflight\n")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			originValues := r.Header.Values("Origin")
			if len(originValues) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if !configuration.wildcard {
				httpx.AddVary(w.Header(), "Origin")
			}
			if len(originValues) != 1 || len(originValues) > configuration.maxValues {
				serveVary(next, w, r, configuration.wildcard)
				return
			}
			origin, ok := canonicalOrigin(originValues[0])
			if !ok {
				serveVary(next, w, r, configuration.wildcard)
				return
			}
			allowed := configuration.wildcard
			if configuration.origins[origin] {
				allowed = true
			}
			if !allowed && configuration.dynamic != nil {
				accepted, dynamicErr := configuration.dynamic(r.Context(), origin)
				allowed = dynamicErr == nil && accepted
			}
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if !allowed {
				if preflight {
					httpx.SafeError(w, http.StatusForbidden, "CORS preflight denied\n")
					return
				}
				serveVary(next, w, r, configuration.wildcard)
				return
			}
			allowOrigin := origin
			if configuration.wildcard {
				allowOrigin = "*"
			}
			apply := func(header http.Header) {
				if !configuration.wildcard {
					httpx.AddVary(header, "Origin")
				}
				header.Set("Access-Control-Allow-Origin", allowOrigin)
				if configuration.credentials {
					header.Set("Access-Control-Allow-Credentials", "true")
				}
				if configuration.exposed != "" {
					header.Set("Access-Control-Expose-Headers", configuration.exposed)
				}
			}
			apply(w.Header())
			if !preflight {
				next.ServeHTTP(httpx.WithPolicy(w, apply), r)
				return
			}
			if !configuration.preflight(w, r) {
				return
			}
			if configuration.pass {
				next.ServeHTTP(httpx.WithPolicy(w, apply), r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}, nil
}

func serveVary(next http.Handler, w http.ResponseWriter, r *http.Request, wildcard bool) {
	if wildcard {
		next.ServeHTTP(w, r)
		return
	}
	apply := func(header http.Header) { httpx.AddVary(header, "Origin") }
	next.ServeHTTP(httpx.WithPolicy(w, apply), r)
}

func compile(policy Policy) (compiled, error) {
	result := compiled{origins: map[string]bool{}, methods: map[string]bool{}, headers: map[string]bool{}, dynamic: policy.AllowOrigin, credentials: policy.AllowCredentials, privateNetwork: policy.AllowPrivateNetwork, maxAge: policy.MaxAgeSeconds, pass: policy.PassPreflight, maxValues: policy.MaxHeaderValues, maxBytes: policy.MaxHeaderBytes}
	if result.maxValues == 0 {
		result.maxValues = 64
	}
	if result.maxBytes == 0 {
		result.maxBytes = 8192
	}
	if result.maxValues < 1 || result.maxValues > 256 || policy.MaxAgeSeconds < 0 || policy.MaxAgeSeconds > 86400 {
		return compiled{}, &ConfigError{Field: "limit"}
	}
	if result.maxBytes < 1 || result.maxBytes > 1<<20 {
		return compiled{}, &ConfigError{Field: "header bytes"}
	}
	if len(policy.AllowedOrigins) > result.maxValues || len(policy.AllowedMethods) > result.maxValues || len(policy.AllowedHeaders) > result.maxValues || len(policy.ExposedHeaders) > result.maxValues {
		return compiled{}, &ConfigError{Field: "values"}
	}
	for _, origin := range policy.AllowedOrigins {
		if origin == "*" {
			result.wildcard = true
			continue
		}
		canonical, ok := canonicalOrigin(origin)
		if !ok {
			return compiled{}, &ConfigError{Field: "origin"}
		}
		result.origins[canonical] = true
	}
	if result.wildcard && policy.AllowCredentials {
		return compiled{}, &ConfigError{Field: "credentialed wildcard"}
	}
	methods := policy.AllowedMethods
	if len(methods) == 0 {
		methods = []string{http.MethodGet, http.MethodHead}
	}
	for _, method := range methods {
		if !validToken(method) {
			return compiled{}, &ConfigError{Field: "method"}
		}
		if method == "*" {
			result.methodsWildcard = true
		} else {
			result.methods[method] = true
		}
	}
	for _, header := range policy.AllowedHeaders {
		if !validToken(header) {
			return compiled{}, &ConfigError{Field: "header"}
		}
		if header == "*" {
			result.headersWildcard = true
		} else {
			result.headers[strings.ToLower(header)] = true
		}
	}
	for _, header := range policy.ExposedHeaders {
		if !validToken(header) {
			return compiled{}, &ConfigError{Field: "exposed header"}
		}
	}
	if policy.AllowCredentials && (result.methodsWildcard || result.headersWildcard || contains(policy.ExposedHeaders, "*")) {
		return compiled{}, &ConfigError{Field: "credentialed wildcard"}
	}
	result.exposed = strings.Join(policy.ExposedHeaders, ", ")
	return result, nil
}

func (c compiled) preflight(w http.ResponseWriter, r *http.Request) bool {
	httpx.AddVary(w.Header(), "Access-Control-Request-Method", "Access-Control-Request-Headers")
	methodValue, present, valid := singular(r.Header, "Access-Control-Request-Method", 128)
	if !present || !valid || !validToken(methodValue) {
		clearCORS(w.Header())
		httpx.SafeError(w, http.StatusBadRequest, "invalid CORS preflight\n")
		return false
	}
	method := methodValue
	if !c.methodsWildcard && !c.methods[method] {
		clearCORS(w.Header())
		httpx.SafeError(w, http.StatusForbidden, "CORS preflight denied\n")
		return false
	}
	requested := splitHeaderList(r.Header.Values("Access-Control-Request-Headers"), c.maxValues, c.maxBytes)
	if requested == nil && r.Header.Get("Access-Control-Request-Headers") != "" {
		clearCORS(w.Header())
		httpx.SafeError(w, http.StatusBadRequest, "invalid CORS preflight\n")
		return false
	}
	for _, header := range requested {
		if !c.headersWildcard && !c.headers[strings.ToLower(header)] {
			clearCORS(w.Header())
			httpx.SafeError(w, http.StatusForbidden, "CORS preflight denied\n")
			return false
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", method)
	if len(requested) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(requested, ", "))
	}
	if c.maxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(c.maxAge))
	}
	privateValue, privatePresent, privateValid := singular(r.Header, "Access-Control-Request-Private-Network", 8)
	if !privateValid {
		clearCORS(w.Header())
		httpx.SafeError(w, http.StatusBadRequest, "invalid CORS preflight\n")
		return false
	}
	if privatePresent && privateValue == "true" {
		if !c.privateNetwork {
			clearCORS(w.Header())
			httpx.SafeError(w, http.StatusForbidden, "CORS private network denied\n")
			return false
		}
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
	if privatePresent && privateValue != "true" {
		clearCORS(w.Header())
		httpx.SafeError(w, http.StatusBadRequest, "invalid CORS preflight\n")
		return false
	}
	return true
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func singular(header http.Header, name string, maximum int) (string, bool, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 {
		return "", true, false
	}
	value := strings.TrimSpace(values[0])
	if value == "" || len(value) > maximum || strings.Contains(value, ",") {
		return "", true, false
	}
	return value, true, true
}

func canonicalOrigin(raw string) (string, bool) {
	if raw == "null" {
		return raw, true
	}
	if len(raw) == 0 || len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return "", false
	}
	ascii := hostname
	if net.ParseIP(hostname) == nil {
		ascii, err = idna.Lookup.ToASCII(hostname)
		if err != nil {
			return "", false
		}
	}
	ascii = strings.ToLower(ascii)
	port := parsed.Port()
	if strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}
	if port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return "", false
		}
	}
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	host := ascii
	if strings.Contains(ascii, ":") {
		host = "[" + ascii + "]"
	}
	if port != "" {
		host = net.JoinHostPort(ascii, port)
	}
	return parsed.Scheme + "://" + host, true
}

func splitHeaderList(values []string, maximum, maxBytes int) []string {
	var result []string
	remaining := maxBytes
	for _, line := range values {
		parts, ok := httpx.SplitDelimited(line, ',', remaining, maximum-len(result))
		if !ok {
			return nil
		}
		remaining -= len(line)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" || !validToken(part) || len(result) >= maximum {
				return nil
			}
			result = append(result, http.CanonicalHeaderKey(part))
		}
	}
	return result
}

func corsHeaderBytes(header http.Header) int {
	total := 0
	for _, name := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers", "Access-Control-Request-Private-Network"} {
		for _, value := range header.Values(name) {
			total += len(value)
		}
	}
	return total
}
func validToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", char) {
			return false
		}
	}
	return true
}
func clearCORS(header http.Header) {
	for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Max-Age", "Access-Control-Allow-Private-Network"} {
		header.Del(name)
	}
}
