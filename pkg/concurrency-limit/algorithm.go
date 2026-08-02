package concurrencylimit

import (
	"math"
	"time"
)

// Window is the immutable aggregate supplied to an Algorithm. RecentLatency
// is the configured exact quantile of a bounded recent sample window.
type Window struct {
	CurrentLimit        int
	Samples             int
	MaxInFlight         int
	RecentLatency       time.Duration
	BaselineLatency     time.Duration
	Throughput          float64
	PreviousThroughput  float64
	PreviousMaxInFlight int
	Overloads           uint64
	DependencyFailures  uint64
	generationToken     *lifecycleGeneration
}

// Decision is an algorithm's requested next limit and diagnostic state. The
// limiter applies configured step bounds and absolute min/max bounds.
type Decision struct {
	Limit int
	State AlgorithmState
}

// AlgorithmState is a bounded, value-only diagnostic description of the most
// recent algorithm update.
type AlgorithmState struct {
	Name          string
	Reason        string
	Estimate      float64
	Gradient      float64
	QueueEstimate float64
	Throughput    float64
}

// Algorithm computes a concurrency limit from completed sampling windows.
// Limiter serializes Update, Reset, and State calls and never holds its state
// lock while invoking them. Ownership transfers to Limiter at construction.
type Algorithm interface {
	Name() string
	Update(Window) Decision
	Reset(initialLimit int)
	State() AlgorithmState
	algorithm()
}

type fixedAlgorithm struct {
	limit int
	state AlgorithmState
}

// NewFixedAlgorithm returns a control algorithm that never changes the limit.
func NewFixedAlgorithm() Algorithm {
	return &fixedAlgorithm{state: AlgorithmState{Name: "fixed", Reason: "fixed"}}
}

func (*fixedAlgorithm) Name() string { return "fixed" }
func (*fixedAlgorithm) algorithm()   {}

func (algorithm *fixedAlgorithm) Reset(initialLimit int) {
	algorithm.limit = initialLimit
	algorithm.state = AlgorithmState{Name: algorithm.Name(), Reason: "reset", Estimate: float64(initialLimit)}
}

func (algorithm *fixedAlgorithm) Update(window Window) Decision {
	algorithm.limit = window.CurrentLimit
	algorithm.state = AlgorithmState{
		Name:       algorithm.Name(),
		Reason:     "fixed",
		Estimate:   float64(window.CurrentLimit),
		Throughput: window.Throughput,
	}
	return Decision{Limit: window.CurrentLimit, State: algorithm.state}
}

func (algorithm *fixedAlgorithm) State() AlgorithmState { return algorithm.state }

// AIMDConfig configures additive increase and multiplicative decrease. A
// window decreases on an overload signal or when LatencyLimit is positive and
// exceeded. It increases only when observed in-flight work reached at least
// half the current limit.
type AIMDConfig struct {
	Increase       int
	DecreaseFactor float64
	LatencyLimit   time.Duration
}

type aimdAlgorithm struct {
	config AIMDConfig
	state  AlgorithmState
}

// NewAIMDAlgorithm validates and constructs an AIMD reference algorithm.
func NewAIMDAlgorithm(config AIMDConfig) (Algorithm, error) {
	if config.Increase < 1 || config.Increase > MaxLimit {
		return nil, invalidConfig("AIMD.Increase", "must be in [1, MaxLimit]")
	}
	if !finite(config.DecreaseFactor) || config.DecreaseFactor <= 0 || config.DecreaseFactor >= 1 {
		return nil, invalidConfig("AIMD.DecreaseFactor", "must be in (0, 1)")
	}
	if config.LatencyLimit < 0 {
		return nil, invalidConfig("AIMD.LatencyLimit", "must not be negative")
	}
	return &aimdAlgorithm{config: config, state: AlgorithmState{Name: "aimd"}}, nil
}

func (*aimdAlgorithm) Name() string { return "aimd" }
func (*aimdAlgorithm) algorithm()   {}

func (algorithm *aimdAlgorithm) Reset(initialLimit int) {
	algorithm.state = AlgorithmState{Name: algorithm.Name(), Reason: "reset", Estimate: float64(initialLimit)}
}

