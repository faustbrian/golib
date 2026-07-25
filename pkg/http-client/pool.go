package httpclient

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrInvalidPool indicates malformed pool policy, source, or metadata.
	ErrInvalidPool = errors.New("invalid HTTP request pool")
	// ErrPoolLimit indicates that a finite pool-wide budget was reached.
	ErrPoolLimit = errors.New("HTTP request pool limit reached")
)

const (
	defaultPoolConcurrency     = 4
	defaultPoolMaximumRequests = 10_000
	defaultPoolMaximumElapsed  = 5 * time.Minute
	defaultPoolMaximumBytes    = 64 << 20
	maximumPoolConcurrency     = 1 << 10
	maximumPoolPending         = 1 << 20
)

// PoolResultOrder controls result presentation independently from execution.
type PoolResultOrder uint8

const (
	// PoolInputOrder returns results in stable source order.
	PoolInputOrder PoolResultOrder = iota
	// PoolCompletionOrder returns results as workers complete.
	PoolCompletionOrder
)

// PoolFailureMode controls request-error cancellation.
type PoolFailureMode uint8

const (
	// PoolCollectAll retains every per-request result and error.
	PoolCollectAll PoolFailureMode = iota
	// PoolFailFast cancels pending work after the first request error.
	PoolFailFast
)

// PoolValue is one successful typed value and its explicit budget accounting.
type PoolValue[Output any] struct {
	Value         Output
	ResponseBytes int64
	MemoryBytes   int64
}

// PoolExecutor executes one input through caller-owned HTTP policy.
type PoolExecutor[Input any, Output any] func(
	context.Context,
	Input,
) (PoolValue[Output], error)

// PoolKey returns a stable low-cardinality result key.
type PoolKey[Input any] func(Input) (string, error)

// PoolGenerator yields one input at a time and must honor context cancellation.
type PoolGenerator[Input any] func(context.Context) (Input, bool, error)

// PoolWorkload describes source shape for bounded concurrency selection.
type PoolWorkload struct {
	KnownRequests int
}

// PoolConcurrencySelector chooses one run-wide worker count within configured
// minimum and maximum bounds.
type PoolConcurrencySelector func(PoolWorkload) int

// PoolLimits are finite run-wide budgets. Zero fields select safe defaults.
type PoolLimits struct {
	MaximumRequests      int
	MaximumElapsed       time.Duration
	MaximumResponseBytes int64
	MaximumMemoryBytes   int64
}

// PoolOptions configures one immutable typed execution pool.
type PoolOptions[Input any, Output any] struct {
	Concurrency        int
	MinimumConcurrency int
	MaximumConcurrency int
	SelectConcurrency  PoolConcurrencySelector
	Pending            int
	Order              PoolResultOrder
	Failure            PoolFailureMode
	Limits             PoolLimits
	Clock              RetryClock
	Key                PoolKey[Input]
	Execute            PoolExecutor[Input, Output]
}

// PoolResult preserves one source item, typed value, metadata, and independent
// request failure.
type PoolResult[Input any, Output any] struct {
	Index         int
	Key           string
	Input         Input
	Value         Output
	ResponseBytes int64
	MemoryBytes   int64
	Error         error
}

// PoolError reports source, cancellation, fail-fast, or budget termination
// without rendering input, key, response, or underlying error text.
type PoolError struct {
	Completed int
	Cause     error
}

// Error implements error without rendering potentially sensitive causes.
func (*PoolError) Error() string { return "HTTP request pool failed" }

// Unwrap returns the termination cause.
func (err *PoolError) Unwrap() error { return err.Cause }

// PoolPanicError reports a contained selector, source, key, or executor panic.
// Value remains available programmatically but is never rendered.
type PoolPanicError struct {
	Stage string
	Value any
}

// Error implements error without rendering the panic value.
func (err *PoolPanicError) Error() string {
	return fmt.Sprintf("HTTP request pool %s callback panicked", err.Stage)
}

// Pool is an immutable reusable worker policy.
type Pool[Input any, Output any] struct {
	concurrency int
	minimum     int
	maximum     int
	selector    PoolConcurrencySelector
	pending     int
	order       PoolResultOrder
	failure     PoolFailureMode
	limits      PoolLimits
	clock       RetryClock
	key         PoolKey[Input]
	execute     PoolExecutor[Input, Output]
}

