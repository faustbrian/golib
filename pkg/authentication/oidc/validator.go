// Package oidc provides strict OpenID Connect ID-token authentication using
// coreos/go-oidc and go-jose.
package oidc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

const defaultMaxTokenBytes = 16 * 1024

var supportedAlgorithms = map[string]struct{}{
	"RS256": {}, "RS384": {}, "RS512": {},
	"PS256": {}, "PS384": {}, "PS512": {},
	"ES256": {}, "ES384": {}, "ES512": {},
	"EdDSA": {},
}

var registeredClaims = map[string]struct{}{
	"at_hash": {}, "aud": {}, "auth_time": {}, "azp": {}, "exp": {},
	"iat": {}, "iss": {}, "jti": {}, "nbf": {}, "nonce": {}, "sub": {},
}

// Clock supplies validation time and permits deterministic tests.
//
// Deprecated: depend on clock.Clock in new code. This named compatibility
// contract remains available throughout v1.
type Clock interface {
	clockpkg.Clock
}

// NonceValidator validates the per-authentication-flow OIDC nonce.
type NonceValidator interface {
	ValidateNonce(context.Context, string) error
}

// NonceValidatorFunc adapts a function to NonceValidator.
type NonceValidatorFunc func(context.Context, string) error

// ValidateNonce calls f.
func (f NonceValidatorFunc) ValidateNonce(ctx context.Context, nonce string) error {
	return f(ctx, nonce)
}

// Config defines a strict OIDC ID-token trust boundary.
type Config struct {
	Issuer         string
	ClientID       string
	Algorithms     []string
	Clock          Clock
	ClockSkew      time.Duration
	NonceValidator NonceValidator
	MaxTokenBytes  int
	MaxClaims      int
	MaxClaimDepth  int
	ScopeClaim     string
	TenantClaim    string
	InsecureHTTP   bool

	HTTPClient         *http.Client
	MaxHTTPBodyBytes   int64
	DiscoveryTimeout   time.Duration
	MaxKeys            int
	MinRefreshInterval time.Duration
	MaxRefreshInterval time.Duration
	MaxRefreshWaiters  int
}

// Validator authenticates signed OIDC ID-token bearer credentials.
type Validator struct {
	verifier       *upstreamoidc.IDTokenVerifier
	clientID       string
	algorithms     map[string]struct{}
	clock          Clock
	clockSkew      time.Duration
	nonceValidator NonceValidator
	maxTokenBytes  int
	maxClaims      int
	maxClaimDepth  int
	scopeClaim     string
	tenantClaim    string
}

// NewWithKeySet creates a validator from an upstream standards-compliant key set.
func NewWithKeySet(configuration Config, keySet upstreamoidc.KeySet) (*Validator, error) {
	applyDefaults(&configuration)
	algorithms, err := validateConfig(configuration)
	if err != nil {
		return nil, err
	}
	if isNilInterface(keySet) {
		return nil, fmt.Errorf("%w: OIDC key set", authentication.ErrInvalidConfiguration)
	}

	verifier := upstreamoidc.NewVerifier(configuration.Issuer, keySet, &upstreamoidc.Config{
		ClientID:             configuration.ClientID,
		SupportedSigningAlgs: append([]string(nil), configuration.Algorithms...),
		Now:                  configuration.Clock.Now,
		SkipExpiryCheck:      true,
	})
	return newValidator(configuration, algorithms, verifier), nil
}

func newValidator(configuration Config, algorithms map[string]struct{}, verifier *upstreamoidc.IDTokenVerifier) *Validator {
	return &Validator{
		verifier: verifier, clientID: configuration.ClientID,
		algorithms: algorithms, clock: configuration.Clock, clockSkew: configuration.ClockSkew,
		nonceValidator: configuration.NonceValidator,
		maxTokenBytes:  configuration.MaxTokenBytes, maxClaims: configuration.MaxClaims,
		maxClaimDepth: configuration.MaxClaimDepth,
		scopeClaim:    configuration.ScopeClaim, tenantClaim: configuration.TenantClaim,
	}
}

// Authenticate validates an OIDC bearer credential and returns an immutable principal.
func (v *Validator) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	bearer, ok := credential.(authentication.BearerCredential)
	if !ok || bearer.Token() == "" || len(bearer.Token()) > v.maxTokenBytes {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	principal, err := v.ValidateBearer(ctx, bearer.Token())
	if err != nil {
		return authentication.Result{}, err
	}
	return authentication.NewAuthenticatedResult(principal)
}

