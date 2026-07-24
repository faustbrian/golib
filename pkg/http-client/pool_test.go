package httpclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolRunsBoundedWorkersAndReturnsStableInputOrder(t *testing.T) {
	t.Parallel()

	var active atomic.Int64
	var maximum atomic.Int64
	pool, err := NewPool(PoolOptions[int, string]{
		Concurrency: 3,
		Pending:     2,
		Key:         func(input int) (string, error) { return fmt.Sprintf("item-%d", input), nil },
		Execute: func(ctx context.Context, input int) (PoolValue[string], error) {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			defer active.Add(-1)
			select {
			case <-ctx.Done():
				return PoolValue[string]{}, ctx.Err()
			case <-time.After(time.Millisecond):
			}

			return PoolValue[string]{Value: fmt.Sprintf("value-%d", input), ResponseBytes: 10, MemoryBytes: 5}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}
	results, err := pool.RunSlice(context.Background(), []int{5, 4, 3, 2, 1})
	if err != nil {
		t.Fatalf("run pool: %v", err)
	}
	if maximum.Load() > 3 || len(results) != 5 {
		t.Fatalf("maximum active = %d, results = %d", maximum.Load(), len(results))
	}
	for index, result := range results {
		wantInput := 5 - index
		if result.Index != index || result.Input != wantInput || result.Key != fmt.Sprintf("item-%d", wantInput) ||
			result.Value != fmt.Sprintf("value-%d", wantInput) || result.Error != nil {
			t.Fatalf("result %d = %#v", index, result)
		}
	}
}

func TestPoolCollectAllPreservesPerRequestFailures(t *testing.T) {
	t.Parallel()

	requestFailure := errors.New("request failed")
	pool, err := NewPool(PoolOptions[int, int]{
		Concurrency: 3,
		Order:       PoolCompletionOrder,
		Failure:     PoolCollectAll,
		Execute: func(_ context.Context, input int) (PoolValue[int], error) {
			if input == 2 {
				return PoolValue[int]{}, requestFailure
			}

			return PoolValue[int]{Value: input * 10}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}
	results, err := pool.RunSlice(context.Background(), []int{1, 2, 3})
	if err != nil {
		t.Fatalf("collect-all run: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("completion results = %#v", results)
	}
	byInput := make(map[int]PoolResult[int, int], len(results))
	for _, result := range results {
		byInput[result.Input] = result
	}
	if !errors.Is(byInput[2].Error, requestFailure) || byInput[1].Value != 10 || byInput[3].Value != 30 {
		t.Fatalf("collect-all results = %#v", results)
	}
}

func TestPoolResultOrderingIsDeterministic(t *testing.T) {
	t.Parallel()

	completion := []PoolResult[int, int]{{Index: 2}, {Index: 0}, {Index: 1}}
	orderPoolResults(completion, PoolCompletionOrder)
	if completion[0].Index != 2 || completion[1].Index != 0 || completion[2].Index != 1 {
		t.Fatalf("completion order = %#v", completion)
	}
	orderPoolResults(completion, PoolInputOrder)
	if completion[0].Index != 0 || completion[1].Index != 1 || completion[2].Index != 2 {
		t.Fatalf("input order = %#v", completion)
	}
}

func TestPoolFailFastCancelsPendingWorkAndReturnsPartialResults(t *testing.T) {
	t.Parallel()

	failure := errors.New("stop work")
	pool, err := NewPool(PoolOptions[int, int]{
		Concurrency: 1,
		Pending:     1,
		Failure:     PoolFailFast,
		Execute: func(_ context.Context, input int) (PoolValue[int], error) {
			if input == 2 {
				return PoolValue[int]{}, failure
			}

			return PoolValue[int]{Value: input}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}
	generated := 0
	results, err := pool.RunGenerator(context.Background(), func(context.Context) (int, bool, error) {
		generated++

		return generated, generated <= 100, nil
	})
	var poolError *PoolError
	if !errors.As(err, &poolError) || !errors.Is(err, failure) {
		t.Fatalf("fail-fast error = %#v", err)
	}
	if generated >= 100 || len(results) == 0 || poolError.Completed != len(results) {
		t.Fatalf("generated = %d, results = %d, pool error = %#v", generated, len(results), poolError)
	}
}

func TestPoolSupportsChannelInputAndDynamicConcurrency(t *testing.T) {
	t.Parallel()

	selected := 0
	pool, err := NewPool(PoolOptions[int, int]{
		MinimumConcurrency: 1,
		MaximumConcurrency: 4,
		SelectConcurrency: func(workload PoolWorkload) int {
			if workload.KnownRequests != -1 {
				t.Fatalf("channel workload = %#v", workload)
			}
			selected = 2

			return selected
		},
		Execute: func(_ context.Context, input int) (PoolValue[int], error) {
			return PoolValue[int]{Value: input * 2}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}
	input := make(chan int, 3)
	input <- 1
	input <- 2
	input <- 3
	close(input)
	results, err := pool.RunChannel(context.Background(), input)
	if err != nil || selected != 2 || len(results) != 3 || results[2].Value != 6 {
		t.Fatalf("channel run = %#v, %v, selected %d", results, err, selected)
	}
}

func TestPoolEnforcesRequestByteMemoryAndElapsedBudgets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits PoolLimits
		value  PoolValue[int]
		clock  *paginationTestClock
	}{
		{name: "requests", limits: PoolLimits{MaximumRequests: 1}},
		{name: "response bytes", limits: PoolLimits{MaximumResponseBytes: 1}, value: PoolValue[int]{ResponseBytes: 2}},
		{name: "memory", limits: PoolLimits{MaximumMemoryBytes: 1}, value: PoolValue[int]{MemoryBytes: 2}},
		{name: "elapsed", limits: PoolLimits{MaximumElapsed: time.Second}, clock: &paginationTestClock{now: time.Unix(1_700_000_000, 0), advanceOnNow: 2 * time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := PoolOptions[int, int]{
				Concurrency: 1, Limits: test.limits,
				Execute: func(context.Context, int) (PoolValue[int], error) { return test.value, nil },
			}
			if test.clock != nil {
				options.Clock = test.clock
			}
			pool, err := NewPool(options)
			if err != nil {
				t.Fatalf("construct pool: %v", err)
			}
			results, err := pool.RunSlice(context.Background(), []int{1, 2})
			if !errors.Is(err, ErrPoolLimit) {
				t.Fatalf("budget error = %v, results = %#v", err, results)
			}
		})
	}
}

func TestPoolRejectsInvalidConfigurationAndSources(t *testing.T) {
	t.Parallel()

	var nilExecute PoolExecutor[int, int]
	var nilClock *paginationTestClock
	validExecute := func(context.Context, int) (PoolValue[int], error) { return PoolValue[int]{}, nil }
	for _, options := range []PoolOptions[int, int]{
		{Execute: nilExecute},
		{Execute: validExecute, Concurrency: -1},
		{Execute: validExecute, MinimumConcurrency: 3, MaximumConcurrency: 2},
		{Execute: validExecute, MaximumConcurrency: maximumPoolConcurrency + 1},
		{Execute: validExecute, Pending: -1},
		{Execute: validExecute, Pending: maximumPoolPending + 1},
		{Execute: validExecute, Clock: nilClock},
		{Execute: validExecute, Limits: PoolLimits{MaximumRequests: -1}},
		{Execute: validExecute, Order: PoolResultOrder(99)},
		{Execute: validExecute, Failure: PoolFailureMode(99)},
	} {
		if _, err := NewPool(options); !errors.Is(err, ErrInvalidPool) {
			t.Fatalf("invalid pool options error = %v", err)
		}
	}
	pool, err := NewPool(PoolOptions[int, int]{Execute: validExecute})
	if err != nil {
		t.Fatalf("construct valid pool: %v", err)
	}
	var nilContext context.Context
	if _, err := pool.RunSlice(nilContext, nil); !errors.Is(err, ErrInvalidPool) {
		t.Fatalf("nil context error = %v", err)
	}
	var nilGenerator PoolGenerator[int]
	if _, err := pool.RunGenerator(context.Background(), nilGenerator); !errors.Is(err, ErrInvalidPool) {
		t.Fatalf("nil generator error = %v", err)
	}
	var nilChannel <-chan int
	if _, err := pool.RunChannel(context.Background(), nilChannel); !errors.Is(err, ErrInvalidPool) {
		t.Fatalf("nil channel error = %v", err)
	}
}

func TestPoolErrorsAndPanicsRemainTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	secret := "vendor-secret"
	cause := errors.New(secret)
	poolError := &PoolError{Completed: 2, Cause: cause}
	if poolError.Error() != "HTTP request pool failed" || !errors.Is(poolError, cause) ||
		strings.Contains(poolError.Error(), secret) {
		t.Fatalf("pool error = %q", poolError)
	}
	panicError := &PoolPanicError{Stage: "executor", Value: secret}
	if panicError.Error() != "HTTP request pool executor callback panicked" ||
		strings.Contains(panicError.Error(), secret) {
		t.Fatalf("panic error = %q", panicError)
	}

	t.Run("selector panic", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{
			SelectConcurrency: func(PoolWorkload) int { panic(secret) },
			Execute:           poolIdentityExecutor,
		})
		_, err := pool.RunSlice(context.Background(), []int{1})
		assertPoolPanic(t, err, "concurrency", secret)
	})

	t.Run("generator panic", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor})
		_, err := pool.RunGenerator(context.Background(), func(context.Context) (int, bool, error) {
			panic(secret)
		})
		assertPoolPanic(t, err, "generator", secret)
	})

	t.Run("key panic", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{
			Key:     func(int) (string, error) { panic(secret) },
			Execute: poolIdentityExecutor,
		})
		_, err := pool.RunSlice(context.Background(), []int{1})
		assertPoolPanic(t, err, "key", secret)
	})

	t.Run("executor panic", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{
			Execute: func(context.Context, int) (PoolValue[int], error) { panic(secret) },
		})
		results, err := pool.RunSlice(context.Background(), []int{1})
		if err != nil || len(results) != 1 {
			t.Fatalf("executor panic run = %#v, %v", results, err)
		}
		assertPoolPanic(t, results[0].Error, "executor", secret)
	})
}

