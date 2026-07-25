// Package jwt provides strict JWT and JWK authentication using lestrrat-go/jwx.
package jwt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	defaultMaxTokenBytes = 16 * 1024
	defaultMaxKeys       = 64
)

var registeredClaims = map[string]struct{}{
	"aud": {}, "exp": {}, "iat": {}, "iss": {}, "jti": {}, "nbf": {}, "sub": {},
}

// Clock supplies validation time and permits deterministic tests.
//
// Deprecated: depend on clock.Clock in new code. This named compatibility
// contract remains available throughout v1.
type Clock interface {
	clockpkg.Clock
}

// Config defines a strict JWT trust boundary.
type Config struct {
	Issuer        string
	Audience      string
	Algorithms    []jwa.SignatureAlgorithm
	KeySet        jwk.Set
	Provider      KeyProvider
	Clock         Clock
	Skew          time.Duration
	MaxTokenBytes int
	MaxClaims     int
	MaxClaimDepth int
	MaxKeys       int
	ScopeClaim    string
	TenantClaim   string
}

// Validator authenticates signed compact JWT bearer credentials.
type Validator struct {
	issuer        string
	audience      string
	algorithms    map[string]struct{}
	keys          jwk.Set
	provider      KeyProvider
	clock         Clock
	skew          time.Duration
	maxTokenBytes int
	maxClaims     int
	maxClaimDepth int
	maxKeys       int
	scopeClaim    string
	tenantClaim   string
}

// New validates and defensively copies a static JWK trust configuration.
func New(configuration Config) (*Validator, error) {
	applyDefaults(&configuration)
	algorithms, err := validateConfig(configuration)
	if err != nil {
		return nil, err
	}
	var keys jwk.Set
	if configuration.KeySet != nil {
		keys, err = copyAndValidateKeySet(configuration.KeySet, algorithms, configuration.MaxKeys)
		if err != nil {
			return nil, err
		}
	}

	return &Validator{
		issuer: configuration.Issuer, audience: configuration.Audience,
		algorithms: algorithms, keys: keys, provider: configuration.Provider,
		clock: configuration.Clock,
		skew:  configuration.Skew, maxTokenBytes: configuration.MaxTokenBytes,
		maxClaims: configuration.MaxClaims, maxClaimDepth: configuration.MaxClaimDepth,
		maxKeys:    configuration.MaxKeys,
		scopeClaim: configuration.ScopeClaim, tenantClaim: configuration.TenantClaim,
	}, nil
}

// Authenticate validates a bearer credential and returns a JWT principal.
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

// ValidateBearer verifies a bounded compact JWT and constructs an immutable principal.
func (v *Validator) ValidateBearer(ctx context.Context, token string) (authentication.Principal, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if token == "" || len(token) > v.maxTokenBytes {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if err := inspectCompactJWT(token, v.algorithms, v.maxClaims, v.maxClaimDepth); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}

	keys, err := v.keySet(ctx)
	if err != nil {
		return authentication.Principal{}, err
	}
	parsed, err := upstreamjwt.Parse([]byte(token),
		upstreamjwt.WithKeySet(keys),
		upstreamjwt.WithIssuer(v.issuer),
		upstreamjwt.WithAudience(v.audience),
		upstreamjwt.WithClock(v.clock),
		upstreamjwt.WithAcceptableSkew(v.skew),
		upstreamjwt.WithContext(ctx),
		upstreamjwt.WithPedantic(true),
		upstreamjwt.WithStrictStringClaims(true),
		upstreamjwt.WithRequiredClaim("sub"),
		upstreamjwt.WithRequiredClaim("iss"),
		upstreamjwt.WithRequiredClaim("aud"),
		upstreamjwt.WithRequiredClaim("iat"),
		upstreamjwt.WithRequiredClaim("exp"),
	)
	if err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}

	principal, err := v.principal(parsed)
	if err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}
	return principal, nil
}

// KeyProvider returns a current read-only JWK set for one validation attempt.
type KeyProvider interface {
	KeySet(context.Context) (jwk.Set, error)
}

// KeyProviderFunc adapts a function to KeyProvider.
type KeyProviderFunc func(context.Context) (jwk.Set, error)

// KeySet calls f.
func (f KeyProviderFunc) KeySet(ctx context.Context) (jwk.Set, error) { return f(ctx) }

