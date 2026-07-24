package httpclient

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	// ErrInvalidAuthentication indicates invalid credential policy or input.
	ErrInvalidAuthentication = errors.New("invalid HTTP authentication")
)

// RequestEditor mutates one independently cloned HTTP request before it is
// sent. Implementations must be safe for concurrent use.
type RequestEditor interface {
	EditRequest(request *http.Request) error
}

// RequestEditorFunc adapts a function to RequestEditor.
type RequestEditorFunc func(request *http.Request) error

// EditRequest implements RequestEditor.
func (function RequestEditorFunc) EditRequest(request *http.Request) error {
	return function(request)
}

// RequestEditorError reports an editor failure without rendering its cause,
// which may contain credential material.
type RequestEditorError struct {
	Editor string
	Cause  error
}

// AuthenticationOptions configures origin-bound attempt authentication. With
// no AllowedOrigins, credentials are restricted to the logical operation's
// initial origin. Additional sensitive headers are stripped whenever a
// redirect leaves the trusted origin set.
type AuthenticationOptions struct {
	Name             string
	Layer            MiddlewareLayer
	Priority         int
	AllowedOrigins   []string
	SensitiveHeaders []string
	// AllowInsecure permits credentials on trusted plain-HTTP origins. It is
	// intended only for local tests.
	AllowInsecure bool
}

type authenticationContextKey struct{}

type authenticationScope struct {
	origins          map[string]struct{}
	sensitiveHeaders []string
	allowInsecure    bool
}

// HMACPhase identifies the vendor-supplied signing step that failed.
type HMACPhase uint8

const (
	// HMACCanonicalization identifies canonical-request construction.
	HMACCanonicalization HMACPhase = iota
	// HMACCalculation identifies message-authentication-code calculation.
	HMACCalculation
	// HMACApplication identifies signature placement on the request.
	HMACApplication
)

// HMACOptions supplies vendor-specific canonicalization and signature
// placement while core performs the HMAC calculation. Secret is copied.
type HMACOptions struct {
	Secret         []byte
	NewHash        func() hash.Hash
	Canonicalize   func(request *http.Request) ([]byte, error)
	ApplySignature func(request *http.Request, signature []byte) error
}

// HMACError reports a signing failure without rendering its cause or inputs.
type HMACError struct {
	Phase HMACPhase
	Cause error
}

// Error implements error without rendering credential or canonical data.
func (err *HMACError) Error() string {
	return fmt.Sprintf("HTTP HMAC %s failed", err.Phase)
}

// Unwrap returns the vendor callback or hash failure.
func (err *HMACError) Unwrap() error {
	return err.Cause
}

// String returns a stable phase name.
func (phase HMACPhase) String() string {
	switch phase {
	case HMACCanonicalization:
		return "canonicalization"
	case HMACCalculation:
		return "calculation"
	case HMACApplication:
		return "application"
	default:
		return fmt.Sprintf("phase(%d)", phase)
	}
}

// Error implements error without rendering the underlying failure.
func (err *RequestEditorError) Error() string {
	return fmt.Sprintf("HTTP request editor %q failed", err.Editor)
}

// Unwrap returns the underlying editor failure.
func (err *RequestEditorError) Unwrap() error {
	return err.Cause
}

// NewRequestEditorMiddleware adapts a generated-client or application request
// editor to the deterministic request middleware stage.
func NewRequestEditorMiddleware(options MiddlewareOptions, editor RequestEditor) (Middleware, error) {
	if nilLike(editor) {
		return Middleware{}, fmt.Errorf("%w: request editor is nil", ErrInvalidAuthentication)
	}

	return NewRequestMiddleware(options, func(request *http.Request, next Next) (*http.Response, error) {
		if err := editor.EditRequest(request); err != nil {
			return nil, &RequestEditorError{Editor: options.Name, Cause: err}
		}

		return next(request)
	})
}

