package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func TestRFC7520HMACJWKInteroperability(t *testing.T) {
	t.Parallel()

	// RFC 7520, Figure 5: HMAC SHA-256 symmetric JWK.
	keys, err := jwk.Parse([]byte(`{"keys":[{"kty":"oct","kid":"018c0ae5-4d9b-471b-bfd6-eef314bc7037","use":"sig","alg":"HS256","k":"hJtXIZ2uSN5kbQfbtTNWbpdmhkV8FJG-Onbc6mxCcYg"}]}`))
	if err != nil {
		t.Fatalf("Parse(RFC 7520 JWK) error = %v", err)
	}
	signer, ok := keys.Key(0)
	if !ok {
		t.Fatal("RFC 7520 JWK is missing")
	}
	now := time.Unix(1_311_281_000, 0).UTC()
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://server.example.com", Audience: "s6BhdRkqt3",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token := upstreamjwt.New()
	for name, value := range map[string]any{
		"sub": "24400320", "iss": "https://server.example.com", "aud": "s6BhdRkqt3",
		"iat": now, "exp": now.Add(time.Hour),
	} {
		if err := token.Set(name, value); err != nil {
			t.Fatalf("Set(%s) error = %v", name, err)
		}
	}
	signed, err := upstreamjwt.Sign(token, upstreamjwt.WithKey(jwa.HS256(), signer))
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(string(signed)))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "24400320" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestRFC7515AppendixA2RS256CompactJWS(t *testing.T) {
	t.Parallel()

	const compact = "eyJhbGciOiJSUzI1NiJ9." +
		"eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ." +
		"cC4hiUPoj9Eetdgtv3hF80EGrhuB__dzERat0XF9g2VtQgr9PJbu3XOiZj5RZmh7" +
		"AAuHIm4Bh-0Qc_lF5YKt_O8W2Fp5jujGbds9uJdbF9CUAr7t1dnZcAcQjbKBYNX4" +
		"BAynRFdiuB--f_nZLgrnbyTyWzO75vRK5h6xBArLIARNPvkSjtQBMHlb1L07Qe7K" +
		"0GarZRmB_eSN9383LcOLn6_dO--xi12jzDwusC-eOkHWEsqtFZESc6BfI7noOPqv" +
		"hJ1phCnvWh6IeYI2w9QOYEUipUTI8np6LbgGY9Fs98rqVt5AXLIhWkWywlVmtVrB" +
		"p0igcN_IoypGlUPQGe77Rw"
	key, err := jwk.ParseKey([]byte(`{"kty":"RSA","kid":"rfc7515-a2","alg":"RS256","e":"AQAB","n":"ofgWCuLjybRlzo0tZWJjNiuSfb4p4fAkd_wWJcyQoTbji9k0l8W26mPddxHmfHQp-Vaw-4qPCJrcS2mJPMEzP1Pt0Bm4d4QlL-yRT-SFd2lZS-pCgNMsD1W_YpRPEwOWvG6b32690r2jZ47soMZo9wGzjb_7OMg0LOL-bSf63kpaSHSXndS5z5rexMdbBYUsLA9e-KXBdQOS-UTo7WTBEMa2R2CapHg665xsmtdVMTBQY4uDZlxvb3qCo5ZwKh9kG4LT6_I5IhlJH7aGhyxXFvUK-DWNmoudF8NAco9_h9iaGNj8q2ethFkMLs91kzk2PAcDTW9gb54h4FRWyuXpoQ"}`))
	if err != nil {
		t.Fatalf("jwk.ParseKey() error = %v", err)
	}
	if _, err := jws.Verify([]byte(compact), jws.WithKey(jwa.RS256(), key)); err != nil {
		t.Fatalf("jws.Verify(RFC 7515 A.2) error = %v", err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(key); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "joe", Audience: "orders", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
		KeySet: keys, Clock: authtest.NewClock(time.Unix(1_300_000_000, 0)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := validator.ValidateBearer(context.Background(), compact); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(RFC 7515 A.2 JWS) error = %v, want rejected JWT", err)
	}
}

func TestGolangJWTInteroperability(t *testing.T) {
	t.Parallel()

	secret := []byte("01234567890123456789012345678901")
	key, err := jwk.Import(secret)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, "shared-key"); err != nil {
		t.Fatalf("Set(kid) error = %v", err)
	}
	if err := key.Set(jwk.AlgorithmKey, jwa.HS256()); err != nil {
		t.Fatalf("Set(alg) error = %v", err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(key); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	golangToken := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, golangjwt.MapClaims{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	golangToken.Header["kid"] = "shared-key"
	compact, err := golangToken.SignedString(secret)
	if err != nil {
		t.Fatalf("golang-jwt SignedString() error = %v", err)
	}
	principal, err := validator.ValidateBearer(context.Background(), compact)
	if err != nil {
		t.Fatalf("ValidateBearer(golang-jwt) error = %v", err)
	}
	if principal.Subject() != "service" {
		t.Fatalf("ValidateBearer(golang-jwt) subject = %q", principal.Subject())
	}

	lestrratToken := upstreamjwt.New()
	for name, value := range map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now, "exp": now.Add(time.Hour),
	} {
		if err := lestrratToken.Set(name, value); err != nil {
			t.Fatalf("Set(%s) error = %v", name, err)
		}
	}
	lestrratCompact, err := upstreamjwt.Sign(
		lestrratToken,
		upstreamjwt.WithKey(jwa.HS256(), key),
	)
	if err != nil {
		t.Fatalf("lestrrat-go Sign() error = %v", err)
	}
	parsed, err := golangjwt.Parse(
		string(lestrratCompact),
		func(*golangjwt.Token) (any, error) { return secret, nil },
		golangjwt.WithValidMethods([]string{"HS256"}),
		golangjwt.WithIssuer("https://issuer.example.test"),
		golangjwt.WithAudience("orders"),
		golangjwt.WithSubject("service"),
		golangjwt.WithIssuedAt(),
		golangjwt.WithExpirationRequired(),
		golangjwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("golang-jwt Parse(lestrrat-go) error = %v", err)
	}
	if parsed == nil || !parsed.Valid {
		t.Fatal("golang-jwt Parse(lestrrat-go) token is invalid")
	}
}

func TestGolangJWTAlgorithmInteroperability(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now, "exp": now.Add(time.Hour),
	}
	tests := []struct {
		name      string
		algorithm jwa.SignatureAlgorithm
		method    golangjwt.SigningMethod
		keys      func(*testing.T, jwa.SignatureAlgorithm) (jwk.Set, jwk.Key)
	}{
		{name: "HS256", algorithm: jwa.HS256(), method: golangjwt.SigningMethodHS256, keys: hmacKeys},
		{name: "HS384", algorithm: jwa.HS384(), method: golangjwt.SigningMethodHS384, keys: hmacKeys},
		{name: "HS512", algorithm: jwa.HS512(), method: golangjwt.SigningMethodHS512, keys: hmacKeys},
		{name: "RS256", algorithm: jwa.RS256(), method: golangjwt.SigningMethodRS256, keys: matrixRSAKeys},
		{name: "RS384", algorithm: jwa.RS384(), method: golangjwt.SigningMethodRS384, keys: matrixRSAKeys},
		{name: "RS512", algorithm: jwa.RS512(), method: golangjwt.SigningMethodRS512, keys: matrixRSAKeys},
		{name: "PS256", algorithm: jwa.PS256(), method: golangjwt.SigningMethodPS256, keys: matrixRSAKeys},
		{name: "PS384", algorithm: jwa.PS384(), method: golangjwt.SigningMethodPS384, keys: matrixRSAKeys},
		{name: "PS512", algorithm: jwa.PS512(), method: golangjwt.SigningMethodPS512, keys: matrixRSAKeys},
		{name: "ES256", algorithm: jwa.ES256(), method: golangjwt.SigningMethodES256, keys: ecdsaKeys(elliptic.P256())},
		{name: "ES384", algorithm: jwa.ES384(), method: golangjwt.SigningMethodES384, keys: ecdsaKeys(elliptic.P384())},
		{name: "ES512", algorithm: jwa.ES512(), method: golangjwt.SigningMethodES512, keys: ecdsaKeys(elliptic.P521())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, signer := tt.keys(t, tt.algorithm)
			var raw any
			if err := jwk.Export(signer, &raw); err != nil {
				t.Fatalf("jwk.Export() error = %v", err)
			}
			validator, err := authjwt.New(authjwt.Config{
				Issuer: "https://issuer.example.test", Audience: "orders",
				Algorithms: []jwa.SignatureAlgorithm{tt.algorithm}, KeySet: keys,
				Clock: authtest.NewClock(now),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			peerToken := golangjwt.NewWithClaims(tt.method, golangjwt.MapClaims{
				"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			})
			peerToken.Header["kid"] = "key"
			compact, err := peerToken.SignedString(raw)
			if err != nil {
				t.Fatalf("golang-jwt SignedString() error = %v", err)
			}
			if _, err := validator.ValidateBearer(context.Background(), compact); err != nil {
				t.Fatalf("ValidateBearer(golang-jwt) error = %v", err)
			}

			compact = signedToken(t, signer, tt.algorithm, claims)
			parsed, err := golangjwt.Parse(
				compact,
				func(*golangjwt.Token) (any, error) { return peerVerificationKey(raw), nil },
				golangjwt.WithValidMethods([]string{tt.method.Alg()}),
				golangjwt.WithIssuer("https://issuer.example.test"),
				golangjwt.WithAudience("orders"),
				golangjwt.WithSubject("service"),
				golangjwt.WithIssuedAt(), golangjwt.WithExpirationRequired(),
				golangjwt.WithTimeFunc(func() time.Time { return now }),
			)
			if err != nil || parsed == nil || !parsed.Valid {
				t.Fatalf("golang-jwt Parse(jwx) = %v, %v", parsed, err)
			}
		})
	}
}

func peerVerificationKey(signingKey any) any {
	switch key := signingKey.(type) {
	case *rsa.PrivateKey:
		return &key.PublicKey
	case *ecdsa.PrivateKey:
		return &key.PublicKey
	case ed25519.PrivateKey:
		return key.Public().(ed25519.PublicKey)
	default:
		return key
	}
}

func TestSupportedAlgorithmAndKeyMatrix(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now, "exp": now.Add(time.Hour),
	}
	tests := []struct {
		name      string
		algorithm jwa.SignatureAlgorithm
		keys      func(*testing.T, jwa.SignatureAlgorithm) (jwk.Set, jwk.Key)
	}{
		{name: "HS256", algorithm: jwa.HS256(), keys: hmacKeys},
		{name: "HS384", algorithm: jwa.HS384(), keys: hmacKeys},
		{name: "HS512", algorithm: jwa.HS512(), keys: hmacKeys},
		{name: "RS256", algorithm: jwa.RS256(), keys: matrixRSAKeys},
		{name: "RS384", algorithm: jwa.RS384(), keys: matrixRSAKeys},
		{name: "RS512", algorithm: jwa.RS512(), keys: matrixRSAKeys},
		{name: "PS256", algorithm: jwa.PS256(), keys: matrixRSAKeys},
		{name: "PS384", algorithm: jwa.PS384(), keys: matrixRSAKeys},
		{name: "PS512", algorithm: jwa.PS512(), keys: matrixRSAKeys},
		{name: "ES256", algorithm: jwa.ES256(), keys: ecdsaKeys(elliptic.P256())},
		{name: "ES384", algorithm: jwa.ES384(), keys: ecdsaKeys(elliptic.P384())},
		{name: "ES512", algorithm: jwa.ES512(), keys: ecdsaKeys(elliptic.P521())},
		{name: "Ed25519", algorithm: jwa.EdDSAEd25519(), keys: ed25519Keys},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, signer := tt.keys(t, tt.algorithm)
			validator, err := authjwt.New(authjwt.Config{
				Issuer: "https://issuer.example.test", Audience: "orders",
				Algorithms: []jwa.SignatureAlgorithm{tt.algorithm}, KeySet: keys,
				Clock: authtest.NewClock(now),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			principal, err := validator.ValidateBearer(context.Background(), signedToken(t, signer, tt.algorithm, claims))
			if err != nil {
				t.Fatalf("ValidateBearer() error = %v", err)
			}
			if principal.Subject() != "service" {
				t.Fatalf("ValidateBearer() subject = %q", principal.Subject())
			}
		})
	}
}

func TestRFC7520ExactCompactJWSIsValidJOSEButNotAJWT(t *testing.T) {
	t.Parallel()

	// RFC 7520 section 4.4, mirrored by ietf-jose/cookbook commit
	// 13692b68bfc18b99557a5b1ed311fd5077bfff04.
	const compact = "eyJhbGciOiJIUzI1NiIsImtpZCI6IjAxOGMwYWU1LTRkOWItNDcxYi1iZmQ2LWVlZjMxNGJjNzAzNyJ9.SXTigJlzIGEgZGFuZ2Vyb3VzIGJ1c2luZXNzLCBGcm9kbywgZ29pbmcgb3V0IHlvdXIgZG9vci4gWW91IHN0ZXAgb250byB0aGUgcm9hZCwgYW5kIGlmIHlvdSBkb24ndCBrZWVwIHlvdXIgZmVldCwgdGhlcmXigJlzIG5vIGtub3dpbmcgd2hlcmUgeW91IG1pZ2h0IGJlIHN3ZXB0IG9mZiB0by4.s0h6KThzkfBBBkLspW1h84VsJZFTsPPqMDA7g1Md7p0"
	keys, err := jwk.Parse([]byte(`{"keys":[{"kty":"oct","kid":"018c0ae5-4d9b-471b-bfd6-eef314bc7037","use":"sig","alg":"HS256","k":"hJtXIZ2uSN5kbQfbtTNWbpdmhkV8FJG-Onbc6mxCcYg"}]}`))
	if err != nil {
		t.Fatalf("jwk.Parse() error = %v", err)
	}
	key, ok := keys.Key(0)
	if !ok {
		t.Fatal("RFC 7520 key is missing")
	}
	if _, err := jws.Verify([]byte(compact), jws.WithKey(jwa.HS256(), key)); err != nil {
		t.Fatalf("jws.Verify(RFC 7520) error = %v", err)
	}
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(time.Unix(1_800_000_000, 0)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := validator.ValidateBearer(context.Background(), compact); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(RFC 7520 JWS) error = %v, want rejected", err)
	}
}

func TestDifferentialValidationKeepsExplicitlyStricterPolicy(t *testing.T) {
	t.Parallel()

	secret := []byte("01234567890123456789012345678901")
	keys, signer := signingKeySet(t, secret, jwa.HS256())
	now := time.Unix(1_800_000_000, 0).UTC()
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now, "exp": now.Add(time.Hour),
	}
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	valid := signedToken(t, signer, jwa.HS256(), claims)
	duplicate := signedPayload(t, signer, jwa.HS256(), []byte(
		`{"sub":"service","sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1800000000,"exp":1800003600}`,
	))
	critical := signedTokenWithHeaders(t, signer, jwa.HS256(), claims, map[string]any{
		"alg": "HS256", "kid": "key", "crit": []string{"policy"}, "policy": true,
	})

	tests := []struct {
		name       string
		token      string
		wantStrict bool
		wantPeer   bool
	}{
		{name: "valid", token: valid, wantStrict: true, wantPeer: true},
		{name: "duplicate claim", token: duplicate, wantPeer: true},
		{name: "unsupported critical header", token: critical, wantPeer: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, strictErr := validator.ValidateBearer(context.Background(), tt.token)
			if got := strictErr == nil; got != tt.wantStrict {
				t.Fatalf("strict acceptance = %v, error = %v", got, strictErr)
			}
			peer, peerErr := golangjwt.Parse(
				tt.token,
				func(*golangjwt.Token) (any, error) { return secret, nil },
				golangjwt.WithValidMethods([]string{"HS256"}),
				golangjwt.WithIssuer("https://issuer.example.test"),
				golangjwt.WithAudience("orders"),
				golangjwt.WithIssuedAt(), golangjwt.WithExpirationRequired(),
				golangjwt.WithTimeFunc(func() time.Time { return now }),
			)
			if got := peerErr == nil && peer != nil && peer.Valid; got != tt.wantPeer {
				t.Fatalf("golang-jwt acceptance = %v, error = %v", got, peerErr)
			}
		})
	}
}

func hmacKeys(t *testing.T, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	t.Helper()
	sizes := map[string]int{"HS256": 32, "HS384": 48, "HS512": 64}
	return signingKeySet(t, make([]byte, sizes[algorithm.String()]), algorithm)
}

func matrixRSAKeys(t *testing.T, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	t.Helper()
	return rsaKeys(t, "key", algorithm)
}

func ecdsaKeys(curve elliptic.Curve) func(*testing.T, jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	return func(t *testing.T, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
		t.Helper()
		private, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa.GenerateKey() error = %v", err)
		}
		return signingKeySet(t, private, algorithm)
	}
}

func ed25519Keys(t *testing.T, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	return signingKeySet(t, private, algorithm)
}

func signingKeySet(t *testing.T, raw any, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	t.Helper()
	signer, err := jwk.Import(raw)
	if err != nil {
		t.Fatalf("jwk.Import() error = %v", err)
	}
	if err := signer.Set(jwk.KeyIDKey, "key"); err != nil {
		t.Fatalf("Set(kid) error = %v", err)
	}
	if err := signer.Set(jwk.AlgorithmKey, algorithm); err != nil {
		t.Fatalf("Set(alg) error = %v", err)
	}
	verification := signer
	if algorithm.String()[:1] != "H" {
		verification, err = signer.PublicKey()
		if err != nil {
			t.Fatalf("PublicKey() error = %v", err)
		}
	}
	set := jwk.NewSet()
	if err := set.AddKey(verification); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	return set, signer
}
