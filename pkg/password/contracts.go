package password

import "context"

// Hasher creates encoded hashes using caller-cancelable admission. Underlying
// primitives cannot be interrupted once invoked.
type Hasher interface {
	// Hash hashes a caller-owned password without retaining or mutating it.
	Hash(context.Context, []byte) (EncodedHash, error)
}

// Verifier verifies encoded hashes and evaluates the configured upgrade policy.
type Verifier interface {
	// Verify returns a typed match/rehash result or classified failure.
	Verify(context.Context, []byte, string) (Result, error)
	// NeedsRehash evaluates an already parsed hash without invoking a primitive.
	NeedsRehash(EncodedHash) bool
}

// HasherVerifier combines hashing, verification, and explicit upgrade work.
type HasherVerifier interface {
	Hasher
	Verifier
	// VerifyAndUpgrade returns a replacement only after a successful match that
	// requires a monotonic policy upgrade.
	VerifyAndUpgrade(context.Context, []byte, string) (Result, EncodedHash, error)
}