// NewAuthenticationMiddleware creates paired operation and attempt
// middleware. The operation captures the trusted origin set once; every
// physical attempt independently applies or strips credentials.
func NewAuthenticationMiddleware(options AuthenticationOptions, editor RequestEditor) ([]Middleware, error) {
	if nilLike(editor) {
		return nil, fmt.Errorf("%w: request editor is nil", ErrInvalidAuthentication)
	}
	configuredOrigins, err := parseAuthenticationOrigins(options.AllowedOrigins, options.AllowInsecure)
	if err != nil {
		return nil, err
	}
	sensitiveHeaders, err := authenticationSensitiveHeaders(options.SensitiveHeaders)
	if err != nil {
		return nil, err
	}

	operationOptions := MiddlewareOptions{
		Name:     options.Name,
		Scope:    ScopeOperation,
		Layer:    options.Layer,
		Priority: options.Priority,
	}
	operation, err := NewRequestMiddleware(operationOptions, func(request *http.Request, next Next) (*http.Response, error) {
		origin, originErr := requestOrigin(request)
		if originErr != nil {
			return nil, originErr
		}
		origins := configuredOrigins
		if len(origins) == 0 {
			if request.URL.Scheme != "https" && !options.AllowInsecure {
				return nil, fmt.Errorf("%w: credential origin must use HTTPS", ErrInvalidAuthentication)
			}
			origins = map[string]struct{}{origin: {}}
		}
		scope := authenticationScope{
			origins:          origins,
			sensitiveHeaders: sensitiveHeaders,
			allowInsecure:    options.AllowInsecure,
		}

		return next(request.WithContext(context.WithValue(request.Context(), authenticationContextKey{}, scope)))
	})
	if err != nil {
		return nil, err
	}

	attempt := operation
	attempt.information.Scope = ScopeAttempt
	attempt.around = func(request *http.Request, next Next) (*http.Response, error) {
		scope, ok := request.Context().Value(authenticationContextKey{}).(authenticationScope)
		if !ok {
			return nil, fmt.Errorf("%w: authentication operation context is missing", ErrInvalidAuthentication)
		}
		origin, originErr := requestOrigin(request)
		if originErr != nil {
			return nil, originErr
		}
		if _, trusted := scope.origins[origin]; !trusted {
			for _, name := range scope.sensitiveHeaders {
				request.Header.Del(name)
			}

			return next(request)
		}
		if request.URL.Scheme != "https" && !scope.allowInsecure {
			return nil, fmt.Errorf("%w: credential origin must use HTTPS", ErrInvalidAuthentication)
		}
		if editErr := editor.EditRequest(request); editErr != nil {
			return nil, &RequestEditorError{Editor: options.Name, Cause: editErr}
		}

		return next(request)
	}

	return []Middleware{operation, attempt}, nil
}

func parseAuthenticationOrigins(origins []string, allowInsecure bool) (map[string]struct{}, error) {
	if len(origins) == 0 {
		return nil, nil
	}

	parsed := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		candidate, err := url.Parse(raw)
		if err != nil || candidate.User != nil || candidate.RawQuery != "" || candidate.Fragment != "" ||
			(candidate.Path != "" && candidate.Path != "/") {
			return nil, fmt.Errorf("%w: allowed origin is malformed", ErrInvalidAuthentication)
		}
		if candidate.Scheme != "https" && !allowInsecure {
			return nil, fmt.Errorf("%w: allowed origin must use HTTPS", ErrInvalidAuthentication)
		}
		origin, err := canonicalOrigin(candidate)
		if err != nil {
			return nil, err
		}
		parsed[origin] = struct{}{}
	}

	return parsed, nil
}

func authenticationSensitiveHeaders(additional []string) ([]string, error) {
	seen := map[string]struct{}{
		"Authorization":       {},
		"Cookie":              {},
		"Proxy-Authorization": {},
	}
	for _, name := range additional {
		canonical, err := validateHeaderName(name)
		if err != nil {
			return nil, fmt.Errorf("%w: sensitive header name is malformed", ErrInvalidAuthentication)
		}
		seen[canonical] = struct{}{}
	}

	headers := make([]string, 0, len(seen))
	for name := range seen {
		headers = append(headers, name)
	}
	sort.Strings(headers)

	return headers, nil
}

func requestOrigin(request *http.Request) (string, error) {
	if request == nil || request.URL == nil {
		return "", fmt.Errorf("%w: request URL is nil", ErrInvalidAuthentication)
	}

	return canonicalOrigin(request.URL)
}

func canonicalOrigin(candidate *url.URL) (string, error) {
	if candidate == nil || candidate.User != nil ||
		(candidate.Scheme != "http" && candidate.Scheme != "https") || candidate.Host == "" {
		return "", fmt.Errorf("%w: origin must be absolute HTTP(S)", ErrInvalidAuthentication)
	}
	hostname, err := normalizeEgressHost(candidate.Hostname())
	if err != nil {
		return "", fmt.Errorf("%w: origin host is empty", ErrInvalidAuthentication)
	}
	port, err := egressPort(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: origin port is invalid", ErrInvalidAuthentication)
	}
	host := hostname
	if strings.ContainsRune(hostname, ':') {
		host = "[" + hostname + "]"
	}
	if port != 80 || candidate.Scheme != "http" {
		if port != 443 || candidate.Scheme != "https" {
			host = net.JoinHostPort(hostname, fmt.Sprint(port))
		}
	}

	return candidate.Scheme + "://" + host, nil
}

// NewBasicAuth returns an immutable HTTP Basic authentication editor. User
// names containing a colon are rejected because the delimiter would make the
// credentials ambiguous.
func NewBasicAuth(username string, password string) (RequestEditor, error) {
	if username == "" || strings.ContainsRune(username, ':') ||
		!utf8.ValidString(username) || !utf8.ValidString(password) {
		return nil, fmt.Errorf("%w: basic credentials are malformed", ErrInvalidAuthentication)
	}

	return basicAuthEditor{username: username, password: password}, nil
}