func TestPoolRejectsDynamicConcurrencyAndCallbackFailures(t *testing.T) {
	t.Parallel()

	keyFailure := errors.New("key failure")
	sourceFailure := errors.New("source failure")
	tests := []struct {
		name string
		pool *Pool[int, int]
		run  func(*Pool[int, int]) ([]PoolResult[int, int], error)
		want error
	}{
		{
			name: "selector out of range",
			pool: mustPool(t, PoolOptions[int, int]{
				Concurrency:        1,
				MinimumConcurrency: 1,
				MaximumConcurrency: 2,
				SelectConcurrency:  func(PoolWorkload) int { return 3 },
				Execute:            poolIdentityExecutor,
			}),
			run: func(pool *Pool[int, int]) ([]PoolResult[int, int], error) {
				return pool.RunSlice(context.Background(), []int{1})
			},
			want: ErrInvalidPool,
		},
		{
			name: "key failure",
			pool: mustPool(t, PoolOptions[int, int]{
				Key:     func(int) (string, error) { return "", keyFailure },
				Execute: poolIdentityExecutor,
			}),
			run: func(pool *Pool[int, int]) ([]PoolResult[int, int], error) {
				return pool.RunSlice(context.Background(), []int{1})
			},
			want: keyFailure,
		},
		{
			name: "source failure",
			pool: mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor}),
			run: func(pool *Pool[int, int]) ([]PoolResult[int, int], error) {
				return pool.RunGenerator(context.Background(), func(context.Context) (int, bool, error) {
					return 0, false, sourceFailure
				})
			},
			want: sourceFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := test.run(test.pool)
			if len(results) != 0 || !errors.Is(err, test.want) {
				t.Fatalf("callback run = %#v, %v", results, err)
			}
		})
	}
}

