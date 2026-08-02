package concurrencylimit

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestConfigurationRejectsEveryUnboundedOrInvalidDimension(t *testing.T) {
	t.Parallel()

	valid := Config{MinLimit: 1, MaxLimit: 10, InitialLimit: 2, Algorithm: NewFixedAlgorithm()}
	tests := map[string]func(*Config){
		"minimum zero": func(config *Config) { config.MinLimit = 0 },
		"minimum too large": func(config *Config) {
			config.MinLimit = MaxLimit + 1
			config.MaxLimit = MaxLimit + 1
			config.InitialLimit = MaxLimit + 1
		},
		"maximum below minimum": func(config *Config) { config.MaxLimit = 0 },
		"maximum too large":     func(config *Config) { config.MaxLimit = MaxLimit + 1 },
		"initial below minimum": func(config *Config) { config.InitialLimit = 0 },
		"initial above maximum": func(config *Config) { config.InitialLimit = 11 },
		"nil algorithm":         func(config *Config) { config.Algorithm = nil },
		"empty algorithm name":  func(config *Config) { config.Algorithm = &testAlgorithm{} },
		"long algorithm name": func(config *Config) {
			config.Algorithm = &testAlgorithm{name: strings.Repeat("a", maxAlgorithmNameBytes+1)}
		},
		"panicking algorithm name": func(config *Config) { config.Algorithm = &testAlgorithm{panicName: true} },
		"negative min duration":    func(config *Config) { config.Sampling.MinDuration = -1 },
		"negative max duration":    func(config *Config) { config.Sampling.MaxDuration = -1 },
		"reversed durations":       func(config *Config) { config.Sampling.MinDuration = 2; config.Sampling.MaxDuration = 1 },
		"negative min samples":     func(config *Config) { config.Sampling.MinSamples = -1 },
		"negative capacity":        func(config *Config) { config.Sampling.Capacity = -1 },
		"capacity too large":       func(config *Config) { config.Sampling.Capacity = MaxSampleCapacity + 1 },
		"samples exceed capacity":  func(config *Config) { config.Sampling.MinSamples = 2; config.Sampling.Capacity = 1 },
		"quantile negative":        func(config *Config) { config.Sampling.Quantile = -1 },
		"quantile nan":             func(config *Config) { config.Sampling.Quantile = math.NaN() },
		"smoothing negative":       func(config *Config) { config.Sampling.BaselineSmoothing = -1 },
		"smoothing infinity":       func(config *Config) { config.Sampling.BaselineSmoothing = math.Inf(1) },
		"negative increase":        func(config *Config) { config.Sampling.MaxIncrease = -1 },
		"negative decrease":        func(config *Config) { config.Sampling.MaxDecrease = -1 },
		"increase too large":       func(config *Config) { config.Sampling.MaxIncrease = MaxLimit + 1 },
		"decrease too large":       func(config *Config) { config.Sampling.MaxDecrease = MaxLimit + 1 },
		"negative queue":           func(config *Config) { config.Queue.MaxQueued = -1 },
		"queue too large":          func(config *Config) { config.Queue.MaxQueued = MaxQueued + 1; config.Queue.MaxWait = 1 },
		"negative wait":            func(config *Config) { config.Queue.MaxWait = -1 },
		"queue without wait":       func(config *Config) { config.Queue.MaxQueued = 1 },
		"wait without queue":       func(config *Config) { config.Queue.MaxWait = 1 },
		"negative ttl":             func(config *Config) { config.PermitTTL = -1 },
		"typed nil clock":          func(config *Config) { var clock *testClock; config.Clock = clock },
		"negative max priority":    func(config *Config) { config.MaxPriority = -1 },
		"max priority too large":   func(config *Config) { config.MaxPriority = MaxPriority + 1 },
		"too many partitions":      func(config *Config) { config.Partitions = make([]string, MaxPartitions+1) },
		"empty partition":          func(config *Config) { config.Partitions = []string{""} },
		"long partition":           func(config *Config) { config.Partitions = []string{strings.Repeat("p", MaxPartitionBytes+1)} },
		"duplicate partition":      func(config *Config) { config.Partitions = []string{"p", "p"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if _, err := New(config); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestAlgorithmConfigurationValidation(t *testing.T) {
	t.Parallel()

	aimd := []AIMDConfig{
		{}, {Increase: 1, DecreaseFactor: -1}, {Increase: 1, DecreaseFactor: 1},
		{Increase: MaxLimit + 1, DecreaseFactor: 0.5},
		{Increase: 1, DecreaseFactor: math.NaN()}, {Increase: 1, DecreaseFactor: 0.5, LatencyLimit: -1},
	}
	for _, config := range aimd {
		if _, err := NewAIMDAlgorithm(config); err == nil {
			t.Fatalf("NewAIMDAlgorithm(%+v) error = nil", config)
		}
	}
	vegas := []VegasConfig{
		{}, {Alpha: 0, Beta: 2, Increase: 1, Decrease: 1},
		{Alpha: 2, Beta: 2, Increase: 1, Decrease: 1},
		{Alpha: MaxLimit + 1, Beta: MaxLimit + 2, Increase: 1, Decrease: 1},
		{Alpha: 2, Beta: MaxLimit + 1, Increase: 1, Decrease: 1},
		{Alpha: 2, Beta: 4, Increase: 0, Decrease: 1},
		{Alpha: 2, Beta: 4, Increase: MaxLimit + 1, Decrease: 1},
		{Alpha: 2, Beta: 4, Increase: 1, Decrease: 0},
		{Alpha: 2, Beta: 4, Increase: 1, Decrease: MaxLimit + 1},
	}
	for _, config := range vegas {
		if _, err := NewVegasAlgorithm(config); err == nil {
			t.Fatalf("NewVegasAlgorithm(%+v) error = nil", config)
		}
	}
	gradient := []Gradient2Config{
		{}, {LongWindow: 2, Smoothing: -1, Tolerance: 1, MinGradient: 0.5},
		{LongWindow: MaxSampleCapacity + 1, Smoothing: 0.5, Tolerance: 1, MinGradient: 0.5},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: 0, MinGradient: 0.5},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: MaxLimit + 1, MinGradient: 0.5},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: math.Inf(1), MinGradient: 0.5},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: 1, MinGradient: 0},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: 1, MinGradient: math.NaN()},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: 1, MinGradient: 0.5, QueueSize: -1},
		{LongWindow: 2, Smoothing: 0.5, Tolerance: 1, MinGradient: 0.5, QueueSize: MaxLimit + 1},
	}
	for _, config := range gradient {
		if _, err := NewGradient2Algorithm(config); err == nil {
			t.Fatalf("NewGradient2Algorithm(%+v) error = nil", config)
		}
	}
}

type testAlgorithm struct {
	name           string
	state          AlgorithmState
	panicName      bool
	panicReset     bool
	panicState     bool
	panicUpdate    bool
	updateState    AlgorithmState
	useUpdateState bool
	decisionLimit  int
}

func (algorithm *testAlgorithm) Name() string {
	if algorithm.panicName {
		panic("name")
	}
	return algorithm.name
}
func (algorithm *testAlgorithm) Update(Window) Decision {
	if algorithm.panicUpdate {
		panic("update")
	}
	state := algorithm.state
	if algorithm.useUpdateState {
		state = algorithm.updateState
	}
	limit := algorithm.decisionLimit
	if limit == 0 {
		limit = 1
	}
	return Decision{Limit: limit, State: state}
}
func (algorithm *testAlgorithm) Reset(int) {
	if algorithm.panicReset {
		panic("reset")
	}
}
func (algorithm *testAlgorithm) State() AlgorithmState {
	if algorithm.panicState {
		panic("state")
	}
	return algorithm.state
}
func (*testAlgorithm) algorithm() {}

type testClock struct{}

func (*testClock) Now() time.Time               { return time.Time{} }
func (*testClock) NewTimer(time.Duration) Timer { return nil }
