package jwt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestValidatorRejectsCryptographicallyUnsafeKeys(t *testing.T) {
	t.Parallel()

	tests := map[string]jwk.Set{
		"short HMAC secret": keySetFromRaw(t, []byte("too-short"), jwa.HS256()),
		"wrong EC curve":    keySetFromRaw(t, mustECDSAKey(t, elliptic.P384()), jwa.ES256()),
		"private RSA key":   keySetFromRaw(t, mustRSAKey(t, 2048), jwa.RS256()),
	}
	for name, keys := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := New(Config{
				Issuer: "issuer", Audience: "audience",
				Algorithms: []jwa.SignatureAlgorithm{algorithmForSet(t, keys)},
				KeySet:     keys, Clock: authtest.NewClock(time.Unix(1, 0)),
			})
			if !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Fatalf("New() error = %v, want invalid configuration", err)
			}
		})
	}
}

func TestInspectJSONObjectRejectsNonInteroperableUnicodeAndHugeNumbers(t *testing.T) {
	t.Parallel()

	inputs := map[string][]byte{
		"unpaired high surrogate": []byte(`{"value":"\uD800"}`),
		"unpaired low surrogate":  []byte(`{"value":"\uDFFF"}`),
		"huge integer":            []byte(`{"value":` + strings.Repeat("9", 129) + `}`),
		"huge exponent":           []byte(`{"value":1e` + strings.Repeat("9", 129) + `}`),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			if err := inspectJSONObject(input, authentication.MaxClaims, authentication.MaxClaimDepth); err == nil {
				t.Fatalf("inspectJSONObject(%q) error = nil", input)
			}
		})
	}
	exactNumber := []byte(`{"value":` + strings.Repeat("9", maxJSONNumberBytes) + `}`)
	if err := inspectJSONObject(exactNumber, authentication.MaxClaims, authentication.MaxClaimDepth); err != nil {
		t.Fatalf("inspectJSONObject(exact number bound) error = %v", err)
	}
}

func TestValidateKeyMaterialRejectsEveryInvalidRepresentation(t *testing.T) {
	t.Parallel()

	hmac := keyFromRaw(t, bytesOf('h', 64))
	rsaKey := keyFromRaw(t, &mustRSAKey(t, 2048).PublicKey)
	ecdsaKey := keyFromRaw(t, mustECDSAKey(t, elliptic.P256()))
	_, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	edKey := keyFromRaw(t, edPrivate.Public())
	tests := []struct {
		name      string
		key       jwk.Key
		algorithm string
	}{
		{name: "invalid JWK", key: invalidValidationKey{Key: hmac}, algorithm: "HS256"},
		{name: "non-symmetric HMAC", key: keyOnly{Key: hmac}, algorithm: "HS256"},
		{name: "unknown HMAC", key: hmac, algorithm: "HS999"},
		{name: "non-RSA key", key: keyOnly{Key: rsaKey}, algorithm: "RS256"},
		{name: "missing RSA modulus", key: rsaWithoutModulus{RSAPublicKey: rsaKey.(jwk.RSAPublicKey)}, algorithm: "RS256"},
		{name: "untrusted RSA modulus", key: rsaUntrustedModulus{RSAPublicKey: rsaKey.(jwk.RSAPublicKey)}, algorithm: "RS256"},
		{name: "oversized RSA modulus", key: rsaOversizedModulus{RSAPublicKey: rsaKey.(jwk.RSAPublicKey)}, algorithm: "RS256"},
		{name: "non-ECDSA key", key: keyOnly{Key: ecdsaKey}, algorithm: "ES256"},
		{name: "missing ECDSA curve", key: ecdsaWithoutCurve{ECDSAPublicKey: ecdsaKey.(jwk.ECDSAPublicKey)}, algorithm: "ES256"},
		{name: "untrusted ECDSA curve", key: ecdsaUntrustedCurve{ECDSAPublicKey: ecdsaKey.(jwk.ECDSAPublicKey)}, algorithm: "ES256"},
		{name: "unknown ECDSA algorithm", key: ecdsaKey, algorithm: "ES999"},
		{name: "non-OKP key", key: keyOnly{Key: edKey}, algorithm: "Ed25519"},
		{name: "missing OKP curve", key: okpWithoutCurve{OKPPublicKey: edKey.(jwk.OKPPublicKey)}, algorithm: "Ed25519"},
		{name: "untrusted OKP curve", key: okpUntrustedCurve{OKPPublicKey: edKey.(jwk.OKPPublicKey)}, algorithm: "Ed25519"},
		{name: "unsupported algorithm", key: hmac, algorithm: "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateKeyMaterial(tt.key, tt.algorithm); err == nil {
				t.Fatal("validateKeyMaterial() error = nil")
			}
		})
	}
	if err := validateKeyMaterial(rsaMaximumModulus{RSAPublicKey: rsaKey.(jwk.RSAPublicKey)}, "RS256"); err != nil {
		t.Fatalf("validateKeyMaterial(maximum RSA modulus) error = %v", err)
	}

	if got := significantBits([]byte{0, 1}); got != 1 {
		t.Fatalf("significantBits(leading zero) = %d", got)
	}
	if got := significantBits([]byte{0, 0}); got != 0 {
		t.Fatalf("significantBits(zero) = %d", got)
	}
}

