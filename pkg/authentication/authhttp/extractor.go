// Package authhttp provides strict HTTP credential extraction, challenges,
// and authentication-only middleware for net/http.
package authhttp

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

const (
	defaultMaxBasicBytes  = 8 * 1024
	defaultMaxBearerBytes = 8 * 1024
	defaultMaxKeyIDBytes  = 256
	defaultMaxAPIKeyBytes = 8 * 1024
)

// Source is an explicitly enabled HTTP credential location.
type Source interface {
	extract(*http.Request) (authentication.Credential, bool, error)
	validate() error
}

// Extractor rejects ambiguous credentials across all enabled sources.
type Extractor struct{ sources []Source }

// NewExtractor creates an extractor from one or more explicit sources.
func NewExtractor(sources ...Source) (*Extractor, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no HTTP credential sources", authentication.ErrInvalidConfiguration)
	}
	for _, source := range sources {
		if source == nil {
			return nil, fmt.Errorf("%w: nil HTTP credential source", authentication.ErrInvalidConfiguration)
		}
		if err := source.validate(); err != nil {
			return nil, err
		}
	}
	return &Extractor{sources: append([]Source(nil), sources...)}, nil
}

// Extract returns exactly one credential or a classified failure.
func (e *Extractor) Extract(request *http.Request) (authentication.Credential, error) {
	if request == nil {
		return nil, authentication.NewFailure(authentication.FailureInvalid)
	}

	var found authentication.Credential
	for _, source := range e.sources {
		credential, present, err := source.extract(request)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		if found != nil {
			return nil, authentication.NewFailure(authentication.FailureAmbiguous)
		}
		found = credential
	}
	if found == nil {
		return nil, authentication.NewFailure(authentication.FailureAbsent)
	}
	return found, nil
}

type authorizationSource struct {
	kind     authentication.CredentialKind
	maxBytes int
}

// BasicAuthorization enables Basic extraction from the Authorization header.
func BasicAuthorization() Source {
	return authorizationSource{kind: authentication.CredentialBasic, maxBytes: defaultMaxBasicBytes}
}

// BearerOption configures a bearer source.
type BearerOption func(*authorizationSource)

// WithBearerMaxBytes sets the inclusive bearer-token size bound.
func WithBearerMaxBytes(maximum int) BearerOption {
	return func(source *authorizationSource) { source.maxBytes = maximum }
}

// BearerAuthorization enables bearer extraction from the Authorization header.
func BearerAuthorization(options ...BearerOption) Source {
	source := authorizationSource{kind: authentication.CredentialBearer, maxBytes: defaultMaxBearerBytes}
	applyBearerOptions(&source, options)
	return source
}

func (s authorizationSource) validate() error {
	if (s.kind != authentication.CredentialBasic && s.kind != authentication.CredentialBearer) || s.maxBytes <= 0 {
		return fmt.Errorf("%w: Authorization source", authentication.ErrInvalidConfiguration)
	}
	return nil
}