func (algorithm *aimdAlgorithm) Update(window Window) Decision {
	next := window.CurrentLimit
	reason := "application-limited"
	if window.Overloads > 0 || (algorithm.config.LatencyLimit > 0 && window.RecentLatency > algorithm.config.LatencyLimit) {
		next = int(math.Floor(float64(window.CurrentLimit) * algorithm.config.DecreaseFactor))
		reason = "overload"
	} else if window.MaxInFlight >= (window.CurrentLimit+1)/2 {
		next += algorithm.config.Increase
		reason = "capacity"
	}
	algorithm.state = AlgorithmState{
		Name:       algorithm.Name(),
		Reason:     reason,
		Estimate:   float64(next),
		Throughput: window.Throughput,
	}
	return Decision{Limit: next, State: algorithm.state}
}

func (algorithm *aimdAlgorithm) State() AlgorithmState { return algorithm.state }

// VegasConfig configures the Vegas-style queue estimate
// ceil(limit*(1-baseline/recent)). Queue estimates below Alpha increase the
// limit; estimates above Beta decrease it.
type VegasConfig struct {
	Alpha    int
	Beta     int
	Increase int
	Decrease int
}

type vegasAlgorithm struct {
	config VegasConfig
	state  AlgorithmState
}

// NewDefaultAlgorithm returns the conservative Vegas profile exercised by the
// package's reproducible constant, bursty, bimodal, heavy-tail, sparse,
// collapse, and recovery simulations.
func NewDefaultAlgorithm() Algorithm {
	return &vegasAlgorithm{
		config: VegasConfig{Alpha: 2, Beta: 4, Increase: 1, Decrease: 1},
		state:  AlgorithmState{Name: "vegas"},
	}
}

// NewVegasAlgorithm validates and constructs a Vegas-style algorithm.
func NewVegasAlgorithm(config VegasConfig) (Algorithm, error) {
	if config.Alpha < 1 {
		return nil, invalidConfig("Vegas.Alpha", "must be positive")
	}
	if config.Beta <= config.Alpha || config.Beta > MaxLimit {
		return nil, invalidConfig("Vegas.Beta", "must be greater than Alpha and at most MaxLimit")
	}
	if config.Increase < 1 || config.Decrease < 1 || config.Increase > MaxLimit || config.Decrease > MaxLimit {
		return nil, invalidConfig("Vegas.Gain", "increase and decrease must be in [1, MaxLimit]")
	}
	return &vegasAlgorithm{config: config, state: AlgorithmState{Name: "vegas"}}, nil
}

func (*vegasAlgorithm) Name() string { return "vegas" }
func (*vegasAlgorithm) algorithm()   {}

func (algorithm *vegasAlgorithm) Reset(initialLimit int) {
	algorithm.state = AlgorithmState{Name: algorithm.Name(), Reason: "reset", Estimate: float64(initialLimit)}
}

func (algorithm *vegasAlgorithm) Update(window Window) Decision {
	next := window.CurrentLimit
	queue := queueEstimate(window.CurrentLimit, window.BaselineLatency, window.RecentLatency)
	reason := "target-queue"
	switch {
	case window.Overloads > 0:
		next -= algorithm.config.Decrease
		reason = "overload"
	case throughputStalled(window):
		next -= algorithm.config.Decrease
		reason = "throughput"
	case window.MaxInFlight < (window.CurrentLimit+1)/2:
		reason = "application-limited"
	case queue < float64(algorithm.config.Alpha):
		next += algorithm.config.Increase
		reason = "low-queue"
	case queue > float64(algorithm.config.Beta):
		next -= algorithm.config.Decrease
		reason = "high-queue"
	}
	algorithm.state = AlgorithmState{
		Name:          algorithm.Name(),
		Reason:        reason,
		Estimate:      float64(next),
		QueueEstimate: queue,
		Throughput:    window.Throughput,
	}
	return Decision{Limit: next, State: algorithm.state}
}

func (algorithm *vegasAlgorithm) State() AlgorithmState { return algorithm.state }

// Gradient2Config configures the reviewed Netflix Gradient2 equation. The
// long-window RTT is an exponential average with alpha 2/(LongWindow+1).
type Gradient2Config struct {
	LongWindow  int
	Smoothing   float64
	Tolerance   float64
	MinGradient float64
	QueueSize   int
}

type gradient2Algorithm struct {
	config        Gradient2Config
	longRTT       float64
	warmupSamples int
	estimate      float64
	state         AlgorithmState
}

