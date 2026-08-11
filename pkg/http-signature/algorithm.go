package httpsignature

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"math/big"
)

// Algorithm is an active IANA HTTP Signature Algorithms registry key.
type Algorithm string

const (
	// RSAPSSSHA512 is RSASSA-PSS with SHA-512, MGF1-SHA-512, and 64-byte salt.
	RSAPSSSHA512 Algorithm = "rsa-pss-sha512"
	// RSAV15SHA256 is RSASSA-PKCS1-v1_5 with SHA-256.
	RSAV15SHA256 Algorithm = "rsa-v1_5-sha256"
	// HMACSHA256 is HMAC with SHA-256.
	HMACSHA256 Algorithm = "hmac-sha256"
	// ECDSAP256SHA256 is P-256 ECDSA with SHA-256 and IEEE P1363 encoding.
	ECDSAP256SHA256 Algorithm = "ecdsa-p256-sha256"
	// ECDSAP384SHA384 is P-384 ECDSA with SHA-384 and IEEE P1363 encoding.
	ECDSAP384SHA384 Algorithm = "ecdsa-p384-sha384"
	// Ed25519 is pure Ed25519 with no prehash.
	Ed25519 Algorithm = "ed25519"

	minRSAModulusBits = 2048
	// The upper bound caps attacker-controlled verification work while retaining
	// interoperability with conventional high-assurance RSA deployments.
	maxRSAModulusBits   = 8192
	maxRSASignatureSize = maxRSAModulusBits / 8
	minHMACKeySize      = sha256.Size
	maxHMACKeySize      = sha256.BlockSize
)

var (
	// ErrUnsupportedSignatureAlgorithm reports an unavailable or unregistered algorithm.
	ErrUnsupportedSignatureAlgorithm = errors.New("http signature: unsupported signature algorithm")
	// ErrIncompatibleKey reports a wrong, malformed, or out-of-policy key type.
	ErrIncompatibleKey = errors.New("http signature: incompatible key")
	// ErrInvalidSignatureValue reports cryptographic verification failure.
	ErrInvalidSignatureValue = errors.New("http signature: invalid signature value")
	// ErrSignatureRandomness reports unavailable signing randomness.
	ErrSignatureRandomness = errors.New("http signature: signing randomness unavailable")
)

// HMACKey owns copied HMAC key bytes. Keys contain between 256 and 512 bits.
// The lower bound matches the HMAC-SHA-256 output size; the upper bound matches
// its block size, beyond which HMAC hashes the key before use.
type HMACKey struct {
	material []byte
}

// NewHMACKey copies caller key material and enforces the HMACKey size bounds.
func NewHMACKey(material []byte) (HMACKey, error) {
	if len(material) < minHMACKeySize || len(material) > maxHMACKeySize {
		return HMACKey{}, ErrIncompatibleKey
	}

	return HMACKey{material: append([]byte(nil), material...)}, nil
}