func TestJSONUnicodeEscapeValidationAcceptsPairsAndRejectsMalformedPairs(t *testing.T) {
	t.Parallel()

	accepted := [][]byte{
		[]byte(`{"value":"\uD83D\uDE00"}`),
		[]byte(`"\uD800\uDC00`),
		[]byte(`{"value":"\uD800\uDC00"}`),
		[]byte(`{"value":"\uDBFF\uDFFF"}`),
		[]byte(`{"value":"\u0041"}`),
		[]byte(`{"value":"\n"}`),
		[]byte(`\uD800`),
		[]byte(`{"value":"\u"}`),
		{'"', '\\'},
	}
	for _, encoded := range accepted {
		if err := validateJSONUnicodeEscapes(encoded); err != nil {
			t.Fatalf("validateJSONUnicodeEscapes(%q) error = %v", encoded, err)
		}
	}
	rejected := [][]byte{
		[]byte(`"\uD800`),
		[]byte(`\ {"value":"\uD800"}`),
		[]byte(`{"value":"\n\uD800"}`),
		[]byte(`{"value":"\uGGGG\uD800"}`),
		[]byte(`{"value":"\uD800"}`),
		[]byte(`{"value":"\uD800xuDC00"}`),
		[]byte(`{"value":"\uD800\xDC00"}`),
		[]byte(`{"value":"\uD800\uGGGG"}`),
		[]byte(`{"value":"\uD800\u0041"}`),
		[]byte(`{"value":"\uD800\uE000"}`),
		[]byte(`{"value":"\uD800\uDC00\uD800"}`),
		[]byte(`{"value":"\uDFFF"}`),
		[]byte(`{"value":"\uDC00"}`),
	}
	for _, encoded := range rejected {
		if err := validateJSONUnicodeEscapes(encoded); err == nil {
			t.Fatalf("validateJSONUnicodeEscapes(%q) error = nil", encoded)
		}
	}

	tests := []struct {
		encoded string
		value   uint16
		valid   bool
	}{
		{encoded: "123", valid: false},
		{encoded: "09aF", value: 0x09AF, valid: true},
		{encoded: "ffff", value: 0xFFFF, valid: true},
		{encoded: "AAAA", value: 0xAAAA, valid: true},
		{encoded: "GGGG", valid: false},
	}
	for _, tt := range tests {
		value, valid := decodeHexQuad([]byte(tt.encoded))
		if value != tt.value || valid != tt.valid {
			t.Fatalf("decodeHexQuad(%q) = %#x, %t", tt.encoded, value, valid)
		}
	}
}

type invalidValidationKey struct{ jwk.Key }

func (invalidValidationKey) Validate() error { return errors.New("invalid") }

type keyOnly struct{ jwk.Key }

