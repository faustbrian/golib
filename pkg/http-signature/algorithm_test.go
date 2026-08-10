package httpsignature

//lint:file-ignore SA1019 Invalid-key fixtures intentionally corrupt deprecated raw ECDSA fields.

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"io"
	"math/big"
	"testing"
	"time"
)

func TestRegisteredAlgorithmsRoundTripWithStrictKeyTypes(t *testing.T) {
	base := []byte(`"@method": POST
"@signature-params": ("@method");created=1618884473`)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(RSA) error = %v", err)
	}
	p256Key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-256) error = %v", err)
	}
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-384) error = %v", err)
	}
	edPublic, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(Ed25519) error = %v", err)
	}
	hmacKey, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}

	tests := []struct {
		name      string
		algorithm Algorithm
		signing   any
		verify    any
		length    int
	}{
		{"rsa pss", RSAPSSSHA512, rsaKey, &rsaKey.PublicKey, rsaKey.Size()},
		{"rsa v1.5", RSAV15SHA256, rsaKey, &rsaKey.PublicKey, rsaKey.Size()},
		{"hmac", HMACSHA256, hmacKey, hmacKey, 32},
		{"ecdsa p256", ECDSAP256SHA256, p256Key, &p256Key.PublicKey, 64},
		{"ecdsa p384", ECDSAP384SHA384, p384Key, &p384Key.PublicKey, 96},
		{"ed25519", Ed25519, edPrivate, edPublic, ed25519.SignatureSize},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			signature, signErr := Sign(context.Background(), test.algorithm, test.signing, base, rand.Reader)
			if signErr != nil {
				t.Fatalf("Sign() error = %v", signErr)
			}
			if len(signature) != test.length {
				t.Fatalf("signature length = %d, want %d", len(signature), test.length)
			}
			if verifyErr := Verify(context.Background(), test.algorithm, test.verify, base, signature); verifyErr != nil {
				t.Fatalf("Verify() error = %v", verifyErr)
			}

			tampered := append([]byte(nil), signature...)
			tampered[len(tampered)-1] ^= 1
			if verifyErr := Verify(context.Background(), test.algorithm, test.verify, base, tampered); !errors.Is(verifyErr, ErrInvalidSignatureValue) {
				t.Fatalf("Verify(tampered) error = %v, want ErrInvalidSignatureValue", verifyErr)
			}
		})
	}
}

func TestAlgorithmsRejectWrongKeysRandomnessAndCancellation(t *testing.T) {
	t.Parallel()

	base := []byte("base")
	hmacKey, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}

	if _, err = Sign(context.Background(), RSAPSSSHA512, hmacKey, base, rand.Reader); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("Sign(wrong key) error = %v, want ErrIncompatibleKey", err)
	}
	if err = Verify(context.Background(), Ed25519, hmacKey, base, make([]byte, 64)); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("Verify(wrong key) error = %v, want ErrIncompatibleKey", err)
	}
	if _, err = Sign(context.Background(), Algorithm("none"), hmacKey, base, rand.Reader); !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Fatalf("Sign(unknown algorithm) error = %v, want ErrUnsupportedSignatureAlgorithm", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Sign(canceled, HMACSHA256, hmacKey, base, rand.Reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sign(canceled) error = %v, want context.Canceled", err)
	}
	if err = Verify(canceled, HMACSHA256, hmacKey, base, make([]byte, 32)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify(canceled) error = %v, want context.Canceled", err)
	}
}

func TestNewHMACKeyRejectsWeakOrEmptyMaterialAndDoesNotAlias(t *testing.T) {
	t.Parallel()

	if _, err := NewHMACKey(make([]byte, 31)); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("NewHMACKey(short) error = %v, want ErrIncompatibleKey", err)
	}

	material := []byte("0123456789abcdef0123456789abcdef")
	key, err := NewHMACKey(material)
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}
	material[0] ^= 1

	signature, err := Sign(context.Background(), HMACSHA256, key, []byte("base"), rand.Reader)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := Verify(context.Background(), HMACSHA256, key, []byte("base"), signature); err != nil {
		t.Fatalf("Verify() error after source mutation = %v", err)
	}
}