// Sign applies the exact RFC 9421 algorithm to signatureBase. RSA moduli must
// contain 2048 through 8192 bits. Randomized algorithms use Go-managed
// cryptographically secure randomness. random is retained for source
// compatibility and ignored.
func Sign(ctx context.Context, algorithm Algorithm, key any, signatureBase []byte, random io.Reader) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	var signature []byte
	var err error
	switch algorithm {
	case RSAPSSSHA512:
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok || !validRSAPrivateKey(privateKey) {
			return nil, ErrIncompatibleKey
		}
		digest := sha512.Sum512(signatureBase)
		signature, err = rsa.SignPSS(rand.Reader, privateKey, crypto.SHA512, digest[:], &rsa.PSSOptions{
			SaltLength: sha512.Size,
			Hash:       crypto.SHA512,
		})
	case RSAV15SHA256:
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok || !validRSAPrivateKey(privateKey) {
			return nil, ErrIncompatibleKey
		}
		digest := sha256.Sum256(signatureBase)
		signature, err = rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, digest[:])
	case HMACSHA256:
		hmacKey, ok := key.(HMACKey)
		if !ok || !validHMACKey(hmacKey) {
			return nil, ErrIncompatibleKey
		}
		mac := hmac.New(sha256.New, append([]byte(nil), hmacKey.material...))
		_, _ = mac.Write(signatureBase)
		signature = mac.Sum(nil)
	case ECDSAP256SHA256:
		privateKey, ok := key.(*ecdsa.PrivateKey)
		if !ok || !validECDSAPrivateKey(privateKey, elliptic.P256()) {
			return nil, ErrIncompatibleKey
		}
		digest := sha256.Sum256(signatureBase)
		signature, err = signECDSA(privateKey, digest[:], 32)
	case ECDSAP384SHA384:
		privateKey, ok := key.(*ecdsa.PrivateKey)
		if !ok || !validECDSAPrivateKey(privateKey, elliptic.P384()) {
			return nil, ErrIncompatibleKey
		}
		digest := sha512.Sum384(signatureBase)
		signature, err = signECDSA(privateKey, digest[:], 48)
	case Ed25519:
		privateKey, ok := key.(ed25519.PrivateKey)
		if !ok || !validEd25519PrivateKey(privateKey) {
			return nil, ErrIncompatibleKey
		}
		signature = ed25519.Sign(append(ed25519.PrivateKey(nil), privateKey...), signatureBase)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSignatureAlgorithm, algorithm)
	}
	return finishSignature(ctx, signature, err)
}

func finishSignature(ctx context.Context, signature []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, ErrSignatureRandomness
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	return signature, nil
}

// Verify applies the exact RFC 9421 algorithm and returns only typed,
// secret-safe errors. RSA moduli must contain 2048 through 8192 bits.
// Verification keys must be public key types; RSA or ECDSA private keys are
// deliberately rejected.
func Verify(ctx context.Context, algorithm Algorithm, key any, signatureBase, signature []byte) error {
	if err := contextError(ctx); err != nil {
		return err
	}

	var valid bool
	switch algorithm {
	case RSAPSSSHA512:
		publicKey, ok := key.(*rsa.PublicKey)
		if !ok || !validRSAPublicKey(publicKey) {
			return ErrIncompatibleKey
		}
		if !validRSASignatureSize(publicKey, signature) {
			return ErrInvalidSignatureValue
		}
		digest := sha512.Sum512(signatureBase)
		valid = rsa.VerifyPSS(publicKey, crypto.SHA512, digest[:], signature, &rsa.PSSOptions{
			SaltLength: sha512.Size,
			Hash:       crypto.SHA512,
		}) == nil
	case RSAV15SHA256:
		publicKey, ok := key.(*rsa.PublicKey)
		if !ok || !validRSAPublicKey(publicKey) {
			return ErrIncompatibleKey
		}
		if !validRSASignatureSize(publicKey, signature) {
			return ErrInvalidSignatureValue
		}
		digest := sha256.Sum256(signatureBase)
		valid = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) == nil
	case HMACSHA256:
		hmacKey, ok := key.(HMACKey)
		if !ok || !validHMACKey(hmacKey) {
			return ErrIncompatibleKey
		}
		mac := hmac.New(sha256.New, append([]byte(nil), hmacKey.material...))
		_, _ = mac.Write(signatureBase)
		expected := mac.Sum(nil)
		valid = len(signature) == len(expected) && subtle.ConstantTimeCompare(signature, expected) == 1
	case ECDSAP256SHA256:
		publicKey, ok := key.(*ecdsa.PublicKey)
		if !ok || !validECDSAPublicKey(publicKey, elliptic.P256()) {
			return ErrIncompatibleKey
		}
		digest := sha256.Sum256(signatureBase)
		valid = verifyECDSA(publicKey, digest[:], signature, 32)
	case ECDSAP384SHA384:
		publicKey, ok := key.(*ecdsa.PublicKey)
		if !ok || !validECDSAPublicKey(publicKey, elliptic.P384()) {
			return ErrIncompatibleKey
		}
		digest := sha512.Sum384(signatureBase)
		valid = verifyECDSA(publicKey, digest[:], signature, 48)
	case Ed25519:
		publicKey, ok := key.(ed25519.PublicKey)
		if !ok || len(publicKey) != ed25519.PublicKeySize {
			return ErrIncompatibleKey
		}
		valid = len(signature) == ed25519.SignatureSize && ed25519.Verify(append(ed25519.PublicKey(nil), publicKey...), signatureBase, signature)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedSignatureAlgorithm, algorithm)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if !valid {
		return ErrInvalidSignatureValue
	}

	return nil
}

