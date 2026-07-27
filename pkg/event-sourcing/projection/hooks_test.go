package projection_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestRunnerInvokesReplayHooksAtCursorBoundaries(t *testing.T) {
	t.Parallel()

	var calls []string
	config := poisonRunnerConfig(t, 1)
	config.Checkpoints = memory.NewProjectionStore()
	config.BeforeReplay = func(context.Context) error {
		calls = append(calls, "before")

		return nil
	}
	config.AfterReplay = func(context.Context) error {
		calls = append(calls, "after")

		return nil
	}
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		calls = append(calls, "handle")

		return nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	first, err := runner.RunBatch(context.Background())
	if err != nil ||
		first.Scanned() != 1 ||
		len(calls) != 2 ||
		calls[0] != "before" ||
		calls[1] != "handle" {
		t.Fatalf("first RunBatch() = %#v, %v calls=%v", first, err, calls)
	}
	second, err := runner.RunBatch(context.Background())
	if err != nil ||
		second.Scanned() != 0 ||
		len(calls) != 3 ||
		calls[2] != "after" {
		t.Fatalf("second RunBatch() = %#v, %v calls=%v", second, err, calls)
	}
	third, err := runner.RunBatch(context.Background())
	if err != nil ||
		third.Scanned() != 0 ||
		len(calls) != 4 ||
		calls[3] != "after" {
		t.Fatalf("third RunBatch() = %#v, %v calls=%v", third, err, calls)
	}
}

func TestRunnerClosesTerminalIteratorBeforeAfterReplayHook(t *testing.T) {
	t.Parallel()

	var calls []string
	config := poisonRunnerConfig(t, 1)
	config.Reader = hookReader{
		read: func(
			context.Context,
			eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			calls = append(calls, "read")

			return &hookIterator{
				close: func() error {
					calls = append(calls, "close")

					return nil
				},
			}, nil
		},
	}
	config.BeforeReplay = func(context.Context) error {
		calls = append(calls, "before")

		return nil
	}
	config.AfterReplay = func(context.Context) error {
		calls = append(calls, "after")

		return nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if err != nil ||
		result.Scanned() != 0 ||
		len(calls) != 4 ||
		calls[0] != "before" ||
		calls[1] != "read" ||
		calls[2] != "close" ||
		calls[3] != "after" {
		t.Fatalf("RunBatch() = %#v, %v calls=%v", result, err, calls)
	}
}

func TestRunnerDoesNotInvokeBeforeReplayAfterCheckpoint(t *testing.T) {
	t.Parallel()

	checkpoints := projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 1, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			t.Fatal("unexpected checkpoint save")

			return nil
		},
	}
	config := poisonRunnerConfig(t, 1)
	config.Checkpoints = checkpoints
	config.BeforeReplay = func(context.Context) error {
		t.Fatal("before hook ran after checkpoint")

		return nil
	}
	afterCalled := false
	config.AfterReplay = func(context.Context) error {
		afterCalled = true

		return nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if err != nil || result.Checkpoint() != 1 || !afterCalled {
		t.Fatalf(
			"RunBatch() = %#v, %v after=%t",
			result,
			err,
			afterCalled,
		)
	}
}

func TestRunnerInvokesAfterReplayAtMaximumCheckpoint(t *testing.T) {
	t.Parallel()

	maximum := eventsourcing.GlobalPosition(^uint64(0))
	config := poisonRunnerConfig(t, 1)
	config.Checkpoints = projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return maximum, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			t.Fatal("unexpected checkpoint save")

			return nil
		},
	}
	config.Reader = hookReader{
		read: func(
			_ context.Context,
			options eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			if options.FromPosition() != maximum ||
				options.ToPosition() != maximum ||
				options.Limit() != 1 {
				t.Fatalf("checkpoint verification options = %#v", options)
			}
			next := true

			return &hookIterator{
				next: func(context.Context) bool {
					available := next
					next = false

					return available
				},
				message: projectionMessageAtPosition(t, maximum),
			}, nil
		},
	}
	afterCalled := false
	config.AfterReplay = func(context.Context) error {
		afterCalled = true

		return nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if err != nil ||
		result.Checkpoint() != maximum ||
		!afterCalled {
		t.Fatalf(
			"RunBatch() = %#v, %v after=%t",
			result,
			err,
			afterCalled,
		)
	}
}