func TestAlgorithmsRejectInternallyInconsistentKeys(t *testing.T) {
	t.Parallel()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(Ed25519) error = %v", err)
	}
	inconsistentEd25519 := append(ed25519.PrivateKey(nil), privateKey...)
	inconsistentEd25519[len(inconsistentEd25519)-1] ^= 1
	if _, err := Sign(context.Background(), Ed25519, inconsistentEd25519, []byte("base"), rand.Reader); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("Sign(inconsistent Ed25519 key) error = %v", err)
	}

	first, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(first P-256) error = %v", err)
	}
	second, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(second P-256) error = %v", err)
	}
	inconsistentECDSA := *first
	inconsistentECDSA.PublicKey = second.PublicKey
	if _, err := Sign(context.Background(), ECDSAP256SHA256, &inconsistentECDSA, []byte("base"), rand.Reader); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("Sign(inconsistent ECDSA key) error = %v", err)
	}

	evenModulus := new(big.Int).Lsh(big.NewInt(1), 2047)
	if err := Verify(context.Background(), RSAV15SHA256, &rsa.PublicKey{N: evenModulus, E: 65537}, []byte("base"), nil); !errors.Is(err, ErrIncompatibleKey) {
		t.Fatalf("Verify(even RSA modulus) error = %v", err)
	}
}

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type secondCheckCanceledContext struct {
	checks int
}

func (*secondCheckCanceledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*secondCheckCanceledContext) Done() <-chan struct{}       { return nil }
func (ctx *secondCheckCanceledContext) Err() error {
	ctx.checks++
	if ctx.checks > 1 {
		return context.Canceled
	}
	return nil
}
func (*secondCheckCanceledContext) Value(any) any { return nil }

func TestAlgorithmsFailClosedAcrossEveryKeyAndCancellationBoundary(t *testing.T) {
	t.Parallel()

	base := []byte("base")
	hmacKey, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	p256Key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-256) error = %v", err)
	}
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-384) error = %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(RSA) error = %v", err)
	}
	edPublic, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(Ed25519) error = %v", err)
	}

	for _, test := range []struct {
		name      string
		algorithm Algorithm
		key       any
		random    io.Reader
	}{
		{"rsa pss wrong key", RSAPSSSHA512, hmacKey, rand.Reader},
		{"rsa v1.5 wrong key", RSAV15SHA256, hmacKey, rand.Reader},
		{"hmac wrong key", HMACSHA256, []byte("not an HMACKey"), rand.Reader},
		{"p256 wrong key", ECDSAP256SHA256, p384Key, rand.Reader},
		{"p384 wrong key", ECDSAP384SHA384, p256Key, rand.Reader},
		{"ed25519 wrong key", Ed25519, edPublic, rand.Reader},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Sign(context.Background(), test.algorithm, test.key, base, test.random); !errors.Is(err, ErrIncompatibleKey) {
				t.Fatalf("Sign() error = %v, want ErrIncompatibleKey", err)
			}
		})
	}

	for _, test := range []struct {
		name      string
		algorithm Algorithm
		key       any
	}{
		{"rsa pss wrong key", RSAPSSSHA512, rsaKey},
		{"rsa v1.5 wrong key", RSAV15SHA256, rsaKey},
		{"hmac wrong key", HMACSHA256, []byte("not an HMACKey")},
		{"p256 wrong key", ECDSAP256SHA256, p256Key},
		{"p384 wrong key", ECDSAP384SHA384, p384Key},
		{"ed25519 wrong key", Ed25519, edPrivate},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := Verify(context.Background(), test.algorithm, test.key, base, nil); !errors.Is(err, ErrIncompatibleKey) {
				t.Fatalf("Verify() error = %v, want ErrIncompatibleKey", err)
			}
		})
	}

	for _, test := range []struct {
		name      string
		algorithm Algorithm
		key       any
	}{
		{"rsa pss", RSAPSSSHA512, rsaKey},
		{"p256", ECDSAP256SHA256, p256Key},
		{"p384", ECDSAP384SHA384, p384Key},
	} {
		test := test
		t.Run(test.name+" missing randomness", func(t *testing.T) {
			if _, err := Sign(context.Background(), test.algorithm, test.key, base, nil); !errors.Is(err, ErrSignatureRandomness) {
				t.Fatalf("Sign() error = %v, want ErrSignatureRandomness", err)
			}
		})
	}
	if _, err := Sign(context.Background(), RSAPSSSHA512, rsaKey, base, failingRandomReader{}); !errors.Is(err, ErrSignatureRandomness) {
		t.Fatalf("Sign(failed randomness) error = %v, want ErrSignatureRandomness", err)
	}
	invalidECDSA := *p256Key
	invalidECDSA.D = big.NewInt(0) //nolint:staticcheck // Deliberately corrupts an invalid-key fixture.
	if _, err := signECDSA(rand.Reader, &invalidECDSA, make([]byte, 32), 32); err == nil {
		t.Fatal("signECDSA(invalid key) succeeded")
	}

	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if _, err := Sign(nil, HMACSHA256, hmacKey, base, nil); err == nil { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatal("Sign(nil context) succeeded")
	}
	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if err := Verify(nil, HMACSHA256, hmacKey, base, nil); err == nil { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatal("Verify(nil context) succeeded")
	}
	if err := Verify(context.Background(), Algorithm("unknown"), hmacKey, base, nil); !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Fatalf("Verify(unknown algorithm) error = %v", err)
	}
	if _, err := Sign(&secondCheckCanceledContext{}, HMACSHA256, hmacKey, base, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sign(second-check cancellation) error = %v", err)
	}
	validHMAC, _ := Sign(context.Background(), HMACSHA256, hmacKey, base, nil)
	if err := Verify(&secondCheckCanceledContext{}, HMACSHA256, hmacKey, base, validHMAC); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify(second-check cancellation) error = %v", err)
	}
}