// NewGradient2Algorithm validates and constructs a Gradient2 algorithm.
func NewGradient2Algorithm(config Gradient2Config) (Algorithm, error) {
	if config.LongWindow < 2 || config.LongWindow > MaxSampleCapacity {
		return nil, invalidConfig("Gradient2.LongWindow", "must be in [2, MaxSampleCapacity]")
	}
	if !unitInterval(config.Smoothing) {
		return nil, invalidConfig("Gradient2.Smoothing", "must be in (0, 1]")
	}
	if !finite(config.Tolerance) || config.Tolerance <= 0 || config.Tolerance > MaxLimit {
		return nil, invalidConfig("Gradient2.Tolerance", "must be positive, finite, and at most MaxLimit")
	}
	if !finite(config.MinGradient) || config.MinGradient <= 0 || config.MinGradient > 1 {
		return nil, invalidConfig("Gradient2.MinGradient", "must be in (0, 1]")
	}
	if config.QueueSize < 0 || config.QueueSize > MaxLimit {
		return nil, invalidConfig("Gradient2.QueueSize", "must be in [0, MaxLimit]")
	}
	return &gradient2Algorithm{config: config, state: AlgorithmState{Name: "gradient2"}}, nil
}

func (*gradient2Algorithm) Name() string { return "gradient2" }
func (*gradient2Algorithm) algorithm()   {}

func (algorithm *gradient2Algorithm) Reset(initialLimit int) {
	algorithm.longRTT = 0
	algorithm.warmupSamples = 0
	algorithm.estimate = float64(initialLimit)
	algorithm.state = AlgorithmState{Name: algorithm.Name(), Reason: "reset", Estimate: float64(initialLimit)}
}

func (algorithm *gradient2Algorithm) Update(window Window) Decision {
	shortRTT := float64(window.RecentLatency)
	if algorithm.warmupSamples < 10 {
		algorithm.warmupSamples++
		algorithm.longRTT += (shortRTT - algorithm.longRTT) / float64(algorithm.warmupSamples)
	} else {
		alpha := 2 / float64(algorithm.config.LongWindow+1)
		algorithm.longRTT += alpha * (shortRTT - algorithm.longRTT)
	}
	if shortRTT > 0 && algorithm.longRTT/shortRTT > 2 {
		algorithm.longRTT *= 0.95
	}

	estimate := algorithm.estimate
	if int(math.Floor(estimate)) != window.CurrentLimit {
		estimate = float64(window.CurrentLimit)
	}
	next := estimate
	gradient := 1.0
	reason := "application-limited"
	if float64(window.MaxInFlight) >= estimate/2 && shortRTT > 0 {
		gradient = clampFloat(algorithm.config.Tolerance*algorithm.longRTT/shortRTT, algorithm.config.MinGradient, 1)
		target := estimate*gradient + float64(algorithm.config.QueueSize)
		next = estimate*(1-algorithm.config.Smoothing) + target*algorithm.config.Smoothing
		reason = "latency-gradient"
	}
	if window.Overloads > 0 && next >= float64(window.CurrentLimit) {
		next = float64(window.CurrentLimit - 1)
		reason = "overload"
	} else if throughputStalled(window) && next >= float64(window.CurrentLimit) {
		next = float64(window.CurrentLimit - 1)
		reason = "throughput"
	}
	algorithm.state = AlgorithmState{
		Name:          algorithm.Name(),
		Reason:        reason,
		Estimate:      next,
		Gradient:      gradient,
		QueueEstimate: float64(algorithm.config.QueueSize),
		Throughput:    window.Throughput,
	}
	algorithm.estimate = next
	return Decision{Limit: int(math.Floor(next)), State: algorithm.state}
}

func (algorithm *gradient2Algorithm) State() AlgorithmState { return algorithm.state }

func queueEstimate(limit int, baseline, recent time.Duration) float64 {
	if baseline <= 0 {
		return 0
	}
	denominator := math.Max(float64(recent), math.SmallestNonzeroFloat64)
	return max(0, math.Ceil(float64(limit)*(1-float64(baseline)/denominator)))
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func unitInterval(value float64) bool { return finite(value) && value > 0 && value <= 1 }

func clampFloat(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func throughputStalled(window Window) bool {
	return window.PreviousMaxInFlight > 0 && window.MaxInFlight > window.PreviousMaxInFlight &&
		window.PreviousThroughput > 0 && window.Throughput <= window.PreviousThroughput
}