func (v *Validator) keySet(ctx context.Context) (jwk.Set, error) {
	if v.keys != nil {
		return v.keys, nil
	}
	keys, err := v.provider.KeySet(ctx)
	if err != nil {
		if errors.Is(err, authentication.ErrAuthenticationUnavailable) {
			return nil, err
		}
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	copied, err := copyAndValidateKeySet(keys, v.algorithms, v.maxKeys)
	if err != nil {
		return nil, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	return copied, nil
}

func (v *Validator) principal(token upstreamjwt.Token) (authentication.Principal, error) {
	subject, subjectOK := token.Subject()
	issuer, issuerOK := token.Issuer()
	audiences, audiencesOK := token.Audience()
	authenticatedAt, issuedAtOK := token.IssuedAt()
	if !subjectOK || subject == "" || !issuerOK || issuer == "" || !audiencesOK ||
		len(audiences) == 0 || !issuedAtOK {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}

	claims := make(map[string]any)
	for _, name := range token.Keys() {
		if _, registered := registeredClaims[name]; registered || name == v.scopeClaim || name == v.tenantClaim {
			continue
		}
		var value any
		// Keys reports only values that Get can decode into the empty interface.
		_ = token.Get(name, &value)
		claims[name] = value
	}
	scopes, err := stringClaim(token, v.scopeClaim, true)
	if err != nil {
		return authentication.Principal{}, err
	}
	tenants, err := stringClaim(token, v.tenantClaim, false)
	if err != nil {
		return authentication.Principal{}, err
	}

	return authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: subject, Method: "jwt", Issuer: issuer,
		Audiences: audiences, TenantHints: tenants, Scopes: scopes,
		Claims: claims, AuthenticatedAt: authenticatedAt,
	})
}

func stringClaim(token upstreamjwt.Token, name string, splitSpaces bool) ([]string, error) {
	if name == "" || !token.Has(name) {
		return nil, nil
	}
	var value any
	// Has guarantees the value exists, and any accepts every decoded claim type.
	_ = token.Get(name, &value)
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
	if configuration.MaxTokenBytes == 0 {
		configuration.MaxTokenBytes = defaultMaxTokenBytes
	}
	if configuration.MaxClaims == 0 {
		configuration.MaxClaims = authentication.MaxClaims
	}
	if configuration.MaxClaimDepth == 0 {
		configuration.MaxClaimDepth = authentication.MaxClaimDepth
	}
	if configuration.MaxKeys == 0 {
		configuration.MaxKeys = defaultMaxKeys
	}
	if configuration.ScopeClaim == "" {
		configuration.ScopeClaim = "scope"
	}
	if configuration.TenantClaim == "" {
		configuration.TenantClaim = "tenant"
	}
}