func signECDSA(key *ecdsa.PrivateKey, digest []byte, size int) ([]byte, error) {
	r, s, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return nil, err
	}
	signature := make([]byte, size*2)
	r.FillBytes(signature[:size])
	s.FillBytes(signature[size:])

	return signature, nil
}

func verifyECDSA(key *ecdsa.PublicKey, digest, signature []byte, size int) bool {
	if len(signature) != size*2 {
		return false
	}
	r := new(big.Int).SetBytes(signature[:size])
	s := new(big.Int).SetBytes(signature[size:])
	return ecdsa.Verify(key, digest, r, s)
}

func validRSAPrivateKey(key *rsa.PrivateKey) bool {
	return key != nil && validRSAPublicKey(&key.PublicKey) && key.Validate() == nil
}

func validRSAPublicKey(key *rsa.PublicKey) bool {
	return key != nil && validRSAModulus(key.N) && key.E >= 3 && key.E%2 == 1
}

func validRSAModulus(modulus *big.Int) bool {
	if modulus == nil || modulus.Sign() != 1 || modulus.Bit(0) != 1 {
		return false
	}
	bits := modulus.BitLen()
	return bits >= minRSAModulusBits && bits <= maxRSAModulusBits
}

func validRSASignatureSize(key *rsa.PublicKey, signature []byte) bool {
	return len(signature) <= maxRSASignatureSize && len(signature) == key.Size()
}

func validHMACKey(key HMACKey) bool {
	return len(key.material) >= minHMACKeySize && len(key.material) <= maxHMACKeySize
}

func validECDSAPrivateKey(key *ecdsa.PrivateKey, curve elliptic.Curve) bool {
	if key == nil || key.Curve != curve || !validECDSAPublicKey(&key.PublicKey, curve) {
		return false
	}
	encoded, ok := ecdsaPrivateKeyBytes(key)
	if !ok {
		return false
	}
	parsed, err := ecdsa.ParseRawPrivateKey(curve, encoded)
	return err == nil && parsed.PublicKey.Equal(&key.PublicKey)
}

func validECDSAPublicKey(key *ecdsa.PublicKey, curve elliptic.Curve) bool {
	if key == nil || key.Curve != curve {
		return false
	}
	encoded, ok := ecdsaPublicKeyBytes(key)
	if !ok {
		return false
	}
	// Bytes returned a canonical point for this exact curve, so reparsing cannot
	// fail independently. The round trip prevents acceptance of a caller-built
	// key whose exported representation differs from its public value.
	parsed, _ := ecdsa.ParseUncompressedPublicKey(curve, encoded)
	return parsed.Equal(key)
}

func ecdsaPrivateKeyBytes(key *ecdsa.PrivateKey) (encoded []byte, ok bool) {
	defer func() {
		if recover() != nil {
			encoded, ok = nil, false
		}
	}()
	encoded, err := key.Bytes()
	return encoded, err == nil
}

func ecdsaPublicKeyBytes(key *ecdsa.PublicKey) (encoded []byte, ok bool) {
	defer func() {
		if recover() != nil {
			encoded, ok = nil, false
		}
	}()
	encoded, err := key.Bytes()
	return encoded, err == nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("http signature: nil context")
	}

	return ctx.Err()
}

func validEd25519PrivateKey(key ed25519.PrivateKey) bool {
	if len(key) != ed25519.PrivateKeySize {
		return false
	}
	expected := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	return subtle.ConstantTimeCompare(key, expected) == 1
}
