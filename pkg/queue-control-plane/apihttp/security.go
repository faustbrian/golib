package apihttp

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

var ErrInvalidSecurityConfiguration = errors.New("apihttp: invalid security configuration")

// RateLimiter makes one bounded admission decision for a stable safe key.
type RateLimiter interface {
	Allow(context.Context, string) bool
}

// SecurityConfig controls browser and request-admission protections.
type SecurityConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
	RateLimiter      RateLimiter
}

type securityMiddleware struct {
	origins          map[string]struct{}
	allowCredentials bool
	rateLimiter      RateLimiter
}

// NewSecurityMiddleware creates the administrative transport security layer.
func NewSecurityMiddleware(config SecurityConfig) (func(http.Handler) http.Handler, error) {
	origins := make(map[string]struct{}, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		if !validOrigin(origin) {
			return nil, ErrInvalidSecurityConfiguration
		}
		origins[origin] = struct{}{}
	}
	if config.RateLimiter != nil && nilInterface(config.RateLimiter) {
		return nil, ErrInvalidSecurityConfiguration
	}

	middleware := &securityMiddleware{
		origins:          origins,
		allowCredentials: config.AllowCredentials,
		rateLimiter:      config.RateLimiter,
	}

	return middleware.wrap, nil
}

func (m *securityMiddleware) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(writer.Header())
		origin := request.Header.Get("Origin")
		if origin != "" {
			if !m.originAllowed(origin) {
				writeProblem(writer, http.StatusForbidden, "origin_forbidden")
				return
			}
			m.setCORSHeaders(writer.Header(), origin)
		}
		if isPreflight(request) {
			if origin == "" || !validPreflight(request) {
				writeProblem(writer, http.StatusForbidden, "preflight_forbidden")
				return
			}
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if m.rateLimiter != nil && !m.rateLimiter.Allow(request.Context(), rateLimitKey(request)) {
			writer.Header().Set("Retry-After", "1")
			writeProblem(writer, http.StatusTooManyRequests, "rate_limited")
			return
		}
		if requiresCSRF(request) && (origin == "" || !validCSRFToken(request)) {
			writeProblem(writer, http.StatusForbidden, "csrf_failed")
			return
		}

		next.ServeHTTP(writer, request)
	})
}

func (m *securityMiddleware) originAllowed(origin string) bool {
	_, ok := m.origins[origin]

	return ok
}

func (m *securityMiddleware) setCORSHeaders(header http.Header, origin string) {
	header.Set("Access-Control-Allow-Origin", origin)
	header.Add("Vary", "Origin")
	header.Set("Access-Control-Allow-Methods", "GET, POST")
	header.Set(
		"Access-Control-Allow-Headers",
		"authorization, content-type, x-csrf-token, x-queue-control-key-id, x-queue-control-key",
	)
	if m.allowCredentials {
		header.Set("Access-Control-Allow-Credentials", "true")
	}
}

func validOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}

	return parsed.String() == origin
}

func setSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}

func isPreflight(request *http.Request) bool {
	return request.Method == http.MethodOptions &&
		request.Header.Get("Access-Control-Request-Method") != ""
}

func validPreflight(request *http.Request) bool {
	method := request.Header.Get("Access-Control-Request-Method")
	if method != http.MethodGet && method != http.MethodPost {
		return false
	}
	allowed := map[string]bool{
		"authorization":          true,
		"content-type":           true,
		"x-csrf-token":           true,
		"x-queue-control-key-id": true,
		"x-queue-control-key":    true,
	}
	for _, header := range strings.Split(request.Header.Get("Access-Control-Request-Headers"), ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if header != "" && !allowed[header] {
			return false
		}
	}

	return true
}

func requiresCSRF(request *http.Request) bool {
	if request.Header.Get("Cookie") == "" || strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
		return false
	}
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validCSRFToken(request *http.Request) bool {
	cookie, err := request.Cookie("csrf_token")
	if err != nil || cookie.Value == "" {
		return false
	}
	header := request.Header.Get("X-CSRF-Token")
	if len(header) != len(cookie.Value) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}

func rateLimitKey(request *http.Request) string {
	if principal, ok := authenticationPrincipal(request.Context()); ok {
		return "subject:" + principal
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	if host == "" {
		host = "unknown"
	}

	return "address:" + host
}

func authenticationPrincipal(ctx context.Context) (string, bool) {
	principal, ok := authentication.PrincipalFromContext(ctx)
	if !ok || principal.IsAnonymous() {
		return "", false
	}

	return principal.Subject(), true
}
