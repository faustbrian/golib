package concurrencylimit

import (
	"fmt"
	"reflect"
	"time"
)

const (
	defaultMinWindow       = 100 * time.Millisecond
	defaultMaxWindow       = time.Second
	defaultMinSamples      = 20
	defaultSampleCapacity  = 256
	defaultQuantile        = 0.9
	defaultBaselineSmooth  = 0.1
	defaultPermitTTL       = 5 * time.Minute
	MaxLimit               = 1 << 30
	MaxSampleCapacity      = 1 << 16
	MaxQueued              = 1 << 16
	MaxPartitions          = 64
	MaxPartitionBytes      = 128
	MaxPriority            = 1024
	maxAlgorithmNameBytes  = 64
	maxAlgorithmStateBytes = 128
)

// SamplingConfig bounds recent latency aggregation and adaptation steps.
// Quantiles are exact over at most Capacity retained samples.
type SamplingConfig struct {
	MinDuration       time.Duration
	MaxDuration       time.Duration
	MinSamples        int
	Capacity          int
	Quantile          float64
	BaselineSmoothing float64
	MaxIncrease       int
	MaxDecrease       int
}

// QueueConfig enables bounded FIFO waiting when both fields are positive. A
// zero value selects immediate rejection.
type QueueConfig struct {
	MaxQueued int
	MaxWait   time.Duration
}

// Clock supplies monotonic-capable timestamps and context-aware queue timers.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Timer is the minimal clock-owned timer used by queued acquisition.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Classifier maps an execution completion to exactly one terminal outcome.
// It runs without limiter locks and must not retain Completion.Context.
type Classifier func(Completion) Outcome

// Observer receives immutable value-only events without limiter locks.
type Observer func(Event)

// Config contains immutable limiter construction parameters.
type Config struct {
	MinLimit     int
	MaxLimit     int
	InitialLimit int
	Algorithm    Algorithm
	Sampling     SamplingConfig
	Queue        QueueConfig
	PermitTTL    time.Duration
	Clock        Clock
	Classifier   Classifier
	Observer     Observer
	MaxPriority  int
	Partitions   []string
}

type normalizedConfig struct {
	minLimit     int
	maxLimit     int
	initialLimit int
	algorithm    Algorithm
	sampling     SamplingConfig
	queue        QueueConfig
	permitTTL    time.Duration
	clock        Clock
	classifier   Classifier
	observer     Observer
	maxPriority  int
	partitions   map[string]struct{}
}

func normalizeConfig(config Config) (normalizedConfig, error) {
	if config.MinLimit < 1 || config.MinLimit > MaxLimit {
		return normalizedConfig{}, invalidConfig("MinLimit", "must be in [1, MaxLimit]")
	}
	if config.MaxLimit < config.MinLimit || config.MaxLimit > MaxLimit {
		return normalizedConfig{}, invalidConfig("MaxLimit", "must be within MinLimit and MaxLimit")
	}
	if config.InitialLimit < config.MinLimit || config.InitialLimit > config.MaxLimit {
		return normalizedConfig{}, invalidConfig("InitialLimit", "must be within MinLimit and MaxLimit")
	}
	if nilInterface(config.Algorithm) {
		return normalizedConfig{}, invalidConfig("Algorithm", "must not be nil")
	}
	name, panicked := safeAlgorithmName(config.Algorithm)
	if panicked || name == "" || len(name) > maxAlgorithmNameBytes {
		return normalizedConfig{}, invalidConfig("Algorithm.Name", "must be nonempty and bounded")
	}

	sampling, err := normalizeSampling(config.Sampling)
	if err != nil {
		return normalizedConfig{}, err
	}
	queue, err := normalizeQueue(config.Queue)
	if err != nil {
		return normalizedConfig{}, err
	}
	if config.PermitTTL < 0 {
		return normalizedConfig{}, invalidConfig("PermitTTL", "must not be negative")
	}
	permitTTL := config.PermitTTL
	if permitTTL == 0 {
		permitTTL = defaultPermitTTL
	}
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	} else if nilInterface(clock) {
		return normalizedConfig{}, invalidConfig("Clock", "must not be nil")
	}
	classifier := config.Classifier
	if classifier == nil {
		classifier = defaultClassifier
	}
	if config.MaxPriority < 0 || config.MaxPriority > MaxPriority {
		return normalizedConfig{}, invalidConfig("MaxPriority", "is outside the supported range")
	}
	partitions, err := normalizePartitions(config.Partitions)
	if err != nil {
		return normalizedConfig{}, err
	}

	return normalizedConfig{
		minLimit: config.MinLimit, maxLimit: config.MaxLimit, initialLimit: config.InitialLimit,
		algorithm: config.Algorithm, sampling: sampling, queue: queue, permitTTL: permitTTL,
		clock: clock, classifier: classifier, observer: config.Observer,
		maxPriority: config.MaxPriority, partitions: partitions,
	}, nil
}

