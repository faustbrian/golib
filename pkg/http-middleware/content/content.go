// Package content enforces HTTP media-type negotiation without decoding or
// encoding representations.
package content

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Policy configures request and response media-type requirements. Each
// configured list is bounded by MaxValues and both lists share the
// MaxHeaderBytes construction budget.
type Policy struct {
	RequestTypes, ResponseTypes []string
	MaxValues, MaxHeaderBytes   int
}

// ErrInvalidPolicy identifies invalid content negotiation configuration.
var ErrInvalidPolicy = errors.New("content: invalid policy")

// ConfigError reports an invalid content negotiation policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("content: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// New constructs 415 request and 406 response acceptance guards.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.MaxValues == 0 {
		policy.MaxValues = 64
	}
	if policy.MaxHeaderBytes == 0 {
		policy.MaxHeaderBytes = 8192
	}
	if policy.MaxValues < 1 || policy.MaxValues > 256 {
		return nil, &ConfigError{Field: "maximum values"}
	}
	if policy.MaxHeaderBytes < 1 || policy.MaxHeaderBytes > 1<<20 {
		return nil, &ConfigError{Field: "maximum header bytes"}
	}
	if len(policy.RequestTypes) > policy.MaxValues || len(policy.ResponseTypes) > policy.MaxValues {
		return nil, &ConfigError{Field: "media type values"}
	}
	configuredBytes := 0
	for _, value := range append(append([]string(nil), policy.RequestTypes...), policy.ResponseTypes...) {
		configuredBytes += len(value)
		if configuredBytes > policy.MaxHeaderBytes {
			return nil, &ConfigError{Field: "media type bytes"}
		}
	}
	requests, err := normalizeTypes(policy.RequestTypes)
	if err != nil {
		return nil, err
	}
	responses, err := normalizeTypes(policy.ResponseTypes)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(requests) > 0 && hasBody(r) {
				values := r.Header.Values("Content-Type")
				if len(values) != 1 {
					httpx.SafeError(w, http.StatusUnsupportedMediaType, "unsupported media type\n")
					return
				}
				mediaType, _, parseErr := mime.ParseMediaType(values[0])
				if parseErr != nil || !validMediaRange(mediaType, false) || !matchesAny(strings.ToLower(mediaType), requests) {
					httpx.SafeError(w, http.StatusUnsupportedMediaType, "unsupported media type\n")
					return
				}
			}
			if len(responses) > 0 && !acceptable(r.Header.Values("Accept"), responses, policy.MaxValues, policy.MaxHeaderBytes) {
				httpx.SafeError(w, http.StatusNotAcceptable, "not acceptable\n")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
func normalizeTypes(values []string) ([]string, error) {
	result := make([]string, len(values))
	for index, value := range values {
		mediaType, _, err := mime.ParseMediaType(value)
		if err != nil || !validMediaRange(mediaType, true) {
			return nil, &ConfigError{Field: "media type"}
		}
		result[index] = strings.ToLower(mediaType)
	}
	return result, nil
}
func hasBody(r *http.Request) bool { return r.Body != nil && r.ContentLength != 0 }
func matchesAny(candidate string, supported []string) bool {
	for _, item := range supported {
		major, minor, _ := strings.Cut(item, "/")
		cmajor, cminor, _ := strings.Cut(candidate, "/")
		if (major == "*" || cmajor == "*" || major == cmajor) && (minor == "*" || cminor == "*" || minor == cminor) {
			return true
		}
	}
	return false
}
func acceptable(lines []string, supported []string, maximum, maxBytes int) bool {
	if len(lines) == 0 {
		return true
	}
	count, remaining := 0, maxBytes
	matched := false
	for _, line := range lines {
		parts, ok := httpx.SplitDelimited(line, ',', remaining, maximum-count)
		if !ok {
			return false
		}
		remaining -= len(line)
		for _, part := range parts {
			count++
			fields, ok := httpx.SplitDelimited(part, ';', len(part), 16)
			if !ok {
				return false
			}
			media := fields[0]
			if !validMediaRange(media, true) {
				return false
			}
			q := 1.0
			qualitySeen := false
			for _, field := range fields[1:] {
				key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
				if !ok {
					return false
				}
				if _, _, err := mime.ParseMediaType("text/plain;" + field); err != nil {
					return false
				}
				if strings.EqualFold(key, "q") {
					if qualitySeen {
						return false
					}
					parsed, valid := httpx.ParseQuality(value)
					if !valid {
						return false
					}
					q = parsed
					qualitySeen = true
				}
			}
			if q > 0 && matchesAny(strings.ToLower(media), supported) {
				matched = true
			}
		}
	}
	return matched
}

func validMediaRange(value string, allowWildcard bool) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, value) {
		return false
	}
	major, minor, found := strings.Cut(mediaType, "/")
	if !found || major == "" || minor == "" {
		return false
	}
	if !allowWildcard {
		return !strings.Contains(mediaType, "*")
	}
	if strings.Contains(major, "*") {
		return major == "*" && minor == "*"
	}
	return !strings.Contains(minor, "*") || minor == "*"
}
