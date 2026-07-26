package breaker

import (
	"context"
	"math"
	rand "math/rand/v2"
	"reflect"
	"time"

	windowpkg "github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

const (
	defaultCountWindowSize    = 100
	defaultMinimumThroughput  = 10
	defaultSlowCallDuration   = 30 * time.Second
	defaultOpenDuration       = 30 * time.Second
	defaultHalfOpenProbeCount = 10
	defaultPermitTTL          = 5 * time.Minute
	// MaxHalfOpenProbes bounds simultaneously retained half-open permits.
	MaxHalfOpenProbes = 1 << 16
	// MaxEventBuffer bounds the asynchronous observer queue allocation.
	MaxEventBuffer = 1 << 16
	// MaxNameBytes bounds retained breaker identity and diagnostic rendering.
	MaxNameBytes = 256
)

// WindowConfig selects a bounded rolling outcome window.
type WindowConfig interface{ windowConfig() }

// CountWindow retains the most recent Size outcomes.
type CountWindow struct{ Size int }

func (CountWindow) windowConfig() {}

// TimeWindow retains aggregates in a bounded number of fixed-duration buckets.
type TimeWindow struct {
	BucketDuration time.Duration
	BucketCount    int
}

func (TimeWindow) windowConfig() {}

// RuleCombination determines how multiple enabled opening thresholds compose.
type RuleCombination uint8

const (
	OpenWhenAny RuleCombination = iota
	OpenWhenAll
)

// IgnoredConsecutiveBehavior controls whether an ignored completion changes
// the consecutive-failure streak.
type IgnoredConsecutiveBehavior uint8

const (
	PreserveConsecutiveFailures IgnoredConsecutiveBehavior = iota
	ResetConsecutiveFailures
)

// OpeningRules contains policy thresholds. Zero disables an individual rule.
// Ratios are expressed in the inclusive range (0, 1].
type OpeningRules struct {
	ConsecutiveFailures uint64
	FailureCount        uint64
	FailureRatio        float64
	SlowCount           uint64
	SlowRatio           float64
	Combination         RuleCombination
	IgnoredBehavior     IgnoredConsecutiveBehavior
}

// OpenDurationPolicy selects a bounded schedule for policy-driven openings.
type OpenDurationPolicy interface{ openDurationPolicy() }

// Random supplies deterministic samples in the range [0, 1) for jitter.
type Random interface{ Float64() float64 }

// FixedOpenDuration uses the same positive duration for every opening.
type FixedOpenDuration time.Duration

func (FixedOpenDuration) openDurationPolicy() {}

// ExponentialOpenDuration grows on each reopening and is capped at Maximum.
type ExponentialOpenDuration struct {
	Initial    time.Duration
	Multiplier float64
	Maximum    time.Duration
}

func (ExponentialOpenDuration) openDurationPolicy() {}

// HalfOpenFailureAction controls when a failed probe reopens the breaker.
type HalfOpenFailureAction uint8

const (
	ReopenImmediately HalfOpenFailureAction = iota
	ReopenAfterSample
)

// HalfOpenPolicy bounds probes and selects exactly one recovery threshold.
type HalfOpenPolicy struct {
	MaxProbes         int
	RequiredSuccesses int
	SuccessRatio      float64
	FailureAction     HalfOpenFailureAction
}

// HalfOpenAdmissionPolicy controls excess callers while probe capacity is full.
type HalfOpenAdmissionPolicy interface{ halfOpenAdmissionPolicy() }

// RejectExcessProbes rejects callers immediately when all probes are active.
type RejectExcessProbes struct{}

func (RejectExcessProbes) halfOpenAdmissionPolicy() {}

// WaitForProbe waits context-sensitively for capacity up to MaxWait.
type WaitForProbe struct{ MaxWait time.Duration }

func (WaitForProbe) halfOpenAdmissionPolicy() {}

// Clock supplies deterministic wall and monotonic-capable timestamps.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Timer is the minimal clock-owned timer used by context-aware admission.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Completion is the ephemeral input to an outcome classifier. Implementations
// must not retain Context, Result, or Err after returning.
type Completion struct {
	Context  context.Context
	Result   any
	Err      error
	Duration time.Duration
}

// Classifier maps one admitted completion to exactly one outcome.
type Classifier func(Completion) Outcome

// Config contains immutable breaker construction parameters.
type Config struct {
	Name               string
	Window             WindowConfig
	MinimumThroughput  int
	SlowCallDuration   time.Duration
	Opening            *OpeningRules
	OpenDuration       OpenDurationPolicy
	HalfOpen           *HalfOpenPolicy
	Clock              Clock
	Classifier         Classifier
	PermitTTL          time.Duration
	HalfOpenAdmission  HalfOpenAdmissionPolicy
	Observer           Observer
	EventDelivery      EventDeliveryPolicy
	OpenDurationJitter float64
	Random             Random
}

type normalizedConfig struct {
	name               string
	countWindowSize    int
	timeWindow         *TimeWindow
	minimumThroughput  int
	slowCallDuration   time.Duration
	opening            OpeningRules
	openDuration       OpenDurationPolicy
	halfOpen           HalfOpenPolicy
	clock              Clock
	classifier         Classifier
	permitTTL          time.Duration
	halfOpenMaxWait    time.Duration
	observer           observerRuntime
	openDurationJitter float64
	random             Random
}

func normalizeConfig(config Config) (normalizedConfig, error) {
	if config.Name == "" {
		return normalizedConfig{}, invalidConfig("Name", "must not be empty")
	}
	if len(config.Name) > MaxNameBytes {
		return normalizedConfig{}, invalidConfig("Name", "exceeds maximum length")
	}

	windowSize, timeWindow, err := normalizeWindow(config.Window)
	if err != nil {
		return normalizedConfig{}, err
	}
	minimumThroughput := config.MinimumThroughput
	if minimumThroughput == 0 {
		minimumThroughput = defaultMinimumThroughput
	}
	if minimumThroughput < 0 {
		return normalizedConfig{}, invalidConfig("MinimumThroughput", "must not be negative")
	}
	if timeWindow == nil && minimumThroughput > windowSize {
		return normalizedConfig{}, invalidConfig("MinimumThroughput", "must not exceed count window size")
	}

	slowCallDuration := config.SlowCallDuration
	if slowCallDuration == 0 {
		slowCallDuration = defaultSlowCallDuration
	}
	if slowCallDuration < 0 {
		return normalizedConfig{}, invalidConfig("SlowCallDuration", "must not be negative")
	}

	opening, err := normalizeOpening(config.Opening)
	if err != nil {
		return normalizedConfig{}, err
	}
	if timeWindow == nil {
		if opening.FailureCount > uint64(windowSize) {
			return normalizedConfig{}, invalidConfig("Opening.FailureCount", "must not exceed count window size")
		}
		if opening.SlowCount > uint64(windowSize) {
			return normalizedConfig{}, invalidConfig("Opening.SlowCount", "must not exceed count window size")
		}
	}
	openDuration, err := normalizeOpenDuration(config.OpenDuration)
	if err != nil {
		return normalizedConfig{}, err
	}
	halfOpen, err := normalizeHalfOpen(config.HalfOpen)
	if err != nil {
		return normalizedConfig{}, err
	}
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	} else if nilValue(clock) {
		return normalizedConfig{}, invalidConfig("Clock", "must not be nil")
	}
	classifier := config.Classifier
	if classifier == nil {
		classifier = defaultClassifier
	}
	permitTTL := config.PermitTTL
	if permitTTL == 0 {
		permitTTL = defaultPermitTTL
	}
	if permitTTL < 0 {
		return normalizedConfig{}, invalidConfig("PermitTTL", "must not be negative")
	}
	halfOpenMaxWait, err := normalizeHalfOpenAdmission(config.HalfOpenAdmission)
	if err != nil {
		return normalizedConfig{}, err
	}
	observer, err := normalizeObserver(config.Observer, config.EventDelivery)
	if err != nil {
		return normalizedConfig{}, err
	}
	if !finite(config.OpenDurationJitter) ||
		config.OpenDurationJitter < 0 || config.OpenDurationJitter >= 1 {
		return normalizedConfig{}, invalidConfig("OpenDurationJitter", "must be in the range [0, 1)")
	}
	random := config.Random
	if random == nil {
		random = standardRandom{}
	} else if nilValue(random) {
		return normalizedConfig{}, invalidConfig("Random", "must not be nil")
	}

	return normalizedConfig{
		name:               config.Name,
		countWindowSize:    windowSize,
		timeWindow:         timeWindow,
		minimumThroughput:  minimumThroughput,
		slowCallDuration:   slowCallDuration,
		opening:            opening,
		openDuration:       openDuration,
		halfOpen:           halfOpen,
		clock:              clock,
		classifier:         classifier,
		permitTTL:          permitTTL,
		halfOpenMaxWait:    halfOpenMaxWait,
		observer:           observer,
		openDurationJitter: config.OpenDurationJitter,
		random:             random,
	}, nil
}