func TestECDSAVerificationRejectsNonCanonicalScalars(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	for _, signature := range [][]byte{
		make([]byte, 63),
		make([]byte, 64),
		append(make([]byte, 32), big.NewInt(1).FillBytes(make([]byte, 32))...),
		append(big.NewInt(1).FillBytes(make([]byte, 32)), make([]byte, 32)...),
		append(key.Params().N.FillBytes(make([]byte, 32)), big.NewInt(1).FillBytes(make([]byte, 32))...),
		append(key.Params().N.FillBytes(make([]byte, 32)), make([]byte, 32)...),
		append(big.NewInt(1).FillBytes(make([]byte, 32)), key.Params().N.FillBytes(make([]byte, 32))...),
	} {
		if err := Verify(context.Background(), ECDSAP256SHA256, &key.PublicKey, []byte("base"), signature); !errors.Is(err, ErrInvalidSignatureValue) {
			t.Fatalf("Verify(%x) error = %v, want ErrInvalidSignatureValue", signature, err)
		}
	}
}

func TestAlgorithmKeyPredicatesRejectEveryIndependentBoundary(t *testing.T) {
	t.Parallel()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if !validRSAPrivateKey(rsaKey) || !validRSAPublicKey(&rsaKey.PublicKey) {
		t.Fatal("generated RSA key rejected")
	}
	for _, key := range []*rsa.PrivateKey{nil, {PublicKey: rsa.PublicKey{N: nil, E: 65537}}} {
		if validRSAPrivateKey(key) {
			t.Fatalf("invalid RSA private key accepted: %#v", key)
		}
	}
	for _, key := range []*rsa.PublicKey{
		nil,
		{N: nil, E: 65537},
		{N: new(big.Int).Neg(new(big.Int).Set(rsaKey.N)), E: 65537},
		{N: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 2047), big.NewInt(1)), E: 65537},
		{N: new(big.Int).Lsh(big.NewInt(1), 2047), E: 65537},
		{N: new(big.Int).Set(rsaKey.N), E: 1},
		{N: new(big.Int).Set(rsaKey.N), E: 4},
	} {
		if validRSAPublicKey(key) {
			t.Fatalf("invalid RSA public key accepted: %#v", key)
		}
	}
	if !validRSAPublicKey(&rsa.PublicKey{N: new(big.Int).Set(rsaKey.N), E: 3}) {
		t.Fatal("minimum odd RSA exponent rejected")
	}

	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !validECDSAPrivateKey(p256, elliptic.P256()) || !validECDSAPublicKey(&p256.PublicKey, elliptic.P256()) {
		t.Fatal("generated ECDSA key rejected")
	}
	for _, key := range []*ecdsa.PublicKey{
		nil,
		{Curve: elliptic.P384(), X: p256.X, Y: p256.Y},
		{Curve: elliptic.P256(), X: nil, Y: p256.Y},
		{Curve: elliptic.P256(), X: p256.X, Y: nil},
		{Curve: elliptic.P256(), X: big.NewInt(1), Y: big.NewInt(1)},
	} {
		if validECDSAPublicKey(key, elliptic.P256()) {
			t.Fatalf("invalid ECDSA public key accepted: %#v", key)
		}
	}
	for _, key := range []*ecdsa.PrivateKey{
		nil,
		{PublicKey: ecdsa.PublicKey{Curve: elliptic.P384()}, D: big.NewInt(1)},
		{PublicKey: p256.PublicKey, D: nil},
		{PublicKey: p256.PublicKey, D: big.NewInt(0)},
		{PublicKey: p256.PublicKey, D: big.NewInt(-1)},
		{PublicKey: p256.PublicKey, D: new(big.Int).Set(elliptic.P256().Params().N)},
		{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: big.NewInt(1), Y: big.NewInt(1)}, D: big.NewInt(1)},
		func() *ecdsa.PrivateKey {
			value := *p256
			value.D = new(big.Int).Sub(value.D, big.NewInt(1)) //nolint:staticcheck // Deliberately corrupts an invalid-key fixture.
			return &value
		}(),
	} {
		if validECDSAPrivateKey(key, elliptic.P256()) {
			t.Fatalf("invalid ECDSA private key accepted: %#v", key)
		}
	}
	oppositeY := *p256
	oppositeY.Y = new(big.Int).Sub(elliptic.P256().Params().P, p256.Y)
	if validECDSAPrivateKey(&oppositeY, elliptic.P256()) {
		t.Fatal("ECDSA private key with opposite public point accepted")
	}
}