func TestRunnerContainsReplayHookFailuresAndPanics(t *testing.T) {
	t.Parallel()

	hookFailure := errors.New("secret hook failure")
	tests := map[string]struct {
		phase projection.ReplayHookPhase
		hook  projection.ReplayHook
		want  error
	}{
		"before failure": {
			phase: projection.ReplayHookBefore,
			hook: func(context.Context) error {
				return hookFailure
			},
			want: hookFailure,
		},
		"before panic": {
			phase: projection.ReplayHookBefore,
			hook: func(context.Context) error {
				panic("secret before state")
			},
			want: projection.ErrReplayHookPanic,
		},
		"after failure": {
			phase: projection.ReplayHookAfter,
			hook: func(context.Context) error {
				return hookFailure
			},
			want: hookFailure,
		},
		"after panic": {
			phase: projection.ReplayHookAfter,
			hook: func(context.Context) error {
				panic("secret after state")
			},
			want: projection.ErrReplayHookPanic,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			readCalled := false
			config := poisonRunnerConfig(t, 1)
			config.Reader = hookReader{
				read: func(
					context.Context,
					eventsourcing.ReadGlobalOptions,
				) (eventsourcing.MessageIterator, error) {
					readCalled = true

					return &hookIterator{}, nil
				},
			}
			if testCase.phase == projection.ReplayHookBefore {
				config.BeforeReplay = testCase.hook
			} else {
				config.AfterReplay = testCase.hook
			}
			runner, runnerErr := projection.NewRunner(config)
			if runnerErr != nil {
				t.Fatal(runnerErr)
			}

			result, runErr := runner.RunBatch(context.Background())
			var hookErr *projection.ReplayHookError
			if !errors.Is(runErr, testCase.want) ||
				!errors.As(runErr, &hookErr) ||
				hookErr.Phase != testCase.phase ||
				!errors.Is(hookErr.Cause, testCase.want) ||
				runErr.Error() != "projection replay hook failed" ||
				strings.Contains(runErr.Error(), "secret") ||
				result.Scanned() != 0 ||
				(testCase.phase == projection.ReplayHookBefore &&
					readCalled) ||
				(testCase.phase == projection.ReplayHookAfter &&
					!readCalled) {
				t.Fatalf(
					"RunBatch() = %#v, %v hook=%#v read=%t",
					result,
					runErr,
					hookErr,
					readCalled,
				)
			}
		})
	}
}

