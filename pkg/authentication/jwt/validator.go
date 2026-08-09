// Package jwt provides strict JWT and JWK authentication using lestrrat-go/jwx.
package jwt

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	defaultMaxTokenBytes = 16 * 1024
	defaultMaxKeys       = 64
	maxJSONNumberBytes   = 128
	minimumRSAKeyBits    = 2048
	maximumRSAKeyBits    = 8192
)

var registeredClaims = map[string]struct{}{
	"aud": {}, "exp": {}, "iat": {}, "iss": {}, "jti": {}, "nbf": {}, "sub": {},
}

var prohibitedKeyReferenceHeaders = []string{"jku", "jwk", "x5c", "x5t", "x5t#S256", "x5u"}

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

// ValidateBearer verifies a bounded compact JWT and constructs an immutable principal.
func (v *Validator) ValidateBearer(ctx context.Context, token string) (authentication.Principal, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if token == "" {
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureInvalid)
	}
	if len(token) > v.maxTokenBytes {
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
	if slices.Contains([]bool{
		subjectOK, subject != "", issuerOK, issuer != "", audiencesOK,
		len(audiences) != 0, issuedAtOK,
	}, false) {
		return authentication.Principal{}, authentication.ErrInvalidPrincipal
	}

	claims := make(map[string]any)
	for _, name := range token.Keys() {
		if !v.excludedPrincipalClaim(name) {
			var value any
			// Keys reports only values that Get can decode into the empty interface.
			_ = token.Get(name, &value)
			claims[name] = value
		}
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

func (v *Validator) excludedPrincipalClaim(name string) bool {
	if _, registered := registeredClaims[name]; registered {
		return true
	}
	return slices.Contains([]string{v.scopeClaim, v.tenantClaim}, name)
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
	validKeySource := validConfiguredKeySource(configuration, keySetConfigured, providerConfigured)
	if slices.Contains([]bool{
		configuration.Issuer != "", configuration.Audience != "", configuration.Clock != nil,
		validKeySource,
		cmp.Compare(configuration.MaxTokenBytes, 0) == 1,
		cmp.Compare(configuration.MaxClaims, 0) == 1,
		cmp.Compare(configuration.MaxClaims, authentication.MaxClaims) != 1,
		cmp.Compare(configuration.MaxClaimDepth, 0) == 1,
		cmp.Compare(configuration.MaxClaimDepth, authentication.MaxClaimDepth) != 1,
		cmp.Compare(configuration.MaxKeys, 0) == 1,
		cmp.Compare(configuration.Skew, time.Duration(0)) != -1,
		configuration.ScopeClaim != configuration.TenantClaim,
	}, false) {
		return nil, fmt.Errorf("%w: JWT configuration", authentication.ErrInvalidConfiguration)
	}
	if len(configuration.Algorithms) == 0 {
		return nil, fmt.Errorf("%w: JWT algorithms", authentication.ErrInvalidConfiguration)
	}
	allowed := make(map[string]struct{}, len(configuration.Algorithms))
	for _, algorithm := range configuration.Algorithms {
		name := algorithm.String()
		known, exists := jwa.LookupSignatureAlgorithm(name)
		if !exists {
			return nil, fmt.Errorf("%w: JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		if name == jwa.NoSignature().String() {
			return nil, fmt.Errorf("%w: JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		if known.IsDeprecated() {
			return nil, fmt.Errorf("%w: JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		if _, duplicate := allowed[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate JWT algorithm", authentication.ErrInvalidConfiguration)
		}
		allowed[name] = struct{}{}
	}
	return allowed, nil
}

func validConfiguredKeySource(configuration Config, keySetConfigured, providerConfigured bool) bool {
	if keySetConfigured == providerConfigured {
		return false
	}
	if keySetConfigured {
		return configuration.KeySet.Len() != 0
	}
	return true
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
	if err != nil {
		return nil, fmt.Errorf("%w: JWK set", authentication.ErrInvalidConfiguration)
	}
	if copied.Len() == 0 {
		return nil, fmt.Errorf("%w: JWK set", authentication.ErrInvalidConfiguration)
	}
	if copied.Len() > maximum {
		return nil, fmt.Errorf("%w: JWK set", authentication.ErrInvalidConfiguration)
	}
	if err := validateJWKEntries(copied, algorithms); err != nil {
		return nil, fmt.Errorf("%w: JWK key", authentication.ErrInvalidConfiguration)
	}
	return copied, nil
}

func validateJWKEntries(set jwk.Set, algorithms map[string]struct{}) error {
	for index := 0; index < set.Len(); index++ {
		key, err := requiredJWKAt(set, index)
		if err != nil {
			return err
		}
		keyID, hasKeyID := key.KeyID()
		algorithm, err := requiredJWKAlgorithm(key)
		if slices.Contains([]bool{hasKeyID, keyID != "", err == nil}, false) {
			return errors.New("JWK identity is invalid")
		}
		if algorithms != nil {
			if _, allowed := algorithms[algorithm]; !allowed {
				return errors.New("JWK algorithm is not allowed")
			}
		}
		if !keyTypeMatchesAlgorithm(key.KeyType(), algorithm) {
			return errors.New("JWK key type does not match its algorithm")
		}
		if err := validateKeyMaterial(key, algorithm); err != nil {
			return err
		}
		if usage, exists := key.KeyUsage(); exists && usage != "sig" {
			return errors.New("JWK usage is invalid")
		}
		if operations, exists := key.KeyOps(); exists && !containsVerifyOperation(operations) {
			return errors.New("JWK operation is invalid")
		}
	}
	return nil
}

func requiredJWKAt(set jwk.Set, index int) (jwk.Key, error) {
	key, exists := set.Key(index)
	if !exists || key == nil {
		return nil, errors.New("JWK is missing")
	}
	return key, nil
}

func requiredJWKAlgorithm(key jwk.Key) (string, error) {
	algorithm, exists := key.Algorithm()
	if !exists || algorithm == nil {
		return "", errors.New("JWK algorithm is missing")
	}
	return algorithm.String(), nil
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

func validateKeyMaterial(key jwk.Key, algorithm string) error {
	if err := key.Validate(); err != nil {
		return err
	}
	private, hasPrivateState := key.(interface{ IsPrivate() bool })
	if hasPrivateState && private.IsPrivate() && key.KeyType() != jwa.OctetSeq() {
		return errors.New("private asymmetric verification key")
	}

	switch {
	case strings.HasPrefix(algorithm, "HS"):
		symmetric, ok := key.(jwk.SymmetricKey)
		if !ok {
			return errors.New("invalid HMAC key")
		}
		octets, ok := symmetric.Octets()
		minimum := map[string]int{"HS256": 32, "HS384": 48, "HS512": 64}[algorithm]
		if !ok || minimum == 0 || len(octets) < minimum {
			return errors.New("insufficient HMAC key strength")
		}
	case strings.HasPrefix(algorithm, "RS"), strings.HasPrefix(algorithm, "PS"):
		public, ok := key.(jwk.RSAPublicKey)
		if !ok {
			return errors.New("invalid RSA verification key")
		}
		modulus, ok := public.N()
		bits := significantBits(modulus)
		if !ok || bits < minimumRSAKeyBits || bits > maximumRSAKeyBits {
			return errors.New("unsafe RSA modulus")
		}
	case strings.HasPrefix(algorithm, "ES"):
		public, ok := key.(jwk.ECDSAPublicKey)
		if !ok {
			return errors.New("invalid ECDSA verification key")
		}
		curve, ok := public.Crv()
		expected := map[string]string{
			"ES256": jwa.P256().String(), "ES256K": "secp256k1",
			"ES384": jwa.P384().String(), "ES512": jwa.P521().String(),
		}[algorithm]
		if !ok || expected == "" || curve.String() != expected {
			return errors.New("ECDSA curve does not match algorithm")
		}
	case algorithm == "Ed25519":
		public, ok := key.(jwk.OKPPublicKey)
		if !ok {
			return errors.New("invalid Ed25519 verification key")
		}
		curve, ok := public.Crv()
		if !ok || curve != jwa.Ed25519() {
			return errors.New("OKP curve does not match algorithm")
		}
	default:
		return errors.New("unsupported key algorithm")
	}
	return nil
}

func significantBits(encoded []byte) int {
	for index, value := range encoded {
		if value != 0 {
			return (len(encoded)-index-1)*8 + 8 - bits.LeadingZeros8(value)
		}
	}
	return 0
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
	if err := json.Unmarshal(fields["alg"], &algorithm); err != nil {
		return errors.New("invalid JWT algorithm")
	}
	if algorithm == "" {
		return errors.New("invalid JWT algorithm")
	}
	if _, allowed := algorithms[algorithm]; !allowed {
		return errors.New("disallowed JWT algorithm")
	}
	if err := json.Unmarshal(fields["kid"], &keyID); err != nil {
		return errors.New("invalid JWT key ID")
	}
	if keyID == "" {
		return errors.New("invalid JWT key ID")
	}
	if _, critical := fields["crit"]; critical {
		return errors.New("unsupported critical JWT header")
	}
	for _, name := range prohibitedKeyReferenceHeaders {
		if _, exists := fields[name]; exists {
			return errors.New("unsupported JWT key reference header")
		}
	}
	return nil
}

func inspectJSONObject(encoded []byte, maxMembers, maxDepth int) error {
	if !utf8.Valid(encoded) {
		return errors.New("invalid UTF-8 in JWT JSON")
	}
	if err := validateJSONUnicodeEscapes(encoded); err != nil {
		return err
	}
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
			return errors.New("JWT JSON value is not an object")
		}
		if number, ok := token.(json.Number); ok && len(number.String()) > maxJSONNumberBytes {
			return errors.New("JWT JSON number bound exceeded")
		}
		return nil
	}
	depth = depth + 1
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
			count = count + 1
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

func validateJSONUnicodeEscapes(encoded []byte) error {
	insideString := false
	escaped := false
	skipUntil := 0
	for index, character := range encoded {
		if index < skipUntil {
			continue
		}
		if !insideString {
			if character == '"' {
				insideString = true
			}
			continue
		}
		if !escaped {
			switch character {
			case '\\':
				escaped = true
			case '"':
				insideString = false
			}
			continue
		}
		escaped = false
		if character != 'u' {
			continue
		}
		value, ok := decodeHexQuad(encoded[index+1:])
		if !ok {
			continue
		}
		switch {
		case value >= 0xD800 && value <= 0xDBFF:
			if index+11 > len(encoded) {
				return errors.New("invalid Unicode surrogate in JWT JSON")
			}
			if encoded[index+5] != '\\' {
				return errors.New("invalid Unicode surrogate in JWT JSON")
			}
			if encoded[index+6] != 'u' {
				return errors.New("invalid Unicode surrogate in JWT JSON")
			}
			low, valid := decodeHexQuad(encoded[index+7:])
			if !valid || low < 0xDC00 || low > 0xDFFF {
				return errors.New("invalid Unicode surrogate in JWT JSON")
			}
			skipUntil = index + 11
		case value >= 0xDC00 && value <= 0xDFFF:
			return errors.New("invalid Unicode surrogate in JWT JSON")
		}
	}
	return nil
}

func decodeHexQuad(encoded []byte) (uint16, bool) {
	if len(encoded) < 4 {
		return 0, false
	}
	var value uint16
	for _, character := range encoded[:4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value |= uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value |= uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value |= uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

var _ authentication.Authenticator = (*Validator)(nil)
