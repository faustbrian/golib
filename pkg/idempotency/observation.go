package idempotency

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

const minCorrelationSecretBytes = 32

// KeyHasher returns a non-reversible correlation value for a logical key.
type KeyHasher func(Key) string

// Transition identifies one fixed-cardinality semantic service operation.
type Transition string

const (
	// TransitionAcquire identifies a Service.Begin acquisition attempt.
	TransitionAcquire Transition = "acquire"
	// TransitionInspect identifies a Service.Inspect read.
	TransitionInspect Transition = "inspect"
	// TransitionHeartbeat identifies a Service.Heartbeat lease extension.
	TransitionHeartbeat Transition = "heartbeat"
	// TransitionComplete identifies a Service.Complete terminal transition.
	TransitionComplete Transition = "complete"
	// TransitionFail identifies a Service.Fail terminal transition.
	TransitionFail Transition = "fail"
	// TransitionRelease identifies a Service.Release abandonment transition.
	TransitionRelease Transition = "release"
	// TransitionExpire identifies a Service.Expire audit transition.
	TransitionExpire Transition = "expire"
)

// Observation is a bounded service transition signal safe for instrumentation.
// Correlation is suitable for restricted logs, never metric labels.
type Observation struct {
	// Transition identifies the semantic operation.
	Transition Transition
	// Outcome is populated for acquisition attempts.
	Outcome Outcome
	// Reason classifies a failed transition and is empty on success.
	Reason Reason
	// Durable reports whether the requested state or result is durably established.
	Durable bool
	// Correlation is a keyed digest when ServiceOptions provides a KeyHasher.
	Correlation string
}

// Observer receives bounded semantic service transition signals. Service
// isolates observer and key-hasher panics so instrumentation cannot change a
// semantic result. Implementations should still return quickly and honor ctx.
type Observer interface {
	Observe(context.Context, Observation)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Observation)

// Observe calls f with the bounded transition signal.
func (f ObserverFunc) Observe(ctx context.Context, observation Observation) {
	f(ctx, observation)
}

// ServiceOptions configures optional bounded service instrumentation.
type ServiceOptions struct {
	// Observer receives one signal after each instrumented semantic transition.
	Observer Observer
	// KeyHasher produces restricted-log correlation without exposing logical keys.
	KeyHasher KeyHasher
}

// NewHMACKeyHasher constructs a deterministic SHA-256 HMAC key hasher.
// The secret is copied and must contain at least 32 bytes.
func NewHMACKeyHasher(secret []byte) (KeyHasher, error) {
	if len(secret) < minCorrelationSecretBytes {
		return nil, &Error{
			Reason: ReasonInvalidConfiguration,
			Field:  "correlation_secret",
		}
	}
	copied := append([]byte(nil), secret...)
	return func(key Key) string {
		digest := hmac.New(sha256.New, copied)
		var size [4]byte
		for _, part := range []string{
			key.Namespace(), key.Tenant(), key.Operation(), key.Caller(), key.Value(),
		} {
			binary.BigEndian.PutUint32(size[:], uint32(len(part)))
			_, _ = digest.Write(size[:])
			_, _ = digest.Write([]byte(part))
		}
		return hex.EncodeToString(digest.Sum(nil))
	}, nil
}