func TestReplayHookPhaseDiagnosticsAndNilHooks(t *testing.T) {
	t.Parallel()

	config := poisonRunnerConfig(t, 1)
	config.Reader = hookReader{
		read: func(
			context.Context,
			eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			return &hookIterator{}, nil
		},
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.RunBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if projection.ReplayHookBefore.String() != "before" ||
		projection.ReplayHookAfter.String() != "after" ||
		projection.ReplayHookPhase(99).String() != "unknown" {
		t.Fatal("replay hook phase diagnostics are unstable")
	}
}

func TestRunnerDoesNotInvokeReplayHooksAfterCancellation(t *testing.T) {
	t.Parallel()

	t.Run("after status", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		config := poisonRunnerConfig(t, 1)
		config.Checkpoints = projectionCheckpointStore{
			load: func(
				context.Context,
				string,
			) (eventsourcing.GlobalPosition, error) {
				cancel()

				return 0, projection.ErrCheckpointNotFound
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint saved after cancellation")

				return nil
			},
		}
		config.BeforeReplay = func(context.Context) error {
			t.Fatal("before hook ran after cancellation")

			return nil
		}
		runner, err := projection.NewRunner(config)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = runner.RunBatch(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBatch() = %v", err)
		}
	})

	t.Run("during before", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		config := poisonRunnerConfig(t, 1)
		config.BeforeReplay = func(context.Context) error {
			cancel()

			return nil
		}
		config.Reader = hookReader{
			read: func(
				context.Context,
				eventsourcing.ReadGlobalOptions,
			) (eventsourcing.MessageIterator, error) {
				t.Fatal("global read started after before hook cancellation")

				return nil, nil
			},
		}
		runner, err := projection.NewRunner(config)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = runner.RunBatch(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBatch() = %v", err)
		}
	})

	t.Run("before after", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		config := poisonRunnerConfig(t, 1)
		config.Reader = hookReader{
			read: func(
				context.Context,
				eventsourcing.ReadGlobalOptions,
			) (eventsourcing.MessageIterator, error) {
				return &hookIterator{
					next: func(context.Context) bool {
						cancel()

						return false
					},
				}, nil
			},
		}
		config.AfterReplay = func(context.Context) error {
			t.Fatal("after hook ran after cancellation")

			return nil
		}
		runner, err := projection.NewRunner(config)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = runner.RunBatch(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBatch() = %v", err)
		}
	})

	t.Run("terminal without hook", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		config := poisonRunnerConfig(t, 1)
		config.Reader = hookReader{
			read: func(
				context.Context,
				eventsourcing.ReadGlobalOptions,
			) (eventsourcing.MessageIterator, error) {
				return &hookIterator{
					next: func(context.Context) bool {
						cancel()

						return false
					},
				}, nil
			},
		}
		runner, err := projection.NewRunner(config)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = runner.RunBatch(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBatch() = %v", err)
		}
	})
}

func TestRunnerDoesNotInvokeAfterReplayWhenTerminalIteratorFails(t *testing.T) {
	t.Parallel()

	iteratorFailure := errors.New("iterator failure")
	closeFailure := errors.New("close failure")
	tests := map[string]*hookIterator{
		"iterator": {err: iteratorFailure},
		"close": {
			close: func() error {
				return closeFailure
			},
		},
	}
	for name, iterator := range tests {
		iterator := iterator
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := poisonRunnerConfig(t, 1)
			config.Reader = hookReader{
				read: func(
					context.Context,
					eventsourcing.ReadGlobalOptions,
				) (eventsourcing.MessageIterator, error) {
					return iterator, nil
				},
			}
			config.AfterReplay = func(context.Context) error {
				t.Fatal("after hook ran after terminal iterator failure")

				return nil
			}
			runner, err := projection.NewRunner(config)
			if err != nil {
				t.Fatal(err)
			}

			_, err = runner.RunBatch(context.Background())
			if !errors.Is(err, iterator.err) &&
				!errors.Is(err, closeFailure) {
				t.Fatalf("RunBatch() = %v", err)
			}
		})
	}
}

type hookReader struct {
	read func(
		context.Context,
		eventsourcing.ReadGlobalOptions,
	) (eventsourcing.MessageIterator, error)
}

func (reader hookReader) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	return reader.read(ctx, options)
}

type hookIterator struct {
	next    func(context.Context) bool
	message eventsourcing.Message
	err     error
	close   func() error
}

func (iterator *hookIterator) Next(ctx context.Context) bool {
	if iterator.next != nil {
		return iterator.next(ctx)
	}

	return false
}

func (iterator *hookIterator) Message() eventsourcing.Message {
	return iterator.message
}

func (iterator *hookIterator) Err() error {
	return iterator.err
}

func (iterator *hookIterator) Close() error {
	if iterator.close != nil {
		return iterator.close()
	}

	return nil
}
