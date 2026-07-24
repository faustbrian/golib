package password

import (
	"fmt"
)

// Algorithm identifies a supported password hashing algorithm.
type Algorithm string

const (
	// Argon2id identifies PHC Argon2id version 19 encodings.
	Argon2id Algorithm = "argon2id"
	// Bcrypt identifies standard $2a$, $2b$, and $2y$ bcrypt encodings.
	Bcrypt Algorithm = "bcrypt"
)

// Argon2idParameters is an immutable Argon2id work and output specification.
type Argon2idParameters struct {
	// Version is the Argon2 version; only 19 is supported.
	Version uint32
	// Time is the number of iterations.
	Time uint32
	// MemoryKiB is total memory in kibibytes.
	MemoryKiB uint32
	// Parallelism is the number of Argon2 lanes.
	Parallelism uint8
	// SaltLength is the generated salt length in bytes.
	SaltLength uint32
	// OutputLength is the derived-key length in bytes.
	OutputLength uint32
}

// Limits bounds caller input, encoded fields, primitive resources, and
// concurrent work. Values are copied into Policy.
type Limits struct {
	// PasswordBytes is the maximum password length accepted by operations.
	PasswordBytes int
	// EncodedHashBytes is the maximum encoded-hash length accepted by parsing.
	EncodedHashBytes int
	// Argon2Time is the maximum accepted Argon2 iteration count.
	Argon2Time uint32
	// MemoryKiB is the maximum accepted Argon2 memory in kibibytes.
	MemoryKiB uint32
	// Parallelism is the maximum accepted Argon2 lane count.
	Parallelism uint8
	// SaltBytes is the maximum decoded salt length.
	SaltBytes uint32
	// OutputBytes is the maximum decoded derived-key length.
	OutputBytes uint32
	// BcryptCost is the maximum accepted bcrypt cost.
	BcryptCost int
	// Concurrent is the maximum active expensive operations.
	Concurrent int
	// Queue is the maximum callers waiting for expensive work.
	Queue int
}

// PolicyConfig contains all fields required to construct an immutable Policy.
type PolicyConfig struct {
	// Algorithm is the target algorithm for new hashes and safe upgrades.
	Algorithm Algorithm
	// Argon2id configures Argon2id when Algorithm is Argon2id.
	Argon2id Argon2idParameters
	// BcryptCost configures bcrypt when Algorithm is Bcrypt.
	BcryptCost int
	// Limits is the hard resource budget for all accepted hashes.
	Limits Limits
}

// Policy is a validated immutable hashing and verification policy.
type Policy struct{ config PolicyConfig }

// NewPolicy validates and copies a complete policy configuration.
func NewPolicy(c PolicyConfig) (Policy, error) {
	if c.Limits.PasswordBytes < 1 || c.Limits.EncodedHashBytes < 60 || c.Limits.Argon2Time < 1 || c.Limits.MemoryKiB < 8 || c.Limits.Parallelism < 1 || c.Limits.SaltBytes < 8 || c.Limits.OutputBytes < 16 || c.Limits.BcryptCost < 4 || c.Limits.BcryptCost > 31 || c.Limits.Concurrent < 1 || c.Limits.Queue < 0 {
		return Policy{}, newError(ErrInvalidPolicy, "configure limits", nil)
	}
	switch c.Algorithm {
	case Argon2id:
		p := c.Argon2id
		if p.Version != 19 || p.Time < 1 || p.Time > c.Limits.Argon2Time || p.MemoryKiB < 8*uint32(p.Parallelism) || p.MemoryKiB > c.Limits.MemoryKiB || p.Parallelism < 1 || p.Parallelism > c.Limits.Parallelism || p.SaltLength < 8 || p.SaltLength > c.Limits.SaltBytes || p.OutputLength < 16 || p.OutputLength > c.Limits.OutputBytes || argon2idEncodedLength(p) > uint64(c.Limits.EncodedHashBytes) {
			return Policy{}, newError(ErrInvalidPolicy, "configure argon2id", nil)
		}
	case Bcrypt:
		if c.BcryptCost < 4 || c.BcryptCost > c.Limits.BcryptCost {
			return Policy{}, newError(ErrInvalidPolicy, "configure bcrypt", nil)
		}
	default:
		return Policy{}, newError(ErrInvalidPolicy, "configure algorithm", nil)
	}
	return Policy{config: c}, nil
}

func argon2idEncodedLength(parameters Argon2idParameters) uint64 {
	prefix := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$$", parameters.Version, parameters.MemoryKiB, parameters.Time, parameters.Parallelism)
	return uint64(len(prefix)) + rawBase64EncodedLength(parameters.SaltLength) + rawBase64EncodedLength(parameters.OutputLength)
}

func rawBase64EncodedLength(bytes uint32) uint64 { return (uint64(bytes)*8 + 5) / 6 }

// DefaultPolicy returns the measured default Argon2id policy: version 19,
// time 2, 64 MiB memory, one lane, 16-byte salt, and 32-byte output.
func DefaultPolicy() Policy {
	return Policy{config: PolicyConfig{Algorithm: Argon2id, Argon2id: Argon2idParameters{Version: 19, Time: 2, MemoryKiB: 64 * 1024, Parallelism: 1, SaltLength: 16, OutputLength: 32}, Limits: Limits{PasswordBytes: 1024, EncodedHashBytes: 512, Argon2Time: 4, MemoryKiB: 128 * 1024, Parallelism: 4, SaltBytes: 64, OutputBytes: 64, BcryptCost: 14, Concurrent: 4, Queue: 16}}}
}

// Algorithm returns the target algorithm.
func (p Policy) Algorithm() Algorithm { return p.config.Algorithm }

// Limits returns the policy resource limits by value.
func (p Policy) Limits() Limits { return p.config.Limits }

// Argon2idParameters returns the configured Argon2id parameters by value.
func (p Policy) Argon2idParameters() Argon2idParameters { return p.config.Argon2id }

// BcryptCost returns the configured bcrypt cost.
func (p Policy) BcryptCost() int { return p.config.BcryptCost }