func normalizeObserver(observer Observer, delivery EventDeliveryPolicy) (observerRuntime, error) {
	if observer == nil {
		if delivery != nil {
			return observerRuntime{}, invalidConfig("EventDelivery", "requires an Observer")
		}
		return observerRuntime{}, nil
	}
	if delivery == nil {
		return observerRuntime{
			observer: observer,
			buffer:   64,
			overflow: DropNewestEvent,
		}, nil
	}
	switch policy := delivery.(type) {
	case SynchronousEvents:
		return observerRuntime{observer: observer, sync: true}, nil
	case AsynchronousEvents:
		if policy.Buffer <= 0 {
			return observerRuntime{}, invalidConfig("EventDelivery.Buffer", "must be greater than zero")
		}
		if policy.Buffer > MaxEventBuffer {
			return observerRuntime{}, invalidConfig("EventDelivery.Buffer", "exceeds maximum")
		}
		if policy.Overflow != DropNewestEvent && policy.Overflow != DropOldestEvent {
			return observerRuntime{}, invalidConfig("EventDelivery.Overflow", "is unknown")
		}
		return observerRuntime{
			observer: observer,
			buffer:   policy.Buffer,
			overflow: policy.Overflow,
		}, nil
	default:
		return observerRuntime{}, invalidConfig("EventDelivery", "unsupported delivery policy")
	}
}

