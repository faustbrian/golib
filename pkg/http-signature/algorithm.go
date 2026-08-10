package httpsignature

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
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
)

var (
	// ErrUnsupportedSignatureAlgorithm reports an unavailable or unregistered algorithm.
	ErrUnsupportedSignatureAlgorithm = errors.New("http signature: unsupported signature algorithm")
	// ErrIncompatibleKey reports a wrong, malformed, or below-policy key type.
	ErrIncompatibleKey = errors.New("http signature: incompatible key")
	// ErrInvalidSignatureValue reports cryptographic verification failure.
	ErrInvalidSignatureValue = errors.New("http signature: invalid signature value")
	// ErrSignatureRandomness reports unavailable signing randomness.
	ErrSignatureRandomness = errors.New("http signature: signing randomness unavailable")
)

// HMACKey owns copied HMAC key bytes. The minimum length is 256 bits, matching
// the output size and security strength expected for HMAC-SHA-256 profiles.
type HMACKey struct {
	material []byte
}

// NewHMACKey copies caller key material and rejects keys shorter than 256 bits.
func NewHMACKey(material []byte) (HMACKey, error) {
	if len(material) < sha256.Size {
		return HMACKey{}, ErrIncompatibleKey
	}

	return HMACKey{material: append([]byte(nil), material...)}, nil
}

// Sign applies the exact RFC 9421 algorithm to signatureBase. random is
// required for RSA-PSS and ECDSA signing and ignored by deterministic
// algorithms.
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
		if random == nil {
			return nil, ErrSignatureRandomness
		}
		digest := sha512.Sum512(signatureBase)
		signature, err = rsa.SignPSS(random, privateKey, crypto.SHA512, digest[:], &rsa.PSSOptions{
			SaltLength: sha512.Size,
			Hash:       crypto.SHA512,
		})
	case RSAV15SHA256:
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok || !validRSAPrivateKey(privateKey) {
			return nil, ErrIncompatibleKey
		}
		digest := sha256.Sum256(signatureBase)
		signature, err = rsa.SignPKCS1v15(random, privateKey, crypto.SHA256, digest[:])
	case HMACSHA256:
		hmacKey, ok := key.(HMACKey)
		if !ok || len(hmacKey.material) < sha256.Size {
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
		if random == nil {
			return nil, ErrSignatureRandomness
		}
		digest := sha256.Sum256(signatureBase)
		signature, err = signECDSA(random, privateKey, digest[:], 32)
	case ECDSAP384SHA384:
		privateKey, ok := key.(*ecdsa.PrivateKey)
		if !ok || !validECDSAPrivateKey(privateKey, elliptic.P384()) {
			return nil, ErrIncompatibleKey
		}
		if random == nil {
			return nil, ErrSignatureRandomness
		}
		digest := sha512.Sum384(signatureBase)
		signature, err = signECDSA(random, privateKey, digest[:], 48)
	case Ed25519:
		privateKey, ok := key.(ed25519.PrivateKey)
		if !ok || !validEd25519PrivateKey(privateKey) {
			return nil, ErrIncompatibleKey
		}
		signature = ed25519.Sign(append(ed25519.PrivateKey(nil), privateKey...), signatureBase)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSignatureAlgorithm, algorithm)
	}
	if err != nil {
		return nil, ErrSignatureRandomness
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	return signature, nil
}

// Verify applies the exact RFC 9421 algorithm and returns only typed,
// secret-safe errors. Verification keys must be public key types; RSA or ECDSA
// private keys are deliberately rejected.
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
		digest := sha256.Sum256(signatureBase)
		valid = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) == nil
	case HMACSHA256:
		hmacKey, ok := key.(HMACKey)
		if !ok || len(hmacKey.material) < sha256.Size {
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

func signECDSA(random io.Reader, key *ecdsa.PrivateKey, digest []byte, size int) ([]byte, error) {
	r, s, err := ecdsa.Sign(random, key, digest)
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
	return key != nil && key.N != nil && key.N.BitLen() >= 2048 && key.Validate() == nil
}

func validRSAPublicKey(key *rsa.PublicKey) bool {
	return key != nil && key.N != nil && key.N.Sign() != -1 && key.N.BitLen() >= 2048 && key.N.Bit(0) == 1 && key.E >= 3 && key.E%2 == 1
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