func normalizeSampling(config SamplingConfig) (SamplingConfig, error) {
	if config.MinDuration < 0 || config.MaxDuration < 0 {
		return SamplingConfig{}, invalidConfig("Sampling.Duration", "must not be negative")
	}
	if config.MinDuration == 0 {
		config.MinDuration = defaultMinWindow
	}
	if config.MaxDuration == 0 {
		config.MaxDuration = defaultMaxWindow
	}
	if config.MaxDuration < config.MinDuration {
		return SamplingConfig{}, invalidConfig("Sampling.MaxDuration", "must not be less than MinDuration")
	}
	if config.MinSamples < 0 || config.Capacity < 0 {
		return SamplingConfig{}, invalidConfig("Sampling.Size", "must not be negative")
	}
	if config.MinSamples == 0 {
		config.MinSamples = defaultMinSamples
	}
	if config.Capacity == 0 {
		config.Capacity = defaultSampleCapacity
	}
	if config.Capacity > MaxSampleCapacity || config.MinSamples > config.Capacity {
		return SamplingConfig{}, invalidConfig("Sampling.Capacity", "must bound at least MinSamples")
	}
	if config.Quantile == 0 {
		config.Quantile = defaultQuantile
	}
	if !unitInterval(config.Quantile) {
		return SamplingConfig{}, invalidConfig("Sampling.Quantile", "must be in (0, 1]")
	}
	if config.BaselineSmoothing == 0 {
		config.BaselineSmoothing = defaultBaselineSmooth
	}
	if !unitInterval(config.BaselineSmoothing) {
		return SamplingConfig{}, invalidConfig("Sampling.BaselineSmoothing", "must be in (0, 1]")
	}
	if config.MaxIncrease < 0 || config.MaxDecrease < 0 || config.MaxIncrease > MaxLimit || config.MaxDecrease > MaxLimit {
		return SamplingConfig{}, invalidConfig("Sampling.Step", "must be within arithmetic bounds")
	}
	if config.MaxIncrease == 0 {
		config.MaxIncrease = 1
	}
	if config.MaxDecrease == 0 {
		config.MaxDecrease = MaxLimit
	}
	return config, nil
}

func normalizeQueue(config QueueConfig) (QueueConfig, error) {
	if config.MaxQueued < 0 || config.MaxQueued > MaxQueued || config.MaxWait < 0 {
		return QueueConfig{}, invalidConfig("Queue", "has invalid bounds")
	}
	if (config.MaxQueued == 0) != (config.MaxWait == 0) {
		return QueueConfig{}, invalidConfig("Queue", "MaxQueued and MaxWait must both be zero or positive")
	}
	return config, nil
}

func normalizePartitions(values []string) (map[string]struct{}, error) {
	if len(values) > MaxPartitions {
		return nil, invalidConfig("Partitions", "exceeds bounded cardinality")
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || len(value) > MaxPartitionBytes {
			return nil, invalidConfig("Partitions", "contains an invalid key")
		}
		if _, exists := result[value]; exists {
			return nil, invalidConfig("Partitions", "contains a duplicate key")
		}
		result[value] = struct{}{}
	}
	return result, nil
}

func invalidConfig(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidConfig, field, reason)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func safeAlgorithmName(algorithm Algorithm) (name string, panicked bool) {
	defer func() { panicked = recover() != nil }()
	algorithm.algorithm()
	return algorithm.Name(), false
}
