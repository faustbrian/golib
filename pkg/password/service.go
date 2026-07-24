package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const bcryptMaximumPasswordBytes = 72

// Result carries match and upgrade state without derived material.
type Result struct {
	match  bool
	rehash bool
}

// Match reports successful password verification.
func (r Result) Match() bool { return r.match }

// NeedsRehash reports that a successful match should be upgraded.
func (r Result) NeedsRehash() bool { return r.rehash }

// Service performs policy-bound hashing, verification, and upgrades. It is
// safe for concurrent use when configured collaborators are concurrency-safe.
type Service struct {
	policy    Policy
	entropy   io.Reader
	admission *Admission
	observer  Observer
}

// Option configures optional admission and observation collaborators.
type Option func(*Service) error

// WithAdmission replaces the policy-derived admission controller. The caller
// owns its lifecycle and may share it across services.
func WithAdmission(admission *Admission) Option {
	return func(s *Service) error {
		if admission == nil {
			return newError(ErrInvalidPolicy, "configure admission", nil)
		}
		s.admission = admission
		return nil
	}
}

// New constructs a production service using crypto/rand.Reader for salts.
func New(policy Policy, options ...Option) (*Service, error) {
	if policy.config.Algorithm == "" {
		return nil, newError(ErrInvalidPolicy, "create service", nil)
	}
	admission := newAdmission(policy.config.Limits.Concurrent, policy.config.Limits.Queue)
	s := &Service{policy: policy, entropy: rand.Reader, admission: admission}
	for _, option := range options {
		if option == nil {
			return nil, newError(ErrInvalidPolicy, "configure service", nil)
		}
		if err := option(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// NewTestService constructs a service with caller-controlled entropy. It is
// intended exclusively for deterministic tests and interoperability fixtures;
// production code must use New.
func NewTestService(policy Policy, entropy io.Reader, options ...Option) (*Service, error) {
	if isNilInterface(entropy) {
		return nil, newError(ErrEntropy, "create test service", nil)
	}
	s, err := New(policy, options...)
	if err != nil {
		return nil, err
	}
	s.entropy = entropy
	return s, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	kind := reflected.Kind()
	return (kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface || kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice) && reflected.IsNil()
}

// Hash copies the password, obtains admission and entropy, and returns a new
// encoded hash. Cancellation cannot interrupt a primitive already executing.
func (s *Service) Hash(ctx context.Context, password []byte) (EncodedHash, error) {
	started := time.Now()
	hash, err := s.hash(ctx, password)
	s.observe(ctx, OperationHash, started, false, false, err)
	return hash, err
}

func (s *Service) hash(ctx context.Context, password []byte) (EncodedHash, error) {
	if err := ctx.Err(); err != nil {
		return EncodedHash{}, canceled("hash password", err)
	}
	if len(password) > s.policy.config.Limits.PasswordBytes {
		return EncodedHash{}, newError(ErrResourceRejected, "hash password", nil)
	}
	release, err := s.admission.Acquire(ctx)
	if err != nil {
		return EncodedHash{}, classifyAcquire("hash password", err)
	}
	defer release()
	copyPassword := append([]byte(nil), password...)
	defer clear(copyPassword)
	if s.policy.config.Algorithm == Bcrypt {
		encoded, err := bcrypt.GenerateFromPassword(copyPassword, s.policy.config.BcryptCost)
		if err != nil {
			return EncodedHash{}, newError(ErrResourceRejected, "hash bcrypt", err)
		}
		return ParseEncodedHash(string(encoded), s.policy.config.Limits)
	}
	p := s.policy.config.Argon2id
	salt := make([]byte, p.SaltLength)
	if _, err := io.ReadFull(s.entropy, salt); err != nil {
		return EncodedHash{}, newError(ErrEntropy, "read salt", err)
	}
	digest := argon2.IDKey(copyPassword, salt, p.Time, p.MemoryKiB, p.Parallelism, p.OutputLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", p.Version, p.MemoryKiB, p.Time, p.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest))
	return ParseEncodedHash(encoded, s.policy.config.Limits)
}

// Verify parses and bounds encoded before admission and primitive execution.
// Cancellation cannot interrupt a primitive already executing.
func (s *Service) Verify(ctx context.Context, password []byte, encoded string) (Result, error) {
	started := time.Now()
	result, err := s.verify(ctx, password, encoded)
	s.observe(ctx, OperationVerify, started, result.rehash, false, err)
	return result, err
}

func (s *Service) verify(ctx context.Context, password []byte, encoded string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, canceled("verify password", err)
	}
	if len(password) > s.policy.config.Limits.PasswordBytes {
		return Result{}, newError(ErrResourceRejected, "verify password", nil)
	}
	hash, err := ParseEncodedHash(encoded, s.policy.config.Limits)
	if err != nil {
		return Result{}, err
	}
	if hash.algorithm == Bcrypt && len(password) > bcryptMaximumPasswordBytes {
		return Result{}, newError(ErrResourceRejected, "verify password", nil)
	}
	release, err := s.admission.Acquire(ctx)
	if err != nil {
		return Result{}, classifyAcquire("verify password", err)
	}
	defer release()
	copyPassword := append([]byte(nil), password...)
	defer clear(copyPassword)
	match := false
	switch hash.algorithm {
	case Argon2id:
		p := hash.argon2id
		actual := argon2.IDKey(copyPassword, hash.salt, p.Time, p.MemoryKiB, p.Parallelism, p.OutputLength)
		match = subtle.ConstantTimeCompare(actual, hash.digest) == 1
		clear(actual)
	case Bcrypt:
		match = bcrypt.CompareHashAndPassword([]byte(hash.encoded), copyPassword) == nil
	}
	if !match {
		return Result{}, newError(ErrMismatch, "verify password", nil)
	}
	return Result{match: true, rehash: s.NeedsRehash(hash)}, nil
}

func canceled(operation string, cause error) error {
	return newError(ErrCanceled, operation, cause)
}

func classifyAcquire(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return canceled(operation, err)
	}
	return err
}

// NeedsRehash recommends only monotonic upgrades and never Argon2id-to-bcrypt
// or stronger-parameter downgrades.
func (s *Service) NeedsRehash(hash EncodedHash) bool {
	if hash.algorithm != s.policy.config.Algorithm {
		return s.policy.config.Algorithm == Argon2id && hash.algorithm == Bcrypt
	}
	if hash.algorithm == Bcrypt {
		return hash.bcryptCost < s.policy.config.BcryptCost
	}
	want := s.policy.config.Argon2id
	got := hash.argon2id
	if got.Version != want.Version || got.Time > want.Time || got.MemoryKiB > want.MemoryKiB || got.SaltLength > want.SaltLength || got.OutputLength > want.OutputLength {
		return false
	}
	return got.Time < want.Time || got.MemoryKiB < want.MemoryKiB || got.Parallelism != want.Parallelism || got.SaltLength < want.SaltLength || got.OutputLength < want.OutputLength
}

// Policy returns the immutable service policy by value.
func (s *Service) Policy() Policy { return s.policy }

// VerifyAndUpgrade verifies before hashing and leaves durable replacement to
// the caller. On upgrade failure, the successful Result is returned with an
// empty replacement so the existing hash remains usable.
func (s *Service) VerifyAndUpgrade(ctx context.Context, password []byte, encoded string) (Result, EncodedHash, error) {
	started := time.Now()
	result, upgraded, err := s.verifyAndUpgrade(ctx, password, encoded)
	s.observe(ctx, OperationVerifyAndUpgrade, started, result.rehash, result.rehash && upgraded.String() != "", err)
	return result, upgraded, err
}

func (s *Service) verifyAndUpgrade(ctx context.Context, password []byte, encoded string) (Result, EncodedHash, error) {
	result, err := s.verify(ctx, password, encoded)
	if err != nil {
		return result, EncodedHash{}, err
	}
	if !result.rehash {
		hash, err := ParseEncodedHash(encoded, s.policy.config.Limits)
		return result, hash, err
	}
	upgraded, err := s.hash(ctx, password)
	if err != nil {
		return result, EncodedHash{}, err
	}
	return result, upgraded, nil
}