// ValidateBearer verifies a bounded OIDC ID token.
func (v *Validator) ValidateBearer(ctx context.Context, rawToken string) (authentication.Principal, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if rawToken == "" || len(rawToken) > v.maxTokenBytes {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if err := inspectCompactToken(rawToken, v.algorithms, v.maxClaims, v.maxClaimDepth); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}

	report := &verificationReport{}
	verifyContext := context.WithValue(ctx, verificationReportKey{}, report)
	token, err := v.verifier.Verify(verifyContext, rawToken)
	if err != nil {
		if report.err != nil {
			return authentication.Principal{}, authentication.NewFailure(authentication.FailureUnavailable,
				authentication.WithFailureCause(report.err))
		}
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}
	principal, err := v.principal(ctx, token)
	if err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}
	return principal, nil
}

func (v *Validator) principal(ctx context.Context, token *upstreamoidc.IDToken) (authentication.Principal, error) {
	now := v.clock.Now()
	if token.Subject == "" || token.Issuer == "" || len(token.Audience) == 0 ||
		token.IssuedAt.IsZero() || token.Expiry.IsZero() ||
		!token.Expiry.After(now.Add(-v.clockSkew)) || token.IssuedAt.After(now.Add(v.clockSkew)) {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	var rawClaims map[string]any
	// Verify and inspectCompactToken have already proven valid JSON claims.
	_ = token.Claims(&rawClaims)
	var protocol struct {
		AuthorizedParty string          `json:"azp"`
		AuthTime        *int64          `json:"auth_time"`
		NotBefore       json.RawMessage `json:"nbf"`
	}
	if err := token.Claims(&protocol); err != nil {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if len(protocol.NotBefore) > 0 {
		notBefore, err := numericDate(protocol.NotBefore)
		if err != nil || notBefore.After(now.Add(v.clockSkew)) {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
	}
	if (len(token.Audience) > 1 && protocol.AuthorizedParty == "") ||
		(protocol.AuthorizedParty != "" && protocol.AuthorizedParty != v.clientID) {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if v.nonceValidator != nil {
		if err := v.nonceValidator.ValidateNonce(ctx, token.Nonce); err != nil {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
	}

	scopes, err := claimStrings(rawClaims[v.scopeClaim], true)
	if err != nil {
		return authentication.Principal{}, err
	}
	tenants, err := claimStrings(rawClaims[v.tenantClaim], false)
	if err != nil {
		return authentication.Principal{}, err
	}
	claims := make(map[string]any)
	for name, value := range rawClaims {
		if _, registered := registeredClaims[name]; registered || name == v.scopeClaim || name == v.tenantClaim {
			continue
		}
		claims[name] = value
	}
	authenticatedAt := token.IssuedAt
	if protocol.AuthTime != nil {
		if *protocol.AuthTime <= 0 || time.Unix(*protocol.AuthTime, 0).After(now.Add(v.clockSkew)) {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
		authenticatedAt = time.Unix(*protocol.AuthTime, 0).UTC()
	}

	return authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: token.Subject, Method: "oidc", Issuer: token.Issuer,
		Audiences: token.Audience, TenantHints: tenants, Scopes: scopes,
		Claims: claims, AuthenticatedAt: authenticatedAt,
	})
}

func numericDate(encoded json.RawMessage) (time.Time, error) {
	var seconds float64
	if err := json.Unmarshal(encoded, &seconds); err != nil || math.IsInf(seconds, 0) || math.IsNaN(seconds) ||
		seconds < -62135596800 || seconds > 253402300799 {
		return time.Time{}, authentication.ErrInvalidPrincipal
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*float64(time.Second))).UTC(), nil
}

func claimStrings(value any, splitSpaces bool) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		if splitSpaces {
			return strings.Fields(typed), nil
		}
		if typed == "" {
			return nil, authentication.ErrInvalidPrincipal
		}
		return []string{typed}, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		values := make([]string, len(typed))
		for index, item := range typed {
			text, ok := item.(string)
			if !ok || text == "" {
				return nil, authentication.ErrInvalidPrincipal
			}
			values[index] = text
		}
		return values, nil
	default:
		return nil, authentication.ErrInvalidPrincipal
	}
}

func applyDefaults(configuration *Config) {
	if configuration.ClockSkew == 0 {
		configuration.ClockSkew = 5 * time.Minute
	}
	if configuration.MaxTokenBytes == 0 {
		configuration.MaxTokenBytes = defaultMaxTokenBytes
	}
	if configuration.MaxClaims == 0 {
		configuration.MaxClaims = authentication.MaxClaims
	}
	if configuration.MaxClaimDepth == 0 {
		configuration.MaxClaimDepth = authentication.MaxClaimDepth
	}
	if configuration.ScopeClaim == "" {
		configuration.ScopeClaim = "scope"
	}
	if configuration.TenantClaim == "" {
		configuration.TenantClaim = "tenant"
	}
	if configuration.MaxHTTPBodyBytes == 0 {
		configuration.MaxHTTPBodyBytes = 1024 * 1024
	}
	if configuration.DiscoveryTimeout == 0 {
		configuration.DiscoveryTimeout = 10 * time.Second
	}
	if configuration.MaxKeys == 0 {
		configuration.MaxKeys = 64
	}
	if configuration.MinRefreshInterval == 0 {
		configuration.MinRefreshInterval = time.Minute
	}
	if configuration.MaxRefreshInterval == 0 {
		configuration.MaxRefreshInterval = time.Hour
	}
	if configuration.MaxRefreshWaiters == 0 {
		configuration.MaxRefreshWaiters = 64
	}
}

func validateConfig(configuration Config) (map[string]struct{}, error) {
	issuer, err := url.Parse(configuration.Issuer)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		(issuer.Scheme != "https" && (!configuration.InsecureHTTP || issuer.Scheme != "http")) ||
		configuration.ClientID == "" || configuration.Clock == nil ||
		configuration.ClockSkew < 0 || configuration.ClockSkew > 24*time.Hour ||
		configuration.MaxTokenBytes <= 0 || configuration.MaxClaims <= 0 ||
		configuration.MaxClaims > authentication.MaxClaims || configuration.MaxClaimDepth <= 0 ||
		configuration.MaxClaimDepth > authentication.MaxClaimDepth ||
		configuration.MaxHTTPBodyBytes <= 0 || configuration.DiscoveryTimeout <= 0 ||
		configuration.MaxKeys <= 0 || configuration.MinRefreshInterval <= 0 ||
		configuration.MaxRefreshInterval < configuration.MinRefreshInterval ||
		configuration.MaxRefreshWaiters <= 0 ||
		configuration.ScopeClaim == configuration.TenantClaim || len(configuration.Algorithms) == 0 ||
		isNilNonceValidator(configuration.NonceValidator) && configuration.NonceValidator != nil {
		return nil, fmt.Errorf("%w: OIDC configuration", authentication.ErrInvalidConfiguration)
	}
	allowed := make(map[string]struct{}, len(configuration.Algorithms))
	for _, algorithm := range configuration.Algorithms {
		if _, supported := supportedAlgorithms[algorithm]; !supported {
			return nil, fmt.Errorf("%w: OIDC algorithm", authentication.ErrInvalidConfiguration)
		}
		if _, duplicate := allowed[algorithm]; duplicate {
			return nil, fmt.Errorf("%w: duplicate OIDC algorithm", authentication.ErrInvalidConfiguration)
		}
		allowed[algorithm] = struct{}{}
	}
	return allowed, nil
}

func isNilNonceValidator(validator NonceValidator) bool {
	if validator == nil {
		return true
	}
	return isNilInterface(validator)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func inspectCompactToken(raw string, algorithms map[string]struct{}, maxClaims, maxDepth int) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return errors.New("invalid compact ID token")
	}
	header, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return err
	}
	claims, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return err
	}
	if err := inspectJSONObject(header, 64, 4); err != nil {
		return err
	}
	if err := inspectJSONObject(claims, maxClaims, maxDepth); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	// inspectJSONObject has already proven that header is valid JSON.
	_ = json.Unmarshal(header, &fields)
	var algorithm string
	if err := json.Unmarshal(fields["alg"], &algorithm); err != nil {
		return err
	}
	if _, allowed := algorithms[algorithm]; !allowed {
		return errors.New("disallowed ID-token algorithm")
	}
	if _, critical := fields["crit"]; critical {
		return errors.New("unsupported critical ID-token header")
	}
	return nil
}

