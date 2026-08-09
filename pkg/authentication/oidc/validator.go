// Package oidc provides strict OpenID Connect ID-token authentication using
// coreos/go-oidc and go-jose.
package oidc

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

const defaultMaxTokenBytes = 16 * 1024

const (
	maximumTokenBytes      = 1 << 20
	maximumHTTPBodyBytes   = 16 << 20
	maximumDiscoveryWait   = 5 * time.Minute
	maximumJWKCount        = 4096
	maximumRefreshInterval = 24 * time.Hour
	maximumRefreshWaiters  = 4096
)

var supportedAlgorithms = map[string]struct{}{
	"RS256": {}, "RS384": {}, "RS512": {},
	"PS256": {}, "PS384": {}, "PS512": {},
	"ES256": {}, "ES384": {}, "ES512": {},
	"EdDSA": {},
}

var registeredClaims = map[string]struct{}{
	"at_hash": {}, "aud": {}, "auth_time": {}, "azp": {}, "c_hash": {}, "exp": {},
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

// TokenBinding supplies front-channel values that an ID token cryptographically
// binds through at_hash or c_hash. Values are used only during the call.
type TokenBinding struct {
	AccessToken       string
	AuthorizationCode string
}

// Config defines a strict OIDC ID-token trust boundary.
type Config struct {
	Issuer           string
	ClientID         string
	TrustedAudiences []string
	Algorithms       []string
	Clock            Clock
	ClockSkew        time.Duration
	NonceValidator   NonceValidator
	MaxTokenBytes    int
	MaxClaims        int
	MaxClaimDepth    int
	ScopeClaim       string
	TenantClaim      string
	InsecureHTTP     bool

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
	verifier         *upstreamoidc.IDTokenVerifier
	issuer           string
	clientID         string
	trustedAudiences map[string]struct{}
	algorithms       map[string]struct{}
	clock            Clock
	clockSkew        time.Duration
	nonceValidator   NonceValidator
	maxTokenBytes    int
	maxClaims        int
	maxClaimDepth    int
	scopeClaim       string
	tenantClaim      string
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
		verifier: verifier, issuer: configuration.Issuer, clientID: configuration.ClientID,
		trustedAudiences: trustedAudienceSet(configuration),
		algorithms:       algorithms, clock: configuration.Clock, clockSkew: configuration.ClockSkew,
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
	if !ok {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if bearer.Token() == "" {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if len(bearer.Token()) > v.maxTokenBytes {
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
	return v.validateIDToken(ctx, rawToken, TokenBinding{})
}

// ValidateIDToken verifies an ID token and any supplied access-token or
// authorization-code hash bindings.
func (v *Validator) ValidateIDToken(
	ctx context.Context,
	rawToken string,
	binding TokenBinding,
) (authentication.Principal, error) {
	return v.validateIDToken(ctx, rawToken, binding)
}

func (v *Validator) validateIDToken(
	ctx context.Context,
	rawToken string,
	binding TokenBinding,
) (authentication.Principal, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if rawToken == "" {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if len(rawToken) > v.maxTokenBytes {
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
	if err := verifyTokenBinding(rawToken, token, binding); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}
	principal, err := v.principal(ctx, token)
	if err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}
	return principal, nil
}

func verifyTokenBinding(rawToken string, token *upstreamoidc.IDToken, binding TokenBinding) error {
	if binding.AccessToken == "" && binding.AuthorizationCode == "" {
		return nil
	}
	algorithm, err := compactTokenAlgorithm(rawToken)
	if err != nil {
		return authentication.ErrInvalidPrincipal
	}
	var hashes struct {
		AccessToken       string `json:"at_hash"`
		AuthorizationCode string `json:"c_hash"`
	}
	if err := token.Claims(&hashes); err != nil {
		return authentication.ErrInvalidPrincipal
	}
	if binding.AccessToken != "" && !validTokenHash(algorithm, binding.AccessToken, hashes.AccessToken) {
		return authentication.ErrInvalidPrincipal
	}
	if binding.AuthorizationCode != "" && !validTokenHash(algorithm, binding.AuthorizationCode, hashes.AuthorizationCode) {
		return authentication.ErrInvalidPrincipal
	}
	return nil
}

func compactTokenAlgorithm(rawToken string) (string, error) {
	encoded, _, found := strings.Cut(rawToken, ".")
	if !found {
		return "", authentication.ErrInvalidPrincipal
	}
	header, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return "", authentication.ErrInvalidPrincipal
	}
	var fields struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(header, &fields); err != nil || fields.Algorithm == "" {
		return "", authentication.ErrInvalidPrincipal
	}
	return fields.Algorithm, nil
}

func validTokenHash(algorithm, value, expected string) bool {
	var digest []byte
	switch {
	case strings.HasSuffix(algorithm, "256"):
		sum := sha256.Sum256([]byte(value))
		digest = sum[:]
	case strings.HasSuffix(algorithm, "384"):
		sum := sha512.Sum384([]byte(value))
		digest = sum[:]
	case strings.HasSuffix(algorithm, "512"):
		sum := sha512.Sum512([]byte(value))
		digest = sum[:]
	default:
		return false
	}
	actual := base64.RawURLEncoding.EncodeToString(digest[:len(digest)/2])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func (v *Validator) principal(ctx context.Context, token *upstreamoidc.IDToken) (authentication.Principal, error) {
	now := v.clock.Now()
	if token.Issuer != v.issuer {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if token.IssuedAt.IsZero() {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if token.Expiry.IsZero() {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if !token.Expiry.After(now.Add(-v.clockSkew)) {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	if token.IssuedAt.After(now.Add(v.clockSkew)) {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}
	var rawClaims map[string]any
	// Verify and inspectCompactToken have already proven valid JSON claims.
	_ = token.Claims(&rawClaims)
	var protocol struct {
		AuthorizedParty string          `json:"azp"`
		AuthTime        json.RawMessage `json:"auth_time"`
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
	seenAudiences := make(map[string]struct{}, len(token.Audience))
	for _, audience := range token.Audience {
		if _, duplicate := seenAudiences[audience]; duplicate {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
		if _, trusted := v.trustedAudiences[audience]; !trusted {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
		seenAudiences[audience] = struct{}{}
	}
	if v.nonceValidator != nil {
		if err := validateNonce(ctx, v.nonceValidator, token.Nonce); err != nil {
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
		if !v.excludedPrincipalClaim(name) {
			claims[name] = value
		}
	}
	authenticatedAt := token.IssuedAt
	if len(protocol.AuthTime) > 0 {
		parsedAuthTime, err := numericDate(protocol.AuthTime)
		if err != nil || parsedAuthTime.After(now.Add(v.clockSkew)) {
			return authentication.Principal{}, authentication.ErrInvalidPrincipal
		}
		authenticatedAt = parsedAuthTime
	}

	return authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: token.Subject, Method: "oidc", Issuer: token.Issuer,
		Audiences: token.Audience, TenantHints: tenants, Scopes: scopes,
		Claims: claims, AuthenticatedAt: authenticatedAt,
	})
}

func validateNonce(ctx context.Context, validator NonceValidator, nonce string) (err error) {
	defer func() {
		if recover() != nil {
			err = authentication.ErrInvalidPrincipal
		}
	}()
	return validator.ValidateNonce(ctx, nonce)
}

func (v *Validator) excludedPrincipalClaim(name string) bool {
	if _, registered := registeredClaims[name]; registered {
		return true
	}
	return slices.Contains([]string{v.scopeClaim, v.tenantClaim}, name)
}

func numericDate(encoded json.RawMessage) (time.Time, error) {
	var seconds float64
	if err := json.Unmarshal(encoded, &seconds); err != nil {
		return time.Time{}, authentication.ErrInvalidPrincipal
	}
	if seconds < -62135596800 {
		return time.Time{}, authentication.ErrInvalidPrincipal
	}
	if seconds > 253402300799 {
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
	if err != nil {
		return nil, fmt.Errorf("%w: OIDC configuration", authentication.ErrInvalidConfiguration)
	}
	if !validIssuerURL(issuer, configuration.InsecureHTTP) {
		return nil, fmt.Errorf("%w: OIDC configuration", authentication.ErrInvalidConfiguration)
	}
	if !validConfigLimits(configuration) {
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
	trusted := map[string]struct{}{configuration.ClientID: {}}
	for _, audience := range configuration.TrustedAudiences {
		if audience == "" {
			return nil, fmt.Errorf("%w: OIDC trusted audience", authentication.ErrInvalidConfiguration)
		}
		if _, duplicate := trusted[audience]; duplicate {
			return nil, fmt.Errorf("%w: duplicate OIDC trusted audience", authentication.ErrInvalidConfiguration)
		}
		trusted[audience] = struct{}{}
	}
	return allowed, nil
}

func trustedAudienceSet(configuration Config) map[string]struct{} {
	trusted := make(map[string]struct{}, len(configuration.TrustedAudiences)+1)
	trusted[configuration.ClientID] = struct{}{}
	for _, audience := range configuration.TrustedAudiences {
		trusted[audience] = struct{}{}
	}
	return trusted
}

func validIssuerURL(issuer *url.URL, allowHTTP bool) bool {
	if issuer.Host == "" {
		return false
	}
	if issuer.User != nil {
		return false
	}
	if issuer.RawQuery != "" {
		return false
	}
	if issuer.Fragment != "" {
		return false
	}
	switch issuer.Scheme {
	case "https":
		return true
	case "http":
		return allowHTTP
	default:
		return false
	}
}

func validConfigLimits(configuration Config) bool {
	return !slices.Contains([]bool{
		configuration.ClientID != "",
		configuration.Clock != nil,
		cmp.Compare(configuration.ClockSkew, time.Duration(0)) != -1,
		cmp.Compare(configuration.ClockSkew, 24*time.Hour) != 1,
		cmp.Compare(configuration.MaxTokenBytes, 0) == 1,
		cmp.Compare(configuration.MaxTokenBytes, maximumTokenBytes) != 1,
		cmp.Compare(configuration.MaxClaims, 0) == 1,
		cmp.Compare(configuration.MaxClaims, authentication.MaxClaims) != 1,
		cmp.Compare(configuration.MaxClaimDepth, 0) == 1,
		cmp.Compare(configuration.MaxClaimDepth, authentication.MaxClaimDepth) != 1,
		cmp.Compare(configuration.MaxHTTPBodyBytes, int64(0)) == 1,
		cmp.Compare(configuration.MaxHTTPBodyBytes, int64(maximumHTTPBodyBytes)) != 1,
		cmp.Compare(configuration.DiscoveryTimeout, time.Duration(0)) == 1,
		cmp.Compare(configuration.DiscoveryTimeout, maximumDiscoveryWait) != 1,
		cmp.Compare(configuration.MaxKeys, 0) == 1,
		cmp.Compare(configuration.MaxKeys, maximumJWKCount) != 1,
		cmp.Compare(configuration.MinRefreshInterval, time.Duration(0)) == 1,
		cmp.Compare(configuration.MinRefreshInterval, maximumRefreshInterval) != 1,
		cmp.Compare(configuration.MaxRefreshInterval, configuration.MinRefreshInterval) != -1,
		cmp.Compare(configuration.MaxRefreshInterval, maximumRefreshInterval) != 1,
		cmp.Compare(configuration.MaxRefreshWaiters, 0) == 1,
		cmp.Compare(configuration.MaxRefreshWaiters, maximumRefreshWaiters) != 1,
		configuration.ScopeClaim != configuration.TenantClaim,
		len(configuration.Algorithms) != 0,
		configuration.NonceValidator == nil || !isNilNonceValidator(configuration.NonceValidator),
	}, false)
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
	if _, err := decoder.Token(); err == nil {
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
	depth = depth + 1
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
			count = count + 1
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