type basicAuthEditor struct {
	username string
	password string
}

func (editor basicAuthEditor) EditRequest(request *http.Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidAuthentication)
	}
	request.SetBasicAuth(editor.username, editor.password)

	return nil
}

// NewBearerAuth returns an immutable RFC 6750 bearer-token editor.
func NewBearerAuth(token string) (RequestEditor, error) {
	if !validBearerToken(token) {
		return nil, fmt.Errorf("%w: bearer token is malformed", ErrInvalidAuthentication)
	}

	return headerCredentialEditor{name: "Authorization", value: "Bearer " + token}, nil
}

// NewAPIKeyHeader returns an immutable header API-key editor. Existing values
// for name are replaced rather than appended.
func NewAPIKeyHeader(name string, value string) (RequestEditor, error) {
	canonicalName, err := validateHeaderName(name)
	if err != nil || value == "" || !validHeaderValue(value) {
		return nil, fmt.Errorf("%w: API key header is malformed", ErrInvalidAuthentication)
	}

	return headerCredentialEditor{name: canonicalName, value: value}, nil
}

type headerCredentialEditor struct {
	name  string
	value string
}

func (editor headerCredentialEditor) EditRequest(request *http.Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidAuthentication)
	}
	request.Header.Set(editor.name, editor.value)

	return nil
}

// NewAPIKeyQuery returns an immutable query API-key editor. Its explicit name
// makes URL placement opt-in; callers should prefer headers whenever the
// provider supports them.
func NewAPIKeyQuery(name string, value string) (RequestEditor, error) {
	if err := validateQueryName(name); err != nil || value == "" {
		return nil, fmt.Errorf("%w: API key query parameter is malformed", ErrInvalidAuthentication)
	}

	return queryCredentialEditor{name: name, value: value}, nil
}

// NewHMACAuth returns an immutable HMAC request editor. The vendor package
// retains control of canonicalization and signature syntax so core does not
// impose a provider-specific signing protocol.
func NewHMACAuth(options HMACOptions) (RequestEditor, error) {
	if len(options.Secret) == 0 || options.NewHash == nil ||
		options.Canonicalize == nil || options.ApplySignature == nil {
		return nil, fmt.Errorf("%w: HMAC policy is incomplete", ErrInvalidAuthentication)
	}
	if err := validateHMACHashFactory(options.NewHash); err != nil {
		return nil, err
	}

	return hmacAuthEditor{
		secret:         append([]byte(nil), options.Secret...),
		newHash:        options.NewHash,
		canonicalize:   options.Canonicalize,
		applySignature: options.ApplySignature,
	}, nil
}

type hmacAuthEditor struct {
	secret         []byte
	newHash        func() hash.Hash
	canonicalize   func(request *http.Request) ([]byte, error)
	applySignature func(request *http.Request, signature []byte) error
}

func (editor hmacAuthEditor) EditRequest(request *http.Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidAuthentication)
	}
	canonical, err := editor.canonicalize(request)
	if err != nil {
		return &HMACError{Phase: HMACCanonicalization, Cause: err}
	}
	signature, err := calculateHMAC(editor.newHash, editor.secret, canonical)
	if err != nil {
		return &HMACError{Phase: HMACCalculation, Cause: err}
	}
	if err := editor.applySignature(request, signature); err != nil {
		return &HMACError{Phase: HMACApplication, Cause: err}
	}

	return nil
}

func validateHMACHashFactory(factory func() hash.Hash) (failure error) {
	defer func() {
		if recover() != nil {
			failure = fmt.Errorf("%w: HMAC hash factory panicked", ErrInvalidAuthentication)
		}
	}()
	if nilLike(factory()) {
		return fmt.Errorf("%w: HMAC hash factory returned nil", ErrInvalidAuthentication)
	}

	return nil
}

func calculateHMAC(factory func() hash.Hash, secret []byte, canonical []byte) (signature []byte, failure error) {
	defer func() {
		if recover() != nil {
			signature = nil
			failure = errors.New("HMAC hash factory failed")
		}
	}()
	mac := hmac.New(factory, secret)
	_, _ = mac.Write(canonical)

	return mac.Sum(nil), nil
}

type queryCredentialEditor struct {
	name  string
	value string
}

func (editor queryCredentialEditor) EditRequest(request *http.Request) error {
	if request == nil || request.URL == nil {
		return fmt.Errorf("%w: request URL is nil", ErrInvalidAuthentication)
	}
	query := request.URL.Query()
	query.Set(editor.name, editor.value)
	request.URL.RawQuery = query.Encode()

	return nil
}

func validBearerToken(token string) bool {
	if token == "" {
		return false
	}

	padding := false
	for _, character := range token {
		if character == '=' {
			padding = true

			continue
		}
		if padding || !bearerTokenCharacter(character) {
			return false
		}
	}

	return true
}

func bearerTokenCharacter(character rune) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~+/", character)
}
