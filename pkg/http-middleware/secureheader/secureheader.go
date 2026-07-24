// Package secureheader applies explicit response security header policy.
package secureheader

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// ExistingPolicy controls configured versus downstream header precedence.
type ExistingPolicy uint8

const (
	// Replace reapplies configured values when the response commits.
	Replace ExistingPolicy = iota
	// Preserve retains an existing downstream value.
	Preserve
)

// Policy is copied during construction. HSTS requires AcknowledgeHSTS because
// an incorrect deployment can create a long-lived outage.
type Policy struct {
	XContentTypeOptions   string
	ReferrerPolicy        string
	FrameOptions          string
	PermissionsPolicy     string
	ContentSecurityPolicy string
	HSTS                  string
	AcknowledgeHSTS       bool
	Existing              ExistingPolicy
}

// ErrInvalidPolicy identifies invalid security header configuration.
var ErrInvalidPolicy = errors.New("secureheader: invalid policy")

// ConfigError reports an invalid security header policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("secureheader: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// APIDefaults returns conservative headers that do not assume HTML rendering.
func APIDefaults() Policy {
	return Policy{XContentTypeOptions: "nosniff", ReferrerPolicy: "no-referrer", FrameOptions: "DENY"}
}

// New constructs header middleware.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.Existing > Preserve {
		return nil, &ConfigError{Field: "existing policy"}
	}
	if policy.HSTS != "" && !policy.AcknowledgeHSTS {
		return nil, &ConfigError{Field: "HSTS acknowledgement"}
	}
	if policy.HSTS != "" && !validHSTS(policy.HSTS) {
		return nil, &ConfigError{Field: "HSTS"}
	}
	if policy.XContentTypeOptions != "" && !strings.EqualFold(policy.XContentTypeOptions, "nosniff") {
		return nil, &ConfigError{Field: "X-Content-Type-Options"}
	}
	if policy.FrameOptions != "" && !strings.EqualFold(policy.FrameOptions, "DENY") && !strings.EqualFold(policy.FrameOptions, "SAMEORIGIN") {
		return nil, &ConfigError{Field: "X-Frame-Options"}
	}
	if policy.ReferrerPolicy != "" && !validReferrerPolicy(policy.ReferrerPolicy) {
		return nil, &ConfigError{Field: "Referrer-Policy"}
	}
	headers := map[string]string{"X-Content-Type-Options": policy.XContentTypeOptions, "Referrer-Policy": policy.ReferrerPolicy, "X-Frame-Options": policy.FrameOptions, "Permissions-Policy": policy.PermissionsPolicy, "Content-Security-Policy": policy.ContentSecurityPolicy, "Strict-Transport-Security": policy.HSTS}
	for name, value := range headers {
		if !httpx.ValidFieldValue(value, 4096) {
			return nil, &ConfigError{Field: name}
		}
	}
	apply := func(header http.Header) {
		for name, value := range headers {
			if value != "" && (policy.Existing == Replace || header.Get(name) == "") {
				header.Set(name, value)
			}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apply(w.Header())
			next.ServeHTTP(httpx.WithPolicy(w, apply), r)
		})
	}, nil
}

func validHSTS(value string) bool {
	parts := strings.Split(value, ";")
	seen := map[string]bool{}
	hasAge := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		key, parameter, hasParameter := strings.Cut(part, "=")
		key = strings.ToLower(key)
		if part == "" || seen[key] {
			return false
		}
		seen[key] = true
		switch key {
		case "max-age":
			if !hasParameter {
				return false
			}
			seconds, err := strconv.ParseUint(parameter, 10, 64)
			if err != nil || seconds > 315360000 {
				return false
			}
			hasAge = true
		case "includesubdomains", "preload":
			if hasParameter {
				return false
			}
		default:
			return false
		}
	}
	return hasAge
}

func validReferrerPolicy(value string) bool {
	switch strings.ToLower(value) {
	case "no-referrer", "no-referrer-when-downgrade", "origin", "origin-when-cross-origin", "same-origin", "strict-origin", "strict-origin-when-cross-origin", "unsafe-url":
		return true
	default:
		return false
	}
}