// NewPool validates and constructs a reusable pool without starting workers.
func NewPool[Input any, Output any](
	options PoolOptions[Input, Output],
) (*Pool[Input, Output], error) {
	if nilLike(options.Execute) {
		return nil, fmt.Errorf("%w: executor is nil", ErrInvalidPool)
	}
	if options.Order > PoolCompletionOrder || options.Failure > PoolFailFast {
		return nil, fmt.Errorf("%w: enum policy is unknown", ErrInvalidPool)
	}
	minimum := options.MinimumConcurrency
	if minimum == 0 {
		minimum = 1
	}
	maximum := options.MaximumConcurrency
	if maximum == 0 {
		maximum = 32
	}
	concurrency := options.Concurrency
	if concurrency == 0 {
		concurrency = defaultPoolConcurrency
	}
	if minimum < 1 || maximum < minimum || maximum > maximumPoolConcurrency ||
		concurrency < minimum || concurrency > maximum {
		return nil, fmt.Errorf("%w: concurrency bounds are invalid", ErrInvalidPool)
	}
	pending := options.Pending
	if pending == 0 {
		pending = concurrency
	}
	if pending < 1 || pending > maximumPoolPending {
		return nil, fmt.Errorf("%w: pending bound is invalid", ErrInvalidPool)
	}
	limits, err := resolvePoolLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidPool)
	}
	return &Pool[Input, Output]{
		concurrency: concurrency, minimum: minimum, maximum: maximum,
		selector: options.SelectConcurrency, pending: pending,
		order: options.Order, failure: options.Failure, limits: limits,
		clock: clock, key: options.Key, execute: options.Execute,
	}, nil
}

func resolvePoolLimits(limits PoolLimits) (PoolLimits, error) {
	if limits.MaximumRequests == 0 {
		limits.MaximumRequests = defaultPoolMaximumRequests
	}
	if limits.MaximumElapsed == 0 {
		limits.MaximumElapsed = defaultPoolMaximumElapsed
	}
	if limits.MaximumResponseBytes == 0 {
		limits.MaximumResponseBytes = defaultPoolMaximumBytes
	}
	if limits.MaximumMemoryBytes == 0 {
		limits.MaximumMemoryBytes = defaultPoolMaximumBytes
	}
	if limits.MaximumRequests < 0 || limits.MaximumElapsed < 0 ||
		limits.MaximumResponseBytes < 0 || limits.MaximumMemoryBytes < 0 {
		return PoolLimits{}, fmt.Errorf("%w: limit is negative", ErrInvalidPool)
	}

	return limits, nil
}

// RunSlice executes a finite snapshot of inputs.
func (pool *Pool[Input, Output]) RunSlice(
	ctx context.Context,
	inputs []Input,
) ([]PoolResult[Input, Output], error) {
	snapshot := append([]Input(nil), inputs...)
	index := 0

	return pool.run(ctx, len(snapshot), func(context.Context) (Input, bool, error) {
		var zero Input
		if index >= len(snapshot) {
			return zero, false, nil
		}
		value := snapshot[index]
		index++

		return value, true, nil
	})
}

// RunGenerator executes a lazy caller-owned source with bounded backpressure.
func (pool *Pool[Input, Output]) RunGenerator(
	ctx context.Context,
	generator PoolGenerator[Input],
) ([]PoolResult[Input, Output], error) {
	if nilLike(generator) {
		return nil, fmt.Errorf("%w: generator is nil", ErrInvalidPool)
	}

	return pool.run(ctx, -1, generator)
}

// RunChannel executes inputs until channel closure or cancellation.
func (pool *Pool[Input, Output]) RunChannel(
	ctx context.Context,
	input <-chan Input,
) ([]PoolResult[Input, Output], error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input channel is nil", ErrInvalidPool)
	}

	return pool.run(ctx, -1, func(ctx context.Context) (Input, bool, error) {
		return receivePoolInput(ctx, input)
	})
}

func receivePoolInput[Input any](ctx context.Context, input <-chan Input) (Input, bool, error) {
	var zero Input
	select {
	case <-ctx.Done():
		return zero, false, ctx.Err()
	case value, ok := <-input:
		return value, ok, nil
	}
}

type poolJob[Input any] struct {
	index int
	key   string
	input Input
}