func normalizeHalfOpenAdmission(config HalfOpenAdmissionPolicy) (time.Duration, error) {
	switch policy := config.(type) {
	case nil, RejectExcessProbes:
		return 0, nil
	case WaitForProbe:
		if policy.MaxWait <= 0 {
			return 0, invalidConfig("HalfOpenAdmission.MaxWait", "must be greater than zero")
		}
		return policy.MaxWait, nil
	default:
		return 0, invalidConfig("HalfOpenAdmission", "unsupported admission policy")
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTimer(duration time.Duration) Timer {
	return systemTimer{Timer: time.NewTimer(duration)}
}

type systemTimer struct{ *time.Timer }

func (t systemTimer) C() <-chan time.Time { return t.Timer.C }

type standardRandom struct{}

func (standardRandom) Float64() float64 { return rand.Float64() }

func defaultClassifier(completion Completion) Outcome {
	if completion.Err != nil {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

func normalizeWindow(config WindowConfig) (int, *TimeWindow, error) {
	switch window := config.(type) {
	case nil:
		return defaultCountWindowSize, nil, nil
	case CountWindow:
		if window.Size <= 0 {
			return 0, nil, invalidConfig("Window.Size", "must be greater than zero")
		}
		if window.Size > windowpkg.MaxCountSize {
			return 0, nil, invalidConfig("Window.Size", "exceeds maximum")
		}
		return window.Size, nil, nil
	case TimeWindow:
		if window.BucketDuration <= 0 {
			return 0, nil, invalidConfig("Window.BucketDuration", "must be greater than zero")
		}
		if window.BucketCount <= 0 {
			return 0, nil, invalidConfig("Window.BucketCount", "must be greater than zero")
		}
		if window.BucketCount > windowpkg.MaxBucketCount {
			return 0, nil, invalidConfig("Window.BucketCount", "exceeds maximum")
		}
		if window.BucketDuration > time.Duration(1<<63-1)/time.Duration(window.BucketCount) {
			return 0, nil, invalidConfig("Window", "rolling interval overflows time.Duration")
		}
		return 0, &window, nil
	default:
		return 0, nil, invalidConfig("Window", "unsupported window configuration")
	}
}

func normalizeOpening(config *OpeningRules) (OpeningRules, error) {
	if config == nil {
		return OpeningRules{FailureRatio: 0.5, Combination: OpenWhenAny}, nil
	}
	rules := *config
	if rules.Combination != OpenWhenAny && rules.Combination != OpenWhenAll {
		return OpeningRules{}, invalidConfig("Opening.Combination", "is unknown")
	}
	if rules.IgnoredBehavior != PreserveConsecutiveFailures &&
		rules.IgnoredBehavior != ResetConsecutiveFailures {
		return OpeningRules{}, invalidConfig("Opening.IgnoredBehavior", "is unknown")
	}
	if !finite(rules.FailureRatio) || rules.FailureRatio < 0 || rules.FailureRatio > 1 {
		return OpeningRules{}, invalidConfig("Opening.FailureRatio", "must be in the range [0, 1]")
	}
	if !finite(rules.SlowRatio) || rules.SlowRatio < 0 || rules.SlowRatio > 1 {
		return OpeningRules{}, invalidConfig("Opening.SlowRatio", "must be in the range [0, 1]")
	}
	if rules.ConsecutiveFailures == 0 && rules.FailureCount == 0 &&
		rules.FailureRatio == 0 && rules.SlowCount == 0 && rules.SlowRatio == 0 {
		return OpeningRules{}, invalidConfig("Opening", "must enable at least one rule")
	}
	return rules, nil
}

func normalizeOpenDuration(config OpenDurationPolicy) (OpenDurationPolicy, error) {
	if config == nil {
		return FixedOpenDuration(defaultOpenDuration), nil
	}
	switch policy := config.(type) {
	case FixedOpenDuration:
		if policy <= 0 {
			return nil, invalidConfig("OpenDuration", "must be greater than zero")
		}
		return policy, nil
	case ExponentialOpenDuration:
		if policy.Initial <= 0 {
			return nil, invalidConfig("OpenDuration.Initial", "must be greater than zero")
		}
		if !finite(policy.Multiplier) || policy.Multiplier < 1 {
			return nil, invalidConfig("OpenDuration.Multiplier", "must be at least one")
		}
		if policy.Maximum < policy.Initial {
			return nil, invalidConfig("OpenDuration.Maximum", "must not be less than Initial")
		}
		return policy, nil
	default:
		return nil, invalidConfig("OpenDuration", "unsupported duration policy")
	}
}

func normalizeHalfOpen(config *HalfOpenPolicy) (HalfOpenPolicy, error) {
	if config == nil {
		return HalfOpenPolicy{
			MaxProbes:         defaultHalfOpenProbeCount,
			RequiredSuccesses: defaultHalfOpenProbeCount,
			FailureAction:     ReopenImmediately,
		}, nil
	}
	policy := *config
	if policy.MaxProbes <= 0 {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen.MaxProbes", "must be greater than zero")
	}
	if policy.MaxProbes > MaxHalfOpenProbes {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen.MaxProbes", "exceeds maximum")
	}
	if policy.FailureAction != ReopenImmediately && policy.FailureAction != ReopenAfterSample {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen.FailureAction", "is unknown")
	}
	if !finite(policy.SuccessRatio) || policy.SuccessRatio < 0 || policy.SuccessRatio > 1 {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen.SuccessRatio", "must be in the range [0, 1]")
	}
	if policy.RequiredSuccesses > 0 && policy.SuccessRatio > 0 {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen", "must select one recovery threshold")
	}
	if policy.RequiredSuccesses <= 0 && policy.SuccessRatio == 0 {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen", "must select a recovery threshold")
	}
	if policy.RequiredSuccesses > policy.MaxProbes {
		return HalfOpenPolicy{}, invalidConfig("HalfOpen.RequiredSuccesses", "must not exceed MaxProbes")
	}
	return policy, nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func nilValue(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func invalidConfig(field, message string) error {
	return &InvalidConfigError{Field: field, Message: message}
}