func validateConfig(configuration Config) (map[string]struct{}, error) {
	keySetConfigured := configuration.KeySet != nil
	providerConfigured := !isNilProvider(configuration.Provider)
	if configuration.Issuer == "" || configuration.Audience == "" || configuration.Clock == nil ||
		keySetConfigured == providerConfigured || (keySetConfigured && configuration.KeySet.Len() == 0) ||
		configuration.MaxTokenBytes <= 0 || configuration.MaxClaims <= 0 ||
		configuration.MaxClaims > authentication.MaxClaims || configuration.MaxClaimDepth <= 0 ||
		configuration.MaxClaimDepth > authentication.MaxClaimDepth || configuration.MaxKeys <= 0 ||
		configuration.Skew < 0 || configuration.ScopeClaim == configuration.TenantClaim {
		return nil, fmt.Errorf("%w: JWT configuration", authentication.ErrInvalidConfiguration)
	}
	if len(configuration.Algorithms) == 0 {
		return nil, fmt.Errorf("%w: JWT algorithms", authentication.ErrInvalidConfiguration)
	}
	allowed := make(map[string]struct{}, len(configuration.Algorithms))
	for _, algorithm := range configuration.Algorithms {
		name := algorithm.String()
		known, exists := jwa.LookupSignatureAlgorithm(name)
		if !exists || name == jwa.NoSignature().String() || known.IsDeprecated() {
			return nil, fmt.Errorf("%w: JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		if _, duplicate := allowed[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		allowed[name] = struct{}{}
	}
	return allowed, nil
}

func isNilProvider(provider KeyProvider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func copyAndValidateKeySet(source jwk.Set, algorithms map[string]struct{}, maximum int) (jwk.Set, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("%w: JWK encoding", authentication.ErrInvalidConfiguration)
	}
	copied, err := jwk.Parse(encoded, jwk.WithRejectDuplicateKID(true))
	if err != nil || copied.Len() == 0 || copied.Len() > maximum {
		return nil, fmt.Errorf("%w: JWK set", authentication.ErrInvalidConfiguration)
	}
	for index := 0; index < copied.Len(); index++ {
		key, _ := copied.Key(index)
		keyID, hasKeyID := key.KeyID()
		algorithm, hasAlgorithm := key.Algorithm()
		if !hasKeyID || keyID == "" || !hasAlgorithm {
			return nil, fmt.Errorf("%w: JWK identity", authentication.ErrInvalidConfiguration)
		}
		if _, allowed := algorithms[algorithm.String()]; !allowed {
			return nil, fmt.Errorf("%w: JWK algorithm", authentication.ErrInvalidConfiguration)
		}
		if key.Validate() != nil || !keyTypeMatchesAlgorithm(key.KeyType(), algorithm.String()) {
			return nil, fmt.Errorf("%w: JWK key type", authentication.ErrInvalidConfiguration)
		}
		if usage, exists := key.KeyUsage(); exists && usage != "sig" {
			return nil, fmt.Errorf("%w: JWK usage", authentication.ErrInvalidConfiguration)
		}
		if operations, exists := key.KeyOps(); exists && !containsVerifyOperation(operations) {
			return nil, fmt.Errorf("%w: JWK operation", authentication.ErrInvalidConfiguration)
		}
	}
	return copied, nil
}

func keyTypeMatchesAlgorithm(keyType jwa.KeyType, algorithm string) bool {
	switch {
	case strings.HasPrefix(algorithm, "HS"):
		return keyType == jwa.OctetSeq()
	case strings.HasPrefix(algorithm, "RS"), strings.HasPrefix(algorithm, "PS"):
		return keyType == jwa.RSA()
	case strings.HasPrefix(algorithm, "ES"):
		return keyType == jwa.EC()
	case algorithm == "EdDSA", algorithm == "Ed25519":
		return keyType == jwa.OKP()
	default:
		return false
	}
}

func containsVerifyOperation(operations jwk.KeyOperationList) bool {
	for _, operation := range operations {
		if operation == jwk.KeyOpVerify {
			return true
		}
	}
	return false
}

func inspectCompactJWT(token string, algorithms map[string]struct{}, maxClaims, maxDepth int) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return errors.New("invalid compact JWT")
	}
	header, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return err
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return err
	}
	if err := inspectJSONObject(header, 64, 4); err != nil {
		return err
	}
	if err := inspectJSONObject(payload, maxClaims, maxDepth); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	// inspectJSONObject has already proven that header is valid JSON.
	_ = json.Unmarshal(header, &fields)
	var algorithm, keyID string
	if err := json.Unmarshal(fields["alg"], &algorithm); err != nil || algorithm == "" {
		return errors.New("invalid JWT algorithm")
	}
	if _, allowed := algorithms[algorithm]; !allowed {
		return errors.New("disallowed JWT algorithm")
	}
	if err := json.Unmarshal(fields["kid"], &keyID); err != nil || keyID == "" {
		return errors.New("invalid JWT key ID")
	}
	if _, critical := fields["crit"]; critical {
		return errors.New("unsupported critical JWT header")
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
			return errors.New("JWT JSON value is not an object")
		}
		return nil
	}
	depth++
	if depth > maxDepth {
		return errors.New("JWT JSON depth exceeded")
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
				return errors.New("duplicate JWT member")
			}
			seen[name] = struct{}{}
			if len(seen) > maxMembers {
				return errors.New("JWT member bound exceeded")
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
				return errors.New("JWT collection bound exceeded")
			}
			if err := inspectJSONValue(decoder, depth, maxMembers, maxDepth, false); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JWT JSON delimiter")
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if top && delimiter != '{' {
		return errors.New("JWT JSON value is not an object")
	}
	return nil
}

var _ authentication.Authenticator = (*Validator)(nil)
