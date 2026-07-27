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

func TestRunnerGuardsEveryReplayBatchBeforeApplicationCallbacks(
	t *testing.T,
) {
	t.Parallel()

	var calls []string
	var attempts []projection.ReplayAttempt
	config := poisonRunnerConfig(t, 1)
	reader := config.Reader
	config.Reader = hookReader{
		read: func(
			ctx context.Context,
			options eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			calls = append(calls, "read")

			return reader.ReadGlobal(ctx, options)
		},
	}
	config.Checkpoints = memory.NewProjectionStore()
	config.Guard = func(
		_ context.Context,
		attempt projection.ReplayAttempt,
	) error {
		calls = append(calls, "guard")
		attempts = append(attempts, attempt)

		return nil
	}
	config.BeforeReplay = func(context.Context) error {
		calls = append(calls, "before")

		return nil
	}
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		calls = append(calls, "handle")

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

	first, err := runner.RunBatch(context.Background())
	if err != nil || first.Handled() != 1 {
		t.Fatalf("first RunBatch() = %#v, %v", first, err)
	}
	second, err := runner.RunBatch(context.Background())
	if err != nil || second.Scanned() != 0 {
		t.Fatalf("second RunBatch() = %#v, %v", second, err)
	}
	if strings.Join(calls, ",") !=
		"guard,before,read,handle,guard,read,read,after" {
		t.Fatalf("callback order = %v", calls)
	}
	if len(attempts) != 2 ||
		!attempts[0].Valid() ||
		attempts[0].ProjectionName() != "account-summary" ||
		attempts[0].BatchSize() != 1 {
		t.Fatalf("first attempt = %#v", attempts)
	}
	if checkpoint, ok := attempts[0].Checkpoint(); ok || checkpoint != 0 {
		t.Fatalf("first checkpoint = %d, %t", checkpoint, ok)
	}
	if checkpoint, ok := attempts[1].Checkpoint(); !ok || checkpoint != 1 {
		t.Fatalf("second checkpoint = %d, %t", checkpoint, ok)
	}
}

func TestRunnerStopsBeforeReplayWhenGuardRejectsOrPanics(t *testing.T) {
	t.Parallel()

	secret := errors.New("secret authorization state")
	tests := map[string]struct {
		guard projection.ReplayGuard
		want  error
	}{
		"error": {
			guard: func(context.Context, projection.ReplayAttempt) error {
				return secret
			},
			want: secret,
		},
		"panic": {
			guard: func(context.Context, projection.ReplayAttempt) error {
				panic("secret panic value")
			},
			want: projection.ErrReplayGuardPanic,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := poisonRunnerConfig(t, 1)
			config.Reader = hookReader{
				read: func(
					context.Context,
					eventsourcing.ReadGlobalOptions,
				) (eventsourcing.MessageIterator, error) {
					t.Fatal("history read after guard failure")

					return nil, nil
				},
			}
			config.Checkpoints = memory.NewProjectionStore()
			config.Guard = testCase.guard
			config.BeforeReplay = func(context.Context) error {
				t.Fatal("before hook ran after guard failure")

				return nil
			}
			config.Handler = func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				t.Fatal("handler ran after guard failure")

				return nil
			}
			runner, err := projection.NewRunner(config)
			if err != nil {
				t.Fatal(err)
			}

			result, err := runner.RunBatch(context.Background())
			var guardErr *projection.ReplayGuardError
			if !errors.Is(err, testCase.want) ||
				!errors.As(err, &guardErr) ||
				result != (projection.BatchResult{}) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("RunBatch() = %#v, %v", result, err)
			}
		})
	}
}

func TestRunnerObservesCancellationCausedByReplayGuard(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	config := poisonRunnerConfig(t, 1)
	config.Reader = hookReader{
		read: func(
			context.Context,
			eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			t.Fatal("history read after guard cancellation")

			return nil, nil
		},
	}
	config.Checkpoints = memory.NewProjectionStore()
	config.Guard = func(context.Context, projection.ReplayAttempt) error {
		cancel()

		return nil
	}
	config.BeforeReplay = func(context.Context) error {
		t.Fatal("before hook ran after guard cancellation")

		return nil
	}
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		t.Fatal("handler ran after guard cancellation")

		return nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(ctx)
	if !errors.Is(err, context.Canceled) ||
		result != (projection.BatchResult{}) {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func TestReplayAttemptZeroValueAndExplicitPermitAreSafe(t *testing.T) {
	t.Parallel()

	attempt := projection.ReplayAttempt{}
	if attempt.Valid() ||
		attempt.ProjectionName() != "" ||
		attempt.BatchSize() != 0 {
		t.Fatalf("zero ReplayAttempt = %#v", attempt)
	}
	if checkpoint, ok := attempt.Checkpoint(); ok || checkpoint != 0 {
		t.Fatalf("zero checkpoint = %d, %t", checkpoint, ok)
	}
	if err := projection.PermitReplay(
		context.Background(),
		attempt,
	); err != nil {
		t.Fatalf("PermitReplay() = %v", err)
	}
}