func inspectJSONObject(encoded []byte, maxMembers, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder, 0, maxMembers, maxDepth, true); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == nil || token != nil {
		return errors.New("trailing JSON data")
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, depth, maxMembers, maxDepth int, top bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		if top {
			return errors.New("ID-token JSON value is not an object")
		}
		return nil
	}
	depth++
	if depth > maxDepth {
		return errors.New("ID-token JSON depth exceeded")
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			// JSON object member names are strings by grammar.
			name := nameToken.(string)
			if _, duplicate := seen[name]; duplicate {
				return errors.New("duplicate ID-token member")
			}
			seen[name] = struct{}{}
			if len(seen) > maxMembers {
				return errors.New("ID-token member bound exceeded")
			}
			if err := inspectJSONValue(decoder, depth, maxMembers, maxDepth, false); err != nil {
				return err
			}
		}
	case '[':
		count := 0
		for decoder.More() {
			count++
			if count > authentication.MaxClaimCollection {
				return errors.New("ID-token collection bound exceeded")
			}
			if err := inspectJSONValue(decoder, depth, maxMembers, maxDepth, false); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid ID-token JSON delimiter")
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if top && delimiter != '{' {
		return errors.New("ID-token JSON value is not an object")
	}
	return nil
}

var _ authentication.Authenticator = (*Validator)(nil)

type verificationReportKey struct{}

type verificationReport struct{ err error }

func reportUnavailable(ctx context.Context, err error) {
	if report, ok := ctx.Value(verificationReportKey{}).(*verificationReport); ok {
		report.err = err
	}
}