func (pool *Pool[Input, Output]) run(
	ctx context.Context,
	knownRequests int,
	source PoolGenerator[Input],
) ([]PoolResult[Input, Output], error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidPool)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if knownRequests > pool.limits.MaximumRequests {
		return nil, &PoolError{Cause: ErrPoolLimit}
	}
	concurrency, err := pool.selectConcurrency(PoolWorkload{KnownRequests: knownRequests})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan poolJob[Input], pool.pending)
	completed := make(chan PoolResult[Input, Output], concurrency)
	sourceFailure := make(chan error, 1)
	deadlineDone := make(chan struct{})
	var elapsed atomic.Bool
	go func() {
		defer close(deadlineDone)
		if pool.clock.Wait(runCtx, pool.limits.MaximumElapsed) == nil {
			elapsed.Store(true)
			cancel()
		}
	}()

	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					value, executeErr := invokePoolExecutor(pool.execute, runCtx, job.input)
					result := PoolResult[Input, Output]{
						Index: job.index, Key: job.key, Input: job.input,
						Value: value.Value, ResponseBytes: value.ResponseBytes,
						MemoryBytes: value.MemoryBytes, Error: executeErr,
					}
					completed <- result
				}
			}
		}()
	}
	go pool.schedule(runCtx, cancel, source, jobs, sourceFailure)
	go func() {
		workers.Wait()
		close(completed)
	}()

	results := make([]PoolResult[Input, Output], 0, max(knownRequests, 0))
	var responseBytes int64
	var memoryBytes int64
	var runFailure error
	for result := range completed {
		if elapsed.Load() {
			if runFailure == nil {
				runFailure = ErrPoolLimit
				cancel()
			}
			continue
		}
		if result.ResponseBytes < 0 || result.MemoryBytes < 0 {
			if runFailure == nil {
				runFailure = ErrInvalidPool
				cancel()
			}
			continue
		}
		nextResponseBytes := responseBytes + result.ResponseBytes
		nextMemoryBytes := memoryBytes + result.MemoryBytes
		if nextResponseBytes < responseBytes || nextMemoryBytes < memoryBytes ||
			nextResponseBytes > pool.limits.MaximumResponseBytes ||
			nextMemoryBytes > pool.limits.MaximumMemoryBytes {
			if runFailure == nil {
				runFailure = ErrPoolLimit
				cancel()
			}
			continue
		}
		responseBytes = nextResponseBytes
		memoryBytes = nextMemoryBytes
		results = append(results, result)
		if pool.failure == PoolFailFast && result.Error != nil && runFailure == nil {
			runFailure = result.Error
			cancel()
		}
	}
	cancel()
	<-deadlineDone
	scheduleFailure := <-sourceFailure
	if runFailure == nil && scheduleFailure != nil {
		runFailure = scheduleFailure
	}
	if runFailure == nil && elapsed.Load() {
		runFailure = ErrPoolLimit
	}
	orderPoolResults(results, pool.order)
	if runFailure != nil {
		return results, &PoolError{Completed: len(results), Cause: runFailure}
	}
	if err := ctx.Err(); err != nil {
		return results, &PoolError{Completed: len(results), Cause: err}
	}

	return results, nil
}

func orderPoolResults[Input any, Output any](
	results []PoolResult[Input, Output],
	order PoolResultOrder,
) {
	if order == PoolInputOrder {
		sort.SliceStable(results, func(left int, right int) bool {
			return results[left].Index < results[right].Index
		})
	}
}

func (pool *Pool[Input, Output]) selectConcurrency(workload PoolWorkload) (int, error) {
	selected := pool.concurrency
	if pool.selector != nil {
		var panicValue any
		func() {
			defer func() { panicValue = recover() }()
			selected = pool.selector(workload)
		}()
		if panicValue != nil {
			return 0, &PoolPanicError{Stage: "concurrency", Value: panicValue}
		}
	}
	if selected < pool.minimum || selected > pool.maximum {
		return 0, fmt.Errorf("%w: selected concurrency is out of bounds", ErrInvalidPool)
	}

	return selected, nil
}

func (pool *Pool[Input, Output]) schedule(
	ctx context.Context,
	cancel context.CancelFunc,
	source PoolGenerator[Input],
	jobs chan<- poolJob[Input],
	failure chan<- error,
) {
	defer close(jobs)
	index := 0
	for {
		if err := ctx.Err(); err != nil {
			failure <- nil
			return
		}
		if index >= pool.limits.MaximumRequests {
			failure <- ErrPoolLimit
			return
		}
		input, ok, sourceErr := invokePoolGenerator(source, ctx)
		if sourceErr != nil {
			failure <- sourceErr
			cancel()
			return
		}
		if !ok {
			failure <- nil
			return
		}
		key := fmt.Sprintf("%d", index)
		if pool.key != nil {
			var keyErr error
			key, keyErr = invokePoolKey(pool.key, input)
			if keyErr != nil {
				failure <- keyErr
				cancel()
				return
			}
		}
		job := poolJob[Input]{index: index, key: key, input: input}
		select {
		case jobs <- job:
			index++
		case <-ctx.Done():
			failure <- nil
			return
		}
	}
}

func invokePoolExecutor[Input any, Output any](
	executor PoolExecutor[Input, Output],
	ctx context.Context,
	input Input,
) (value PoolValue[Output], failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = PoolValue[Output]{}
			failure = &PoolPanicError{Stage: "executor", Value: recovered}
		}
	}()

	return executor(ctx, input)
}

func invokePoolGenerator[Input any](
	generator PoolGenerator[Input],
	ctx context.Context,
) (input Input, ok bool, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = &PoolPanicError{Stage: "generator", Value: recovered}
		}
	}()

	return generator(ctx)
}

func invokePoolKey[Input any](key PoolKey[Input], input Input) (value string, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = &PoolPanicError{Stage: "key", Value: recovered}
		}
	}()

	value, failure = key(input)
	return value, failure
}
