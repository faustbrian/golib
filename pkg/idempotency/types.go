package idempotency

import (
	"crypto/sha256"
	"fmt"
	"time"
)

const (
	// MaxKeyPartBytes bounds each individual logical key component.
	MaxKeyPartBytes = 256
	// MaxFingerprintVersionBytes bounds the canonicalization policy identifier.
	MaxFingerprintVersionBytes = 128
	// MaxOwnerTokenBytes bounds opaque ownership proofs stored by adapters.
	MaxOwnerTokenBytes = 256
	// MaxResultBytes bounds a result stored for terminal replay.
	MaxResultBytes = 1 << 20
	// MaxMetadataEntries bounds the number of stored metadata pairs.
	MaxMetadataEntries = 32
	// MaxMetadataKeyBytes bounds each metadata key.
	MaxMetadataKeyBytes = 128
	// MaxMetadataValueBytes bounds each metadata value.
	MaxMetadataValueBytes = 1024
	// MaxLease is the longest ownership lease accepted by the semantic core.
	MaxLease = 24 * time.Hour
)

// Reason is a stable machine-readable classification for a semantic error.
type Reason string

const (
	// ReasonInvalidKey identifies a missing or invalid logical key component.
	ReasonInvalidKey Reason = "invalid_key"
	// ReasonInvalidFingerprint identifies an invalid fingerprint or policy version.
	ReasonInvalidFingerprint Reason = "invalid_fingerprint"
	// ReasonLimitExceeded identifies input that crosses a documented resource bound.
	ReasonLimitExceeded Reason = "limit_exceeded"
	// ReasonStaleOwner identifies an ownership proof from a superseded attempt.
	ReasonStaleOwner Reason = "stale_owner"
	// ReasonLeaseExpired identifies a current proof used after its lease boundary.
	ReasonLeaseExpired Reason = "lease_expired"
	// ReasonNotFound identifies an operation targeting a missing record.
	ReasonNotFound Reason = "not_found"
	// ReasonInvalidTransition identifies an operation illegal for the current state.
	ReasonInvalidTransition Reason = "invalid_transition"
	// ReasonUnavailable identifies failure to establish or mutate durable state.
	ReasonUnavailable Reason = "unavailable"
	// ReasonInvalidConfiguration identifies invalid constructor or policy options.
	ReasonInvalidConfiguration Reason = "invalid_configuration"
	// ReasonInvalidLease identifies a nonpositive lease duration.
	ReasonInvalidLease Reason = "invalid_lease"
	// ReasonInvalidPayload identifies malformed or unsupported persisted data.
	ReasonInvalidPayload Reason = "invalid_payload"
	// ReasonUnsafeBackend identifies a backend configuration that breaks correctness.
	ReasonUnsafeBackend Reason = "unsafe_backend"
)

// Error reports a stable reason and field while retaining an optional cause.
type Error struct {
	// Reason classifies the failure for programmatic handling.
	Reason Reason
	// Field identifies the input, transition, or backend property involved.
	Field string
	// Cause retains the underlying failure without changing the stable reason.
	Cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("idempotency: %s: %s", e.Reason, e.Field)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// Key is the fully scoped logical identity of one repeatable operation.
// Construct keys with NewKey so every component is present and bounded.
type Key struct {
	namespace string
	tenant    string
	operation string
	caller    string
	value     string
}

// NewKey validates and constructs a namespaced operation identity.
func NewKey(namespace, tenant, operation, caller, value string) (Key, error) {
	parts := []struct {
		name  string
		value string
	}{
		{"namespace", namespace},
		{"tenant", tenant},
		{"operation", operation},
		{"caller", caller},
		{"value", value},
	}
	for _, part := range parts {
		if part.value == "" {
			return Key{}, &Error{Reason: ReasonInvalidKey, Field: part.name}
		}
		if len(part.value) > MaxKeyPartBytes {
			return Key{}, &Error{Reason: ReasonLimitExceeded, Field: part.name}
		}
	}

	return Key{
		namespace: namespace,
		tenant:    tenant,
		operation: operation,
		caller:    caller,
		value:     value,
	}, nil
}

// Namespace returns the broad collision domain for the key.
func (k Key) Namespace() string { return k.namespace }

// Tenant returns the authenticated tenant identity for the key.
func (k Key) Tenant() string { return k.tenant }

// Operation returns the stable business operation name for the key.
func (k Key) Operation() string { return k.operation }

// Caller returns the authenticated caller identity for the key.
func (k Key) Caller() string { return k.caller }

// Value returns the caller-supplied idempotency value for the key.
func (k Key) Value() string { return k.value }

// Fingerprint is a versioned SHA-256 digest of canonical business input.
type Fingerprint struct {
	version string
	sum     [sha256.Size]byte
}

// NewFingerprint hashes canonical business input under a stable policy version.
func NewFingerprint(version string, canonical []byte) (Fingerprint, error) {
	if version == "" {
		return Fingerprint{}, &Error{
			Reason: ReasonInvalidFingerprint,
			Field:  "version",
		}
	}
	if len(version) > MaxFingerprintVersionBytes {
		return Fingerprint{}, &Error{
			Reason: ReasonLimitExceeded,
			Field:  "version",
		}
	}

	return Fingerprint{version: version, sum: sha256.Sum256(canonical)}, nil
}

// NewFingerprintFromSum reconstructs a fingerprint from a persisted SHA-256 sum.
func NewFingerprintFromSum(version string, sum []byte) (Fingerprint, error) {
	if version == "" || len(sum) != sha256.Size {
		return Fingerprint{}, &Error{
			Reason: ReasonInvalidFingerprint,
			Field:  "persisted",
		}
	}
	if len(version) > MaxFingerprintVersionBytes {
		return Fingerprint{}, &Error{
			Reason: ReasonLimitExceeded,
			Field:  "version",
		}
	}

	var digest [sha256.Size]byte
	copy(digest[:], sum)
	return Fingerprint{version: version, sum: digest}, nil
}

// Version returns the canonicalization policy version.
func (f Fingerprint) Version() string { return f.version }

// Sum returns a copy of the SHA-256 digest bytes.
func (f Fingerprint) Sum() []byte {
	sum := make([]byte, len(f.sum))
	copy(sum, f.sum[:])
	return sum
}

// Equal reports whether both the policy version and digest match.
func (f Fingerprint) Equal(other Fingerprint) bool {
	return f.version == other.version && f.sum == other.sum
}