func TestPoolCancellationStopsBlockedSourcesAndReturnsCompletedWork(t *testing.T) {
	t.Parallel()

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pool := mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor})
		if _, err := pool.RunSlice(ctx, []int{1}); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled run error = %v", err)
		}
	})

	t.Run("blocked channel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan int)
		pool := mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor})
		done := make(chan error, 1)
		go func() {
			_, err := pool.RunChannel(ctx, input)
			done <- err
		}()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("channel cancellation error = %v", err)
		}
	})

	t.Run("completed result", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		pool := mustPool(t, PoolOptions[int, int]{
			Concurrency: 1,
			Execute: func(context.Context, int) (PoolValue[int], error) {
				cancel()
				return PoolValue[int]{Value: 1}, nil
			},
		})
		results, err := pool.RunSlice(ctx, []int{1})
		if len(results) != 1 || !errors.Is(err, context.Canceled) {
			t.Fatalf("completed cancellation = %#v, %v", results, err)
		}
	})

	t.Run("backpressured scheduler", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		pool := mustPool(t, PoolOptions[int, int]{
			Concurrency: 1,
			Pending:     1,
			Execute: func(ctx context.Context, input int) (PoolValue[int], error) {
				if input == 1 {
					close(started)
					<-ctx.Done()
				}
				return PoolValue[int]{}, ctx.Err()
			},
		})
		done := make(chan error, 1)
		go func() {
			_, err := pool.RunSlice(ctx, []int{1, 2, 3, 4})
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("backpressure cancellation error = %v", err)
		}
	})
}