type rsaWithoutModulus struct{ jwk.RSAPublicKey }

func (rsaWithoutModulus) N() ([]byte, bool) { return nil, false }

type rsaUntrustedModulus struct{ jwk.RSAPublicKey }

func (key rsaUntrustedModulus) N() ([]byte, bool) {
	modulus, _ := key.RSAPublicKey.N()
	return modulus, false
}

type rsaOversizedModulus struct{ jwk.RSAPublicKey }

func (rsaOversizedModulus) N() ([]byte, bool) {
	modulus := make([]byte, maximumRSAKeyBits/8+1)
	modulus[0] = 1
	return modulus, true
}

type rsaMaximumModulus struct{ jwk.RSAPublicKey }

func (rsaMaximumModulus) N() ([]byte, bool) {
	modulus := make([]byte, maximumRSAKeyBits/8)
	modulus[0] = 0x80
	return modulus, true
}

type ecdsaWithoutCurve struct{ jwk.ECDSAPublicKey }

func (ecdsaWithoutCurve) Crv() (jwa.EllipticCurveAlgorithm, bool) {
	return jwa.EmptyEllipticCurveAlgorithm(), false
}

type ecdsaUntrustedCurve struct{ jwk.ECDSAPublicKey }

func (ecdsaUntrustedCurve) Crv() (jwa.EllipticCurveAlgorithm, bool) {
	return jwa.P256(), false
}

type okpWithoutCurve struct{ jwk.OKPPublicKey }

func (okpWithoutCurve) Crv() (jwa.EllipticCurveAlgorithm, bool) {
	return jwa.EmptyEllipticCurveAlgorithm(), false
}

type okpUntrustedCurve struct{ jwk.OKPPublicKey }

func (okpUntrustedCurve) Crv() (jwa.EllipticCurveAlgorithm, bool) {
	return jwa.Ed25519(), false
}

func keyFromRaw(t *testing.T, raw any) jwk.Key {
	t.Helper()
	key, err := jwk.Import(raw)
	if err != nil {
		t.Fatalf("jwk.Import() error = %v", err)
	}
	return key
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func keySetFromRaw(t *testing.T, raw any, algorithm jwa.SignatureAlgorithm) jwk.Set {
	t.Helper()
	key, err := jwk.Import(raw)
	if err != nil {
		t.Fatalf("jwk.Import() error = %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, "key"); err != nil {
		t.Fatalf("Set(kid) error = %v", err)
	}
	if err := key.Set(jwk.AlgorithmKey, algorithm); err != nil {
		t.Fatalf("Set(alg) error = %v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	return set
}

func algorithmForSet(t *testing.T, set jwk.Set) jwa.SignatureAlgorithm {
	t.Helper()
	key, ok := set.Key(0)
	if !ok {
		t.Fatal("key set is empty")
	}
	algorithm, ok := key.Algorithm()
	if !ok {
		t.Fatal("key has no algorithm")
	}
	result, ok := jwa.LookupSignatureAlgorithm(algorithm.String())
	if !ok {
		t.Fatalf("unknown signature algorithm %q", algorithm.String())
	}
	return result
}

func mustRSAKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	if bits == 2048 {
		hardeningRSAOnce.Do(func() {
			key, err := rsa.GenerateKey(rand.Reader, bits)
			if err != nil {
				hardeningRSAErr = err
				return
			}
			hardeningRSADER, hardeningRSAErr = x509.MarshalPKCS8PrivateKey(key)
		})
		if hardeningRSAErr != nil {
			t.Fatalf("prepare RSA fixture: %v", hardeningRSAErr)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(hardeningRSADER)
		if err != nil {
			t.Fatalf("ParsePKCS8PrivateKey() error = %v", err)
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("ParsePKCS8PrivateKey() type = %T", parsed)
		}
		return key
	}
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	return key
}

var (
	hardeningRSAOnce sync.Once
	hardeningRSADER  []byte
	hardeningRSAErr  error
)

func mustECDSAKey(t *testing.T, curve elliptic.Curve) *ecdsa.PublicKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	return &key.PublicKey
}