func (s authorizationSource) extract(request *http.Request) (authentication.Credential, bool, error) {
	values := request.Header.Values("Authorization")
	if len(values) == 0 {
		return nil, false, nil
	}
	if len(values) != 1 {
		return nil, false, authentication.NewFailure(authentication.FailureAmbiguous)
	}

	wanted := "Basic"
	if s.kind == authentication.CredentialBearer {
		wanted = "Bearer"
	}
	separator := strings.IndexAny(values[0], " \t")
	if separator < 0 {
		if strings.EqualFold(values[0], wanted) {
			return nil, false, authentication.NewFailure(authentication.FailureInvalid)
		}
		return nil, false, nil
	}
	scheme := values[0][:separator]
	if !strings.EqualFold(scheme, wanted) {
		return nil, false, nil
	}
	payload := values[0][separator+1:]
	if values[0][separator] != ' ' || payload == "" || strings.ContainsAny(payload, " \t\r\n") {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}

	if s.kind == authentication.CredentialBearer {
		if len(payload) > s.maxBytes || !validBearerToken(payload) {
			return nil, false, authentication.NewFailure(authentication.FailureInvalid)
		}
		return authentication.NewBearerCredential(payload), true, nil
	}
	if len(payload) > base64.StdEncoding.EncodedLen(s.maxBytes) {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil || len(decoded) > s.maxBytes || base64.StdEncoding.EncodeToString(decoded) != payload {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" || containsControl(username) || containsControl(password) {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	return authentication.NewBasicCredential(username, password), true, nil
}

func containsControl(value string) bool {
	for _, character := range []byte(value) {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

type bearerNamedSource struct {
	location sourceLocation
	name     string
	maxBytes int
}

// BearerQuery explicitly enables bearer extraction from a query parameter.
//
// Deprecated: credentials in URLs can be retained by logs, proxies, and
// browser history. Prefer BearerAuthorization for new designs.
func BearerQuery(name string, options ...BearerOption) Source {
	configuration := authorizationSource{maxBytes: defaultMaxBearerBytes}
	applyBearerOptions(&configuration, options)
	return bearerNamedSource{location: locationQuery, name: name, maxBytes: configuration.maxBytes}
}

// BearerCookie explicitly enables bearer extraction from a cookie.
func BearerCookie(name string, options ...BearerOption) Source {
	configuration := authorizationSource{maxBytes: defaultMaxBearerBytes}
	applyBearerOptions(&configuration, options)
	return bearerNamedSource{location: locationCookie, name: name, maxBytes: configuration.maxBytes}
}

func applyBearerOptions(source *authorizationSource, options []BearerOption) {
	for _, option := range options {
		if option != nil {
			option(source)
		}
	}
}

func (s bearerNamedSource) validate() error {
	if !validName(s.name) || s.maxBytes <= 0 || (s.location != locationQuery && s.location != locationCookie) {
		return fmt.Errorf("%w: bearer named source", authentication.ErrInvalidConfiguration)
	}
	return nil
}

func (s bearerNamedSource) extract(request *http.Request) (authentication.Credential, bool, error) {
	values, err := namedValues(request, s.location, s.name)
	if err != nil {
		return nil, false, err
	}
	if len(values) == 0 {
		return nil, false, nil
	}
	if len(values) != 1 {
		return nil, false, authentication.NewFailure(authentication.FailureAmbiguous)
	}
	if values[0] == "" || len(values[0]) > s.maxBytes || !validBearerToken(values[0]) {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	return authentication.NewBearerCredential(values[0]), true, nil
}

// APIKeyOption configures an API-key source.
type APIKeyOption func(*apiKeySource)

// WithAPIKeyMaxBytes sets the inclusive API-key size bound.
func WithAPIKeyMaxBytes(maximum int) APIKeyOption {
	return func(source *apiKeySource) { source.maxKeyBytes = maximum }
}

type apiKeySource struct {
	location    sourceLocation
	idName      string
	keyName     string
	maxIDBytes  int
	maxKeyBytes int
}

// APIKeyHeader explicitly enables API-key extraction from two headers.
func APIKeyHeader(idHeader, keyHeader string, options ...APIKeyOption) Source {
	return newAPIKeySource(locationHeader, idHeader, keyHeader, options)
}

// APIKeyQuery explicitly enables API-key extraction from two query parameters.
//
// Deprecated: credentials in URLs can be retained by logs, proxies, and
// browser history. Prefer APIKeyHeader for new designs.
func APIKeyQuery(idParameter, keyParameter string, options ...APIKeyOption) Source {
	return newAPIKeySource(locationQuery, idParameter, keyParameter, options)
}

// APIKeyCookie explicitly enables API-key extraction from two cookies.
func APIKeyCookie(idCookie, keyCookie string, options ...APIKeyOption) Source {
	return newAPIKeySource(locationCookie, idCookie, keyCookie, options)
}

func newAPIKeySource(location sourceLocation, idName, keyName string, options []APIKeyOption) Source {
	source := apiKeySource{
		location: location,
		idName:   idName, keyName: keyName,
		maxIDBytes: defaultMaxKeyIDBytes, maxKeyBytes: defaultMaxAPIKeyBytes,
	}
	for _, option := range options {
		if option != nil {
			option(&source)
		}
	}
	return source
}

func (s apiKeySource) validate() error {
	if !validName(s.idName) || !validName(s.keyName) || s.idName == s.keyName ||
		s.maxIDBytes <= 0 || s.maxKeyBytes <= 0 || s.location > locationCookie {
		return fmt.Errorf("%w: API-key source", authentication.ErrInvalidConfiguration)
	}
	return nil
}

func (s apiKeySource) extract(request *http.Request) (authentication.Credential, bool, error) {
	ids, err := namedValues(request, s.location, s.idName)
	if err != nil {
		return nil, false, err
	}
	keys, err := namedValues(request, s.location, s.keyName)
	// The same request and location succeeded above, so this lookup cannot fail.
	_ = err
	if len(ids) == 0 && len(keys) == 0 {
		return nil, false, nil
	}
	if len(ids) != 1 || len(keys) != 1 {
		if len(ids) > 1 || len(keys) > 1 {
			return nil, false, authentication.NewFailure(authentication.FailureAmbiguous)
		}
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	if ids[0] == "" || keys[0] == "" || len(ids[0]) > s.maxIDBytes || len(keys[0]) > s.maxKeyBytes {
		return nil, false, authentication.NewFailure(authentication.FailureInvalid)
	}
	return authentication.NewAPIKeyCredential(ids[0], keys[0]), true, nil
}

type sourceLocation uint8

const (
	locationHeader sourceLocation = iota
	locationQuery
	locationCookie
)

func namedValues(request *http.Request, location sourceLocation, name string) ([]string, error) {
	switch location {
	case locationHeader:
		return request.Header.Values(name), nil
	case locationQuery:
		if request.URL == nil {
			return nil, authentication.NewFailure(authentication.FailureInvalid)
		}
		values, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			return nil, authentication.NewFailure(authentication.FailureInvalid)
		}
		return values[name], nil
	case locationCookie:
		var values []string
		for _, cookie := range request.Cookies() {
			if cookie.Name == name {
				values = append(values, cookie.Value)
			}
		}
		return values, nil
	default:
		return nil, authentication.NewFailure(authentication.FailureInvalid)
	}
}

func validBearerToken(token string) bool {
	padding := false
	for _, character := range []byte(token) {
		if character == '=' {
			padding = true
			continue
		}
		if padding || !isBearerCharacter(character) {
			return false
		}
	}
	return token != ""
}

func isBearerCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~+/", rune(character))
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range []byte(name) {
		if character <= 0x20 || character >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(character)) {
			return false
		}
	}
	return true
}

var _ interface {
	Extract(*http.Request) (authentication.Credential, error)
} = (*Extractor)(nil)