func TestPoolInternalCancellationAndElapsedCompletionBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("channel receive cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		input := make(chan int)
		_, ok, err := receivePoolInput(ctx, input)
		if ok || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled receive = %t, %v", ok, err)
		}
	})

	t.Run("scheduler cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pool := mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor})
		jobs := make(chan poolJob[int], 1)
		failure := make(chan error, 1)
		pool.schedule(ctx, func() {}, func(context.Context) (int, bool, error) {
			t.Fatal("canceled scheduler invoked its source")
			return 0, false, nil
		}, jobs, failure)
		if err := <-failure; err != nil {
			t.Fatalf("scheduler cancellation = %v", err)
		}
		if _, open := <-jobs; open {
			t.Fatal("scheduler did not close jobs")
		}
	})

	t.Run("result completes after elapsed limit", func(t *testing.T) {
		started := make(chan struct{})
		clock := poolElapsedTestClock{started: started}
		pool := mustPool(t, PoolOptions[int, int]{
			Concurrency: 1,
			Clock:       clock,
			Limits:      PoolLimits{MaximumElapsed: time.Second},
			Execute: func(ctx context.Context, _ int) (PoolValue[int], error) {
				close(started)
				<-ctx.Done()
				return PoolValue[int]{Value: 1}, nil
			},
		})
		results, err := pool.RunSlice(context.Background(), []int{1})
		if len(results) != 0 || !errors.Is(err, ErrPoolLimit) {
			t.Fatalf("elapsed completion = %#v, %v", results, err)
		}
	})

	t.Run("empty source reaches elapsed limit", func(t *testing.T) {
		started := make(chan struct{})
		close(started)
		pool := mustPool(t, PoolOptions[int, int]{
			Clock:   poolElapsedTestClock{started: started},
			Limits:  PoolLimits{MaximumElapsed: time.Second},
			Execute: poolIdentityExecutor,
		})
		results, err := pool.RunSlice(context.Background(), nil)
		if len(results) != 0 || !errors.Is(err, ErrPoolLimit) {
			t.Fatalf("empty elapsed completion = %#v, %v", results, err)
		}
	})
}