func TestAlgorithmsRejectCorrectTypesWithInvalidKeyMaterial(t *testing.T) {
	t.Parallel()

	base := []byte("base")
	p256 := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: big.NewInt(1), Y: big.NewInt(1)}, D: big.NewInt(0)}
	p384 := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P384(), X: big.NewInt(1), Y: big.NewInt(1)}, D: big.NewInt(0)}
	badEdPrivate := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	badEdPrivate[len(badEdPrivate)-1] = 1
	for _, test := range []struct {
		algorithm Algorithm
		key       any
	}{
		{RSAPSSSHA512, &rsa.PrivateKey{}},
		{RSAV15SHA256, &rsa.PrivateKey{}},
		{HMACSHA256, HMACKey{material: make([]byte, sha256.Size-1)}},
		{ECDSAP256SHA256, p256},
		{ECDSAP384SHA384, p384},
		{Ed25519, badEdPrivate},
	} {
		if _, err := Sign(context.Background(), test.algorithm, test.key, base, rand.Reader); !errors.Is(err, ErrIncompatibleKey) {
			t.Fatalf("Sign(%s invalid material) error = %v", test.algorithm, err)
		}
	}
	for _, test := range []struct {
		algorithm Algorithm
		key       any
	}{
		{RSAPSSSHA512, &rsa.PublicKey{}},
		{RSAV15SHA256, &rsa.PublicKey{}},
		{HMACSHA256, HMACKey{material: make([]byte, sha256.Size-1)}},
		{ECDSAP256SHA256, &p256.PublicKey},
		{ECDSAP384SHA384, &p384.PublicKey},
		{Ed25519, make(ed25519.PublicKey, ed25519.PublicKeySize-1)},
	} {
		if err := Verify(context.Background(), test.algorithm, test.key, base, nil); !errors.Is(err, ErrIncompatibleKey) {
			t.Fatalf("Verify(%s invalid material) error = %v", test.algorithm, err)
		}
	}
}
