package bulkhead

import (
	"fmt"
	"reflect"
	"regexp"
	"time"
)

const (
	// MaxResourceBytes bounds caller-owned resource labels and metric cardinality payloads.
	MaxResourceBytes = 128
	// MaxQueueSize bounds memory retained by a single bulkhead.
	MaxQueueSize = 1 << 20
	// MaxPartitions bounds memory retained by an explicit partition registry.
	MaxPartitions = 1 << 20
)

var resourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

// AdmissionPolicy selects immediate rejection or strict FIFO bounded waiting.
type AdmissionPolicy interface{ admissionPolicy() }

// RejectImmediately never queues when capacity is unavailable.
type RejectImmediately struct{}

func (RejectImmediately) admissionPolicy() { _ = "reject-immediately" }

// Wait queues at most MaxQueued callers and waits no longer than MaxWait.
// Strict FIFO ordering can cause weighted head-of-line blocking.
type Wait struct {
	MaxQueued int
	MaxWait   time.Duration
}

func (Wait) admissionPolicy() { _ = "strict-fifo-wait" }

// Clock supplies timestamps and timers for deterministic waiting and observations.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Timer is the clock-owned timer used by bounded admission.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Observer receives immutable bounded lifecycle events synchronously. It runs
// outside internal locks; its return value and panics never alter admission.
type Observer interface {
	Observe(Event) error
}

// ObserveFunc adapts a function to Observer.
type ObserveFunc func(Event) error

// Observe invokes function.
func (function ObserveFunc) Observe(event Event) error { return function(event) }

// Config contains immutable construction parameters for one resource partition.
type Config struct {
	Resource       string
	Capacity       int64
	Admission      AdmissionPolicy
	Observer       Observer
	Clock          Clock
	PolicyRevision string
}

type normalizedConfig struct {
	resource       string
	capacity       int64
	wait           bool
	maxQueued      int
	maxWait        time.Duration
	observer       Observer
	clock          Clock
	policyRevision string
}

func normalize(config Config) (normalizedConfig, error) {
	if config.Resource == "" || len(config.Resource) > MaxResourceBytes || !resourcePattern.MatchString(config.Resource) {
		return normalizedConfig{}, invalidConfig("Resource", "must be a bounded metric-safe identifier")
	}
	if config.Capacity <= 0 {
		return normalizedConfig{}, invalidConfig("Capacity", "must be positive")
	}
	if len(config.PolicyRevision) > MaxResourceBytes ||
		(config.PolicyRevision != "" && !resourcePattern.MatchString(config.PolicyRevision)) {
		return normalizedConfig{}, invalidConfig("PolicyRevision", "must be empty or a bounded metric-safe identifier")
	}
	if nilLike(config.Clock) || nilLike(config.Observer) || nilLike(config.Admission) {
		return normalizedConfig{}, invalidConfig("dependency", "must not be a typed nil")
	}

	normalized := normalizedConfig{
		resource:       config.Resource,
		capacity:       config.Capacity,
		observer:       config.Observer,
		clock:          config.Clock,
		policyRevision: config.PolicyRevision,
	}
	if normalized.clock == nil {
		normalized.clock = systemClock{}
	}

	switch policy := config.Admission.(type) {
	case nil, RejectImmediately:
	case Wait:
		if policy.MaxQueued <= 0 || policy.MaxQueued > MaxQueueSize {
			return normalizedConfig{}, invalidConfig("Admission.MaxQueued", "must be positive and bounded")
		}
		if policy.MaxWait <= 0 {
			return normalizedConfig{}, invalidConfig("Admission.MaxWait", "must be positive")
		}
		normalized.wait = true
		normalized.maxQueued = policy.MaxQueued
		normalized.maxWait = policy.MaxWait
	default:
		return normalizedConfig{}, invalidConfig("Admission", "unsupported policy")
	}

	return normalized, nil
}

func invalidConfig(field, message string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidConfig, field, message)
}

func nilLike(value any) bool {
	if value == nil {
		return false
	}
	kind := reflect.ValueOf(value).Kind()
	return (kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice) &&
		reflect.ValueOf(value).IsNil()
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTimer(duration time.Duration) Timer {
	return systemTimer{Timer: time.NewTimer(duration)}
}

type systemTimer struct{ *time.Timer }

func (timer systemTimer) C() <-chan time.Time { return timer.Timer.C }