type poolElapsedTestClock struct{ started <-chan struct{} }

func (clock poolElapsedTestClock) Now() time.Time { return time.Unix(1_700_000_000, 0) }

func (clock poolElapsedTestClock) Wait(context.Context, time.Duration) error {
	<-clock.started
	return nil
}

func TestPoolEnforcesGeneratorAndAccountingBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("generator request limit", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{
			Concurrency: 1,
			Limits:      PoolLimits{MaximumRequests: 1},
			Execute:     poolIdentityExecutor,
		})
		calls := 0
		results, err := pool.RunGenerator(context.Background(), func(context.Context) (int, bool, error) {
			calls++
			return calls, true, nil
		})
		if calls != 1 || len(results) != 1 || !errors.Is(err, ErrPoolLimit) {
			t.Fatalf("request limit = calls %d, results %#v, error %v", calls, results, err)
		}
	})

	for _, test := range []struct {
		name  string
		value PoolValue[int]
		want  error
	}{
		{name: "negative response", value: PoolValue[int]{ResponseBytes: -1}, want: ErrInvalidPool},
		{name: "negative memory", value: PoolValue[int]{MemoryBytes: -1}, want: ErrInvalidPool},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := mustPool(t, PoolOptions[int, int]{Execute: func(context.Context, int) (PoolValue[int], error) {
				return test.value, nil
			}})
			results, err := pool.RunSlice(context.Background(), []int{1})
			if len(results) != 0 || !errors.Is(err, test.want) {
				t.Fatalf("negative accounting = %#v, %v", results, err)
			}
		})
	}

	t.Run("integer overflow", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{
			Concurrency: 1,
			Limits: PoolLimits{
				MaximumResponseBytes: math.MaxInt64,
				MaximumMemoryBytes:   math.MaxInt64,
			},
			Execute: func(_ context.Context, input int) (PoolValue[int], error) {
				if input == 1 {
					return PoolValue[int]{ResponseBytes: math.MaxInt64, MemoryBytes: math.MaxInt64}, nil
				}
				return PoolValue[int]{ResponseBytes: 1, MemoryBytes: 1}, nil
			},
		})
		results, err := pool.RunSlice(context.Background(), []int{1, 2})
		if len(results) != 1 || !errors.Is(err, ErrPoolLimit) {
			t.Fatalf("overflow accounting = %#v, %v", results, err)
		}
	})

	t.Run("default keys", func(t *testing.T) {
		pool := mustPool(t, PoolOptions[int, int]{Execute: poolIdentityExecutor})
		results, err := pool.RunSlice(context.Background(), []int{4, 5})
		if err != nil || results[0].Key != "0" || results[1].Key != "1" {
			t.Fatalf("default keys = %#v, %v", results, err)
		}
	})
}

func mustPool(t *testing.T, options PoolOptions[int, int]) *Pool[int, int] {
	t.Helper()
	pool, err := NewPool(options)
	if err != nil {
		t.Fatalf("construct pool: %v", err)
	}

	return pool
}

func poolIdentityExecutor(_ context.Context, input int) (PoolValue[int], error) {
	return PoolValue[int]{Value: input}, nil
}

func assertPoolPanic(t *testing.T, err error, stage string, value any) {
	t.Helper()
	var panicError *PoolPanicError
	if !errors.As(err, &panicError) || panicError.Stage != stage || panicError.Value != value ||
		strings.Contains(panicError.Error(), fmt.Sprint(value)) {
		t.Fatalf("panic error = %#v", err)
	}
}
