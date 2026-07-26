package projection_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestRunnerExplicitlySkipsPoisonedDeliveriesAndContinues(t *testing.T) {
	t.Parallel()

	handlerFailure := errors.New("handler failure")
	var decisions []string
	var handled []string
	var saves [][2]eventsourcing.GlobalPosition
	config := poisonRunnerConfig(t, 2)
	config.Handler = func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		handled = append(handled, delivery.Message().ID().String())
		if delivery.Message().ID().String() == "message-1" {
			return handlerFailure
		}

		return nil
	}
	config.PoisonPolicy = func(
		_ context.Context,
		poisoned projection.PoisonedDelivery,
	) (projection.PoisonDecision, error) {
		if poisoned.Delivery().Mode() != eventsourcing.DeliveryReplay ||
			!errors.Is(poisoned.Cause(), handlerFailure) ||
			poisoned.IsZero() {
			t.Fatalf("poisoned delivery = %#v", poisoned)
		}
		decisions = append(
			decisions,
			poisoned.Delivery().Message().ID().String(),
		)

		return projection.SkipPoison, nil
	}
	config.Checkpoints = projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, projection.ErrCheckpointNotFound
		},
		save: func(
			_ context.Context,
			_ string,
			expected eventsourcing.GlobalPosition,
			next eventsourcing.GlobalPosition,
		) error {
			saves = append(
				saves,
				[2]eventsourcing.GlobalPosition{expected, next},
			)

			return nil
		},
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if err != nil ||
		result.Scanned() != 2 ||
		result.Handled() != 1 ||
		result.Skipped() != 1 ||
		result.Checkpointed() != 2 ||
		result.Checkpoint() != 2 ||
		len(decisions) != 1 ||
		decisions[0] != "message-1" ||
		len(handled) != 2 ||
		len(saves) != 2 ||
		saves[0] != [2]eventsourcing.GlobalPosition{0, 1} ||
		saves[1] != [2]eventsourcing.GlobalPosition{1, 2} {
		t.Fatalf(
			"RunBatch() = %#v, %v decisions=%v handled=%v saves=%v",
			result,
			err,
			decisions,
			handled,
			saves,
		)
	}
}

func TestRunnerStopsOnPoisonByDefault(t *testing.T) {
	t.Parallel()

	handlerFailure := errors.New("handler failure")
	config := poisonRunnerConfig(t, 1)
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		return handlerFailure
	}
	config.Checkpoints = projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, projection.ErrCheckpointNotFound
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			t.Fatal("checkpoint advanced for default poison policy")

			return nil
		},
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, handlerFailure) ||
		result.Scanned() != 1 ||
		result.Handled() != 0 ||
		result.Skipped() != 0 ||
		result.Checkpointed() != 0 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func TestRunnerReportsPoisonPolicyAndSkipCheckpointFailures(t *testing.T) {
	t.Parallel()

	handlerFailure := errors.New("secret handler failure")
	policyFailure := errors.New("secret policy failure")
	checkpointFailure := errors.New("checkpoint failure")
	tests := map[string]struct {
		policy projection.PoisonPolicy
		save   func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error
		want       error
		alsoWant   error
		wantString string
	}{
		"stop": {
			policy: func(
				context.Context,
				projection.PoisonedDelivery,
			) (projection.PoisonDecision, error) {
				return projection.StopOnPoison, nil
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint advanced after stop decision")

				return nil
			},
			want:       handlerFailure,
			wantString: "projection handler failed",
		},
		"policy failure": {
			policy: func(
				context.Context,
				projection.PoisonedDelivery,
			) (projection.PoisonDecision, error) {
				return projection.SkipPoison, policyFailure
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint advanced after policy failure")

				return nil
			},
			want:       handlerFailure,
			alsoWant:   policyFailure,
			wantString: "projection poison policy failed",
		},
		"policy panic": {
			policy: func(
				context.Context,
				projection.PoisonedDelivery,
			) (projection.PoisonDecision, error) {
				panic("secret policy state")
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint advanced after policy panic")

				return nil
			},
			want:       handlerFailure,
			alsoWant:   projection.ErrPoisonPolicyPanic,
			wantString: "projection poison policy failed",
		},
		"invalid decision": {
			policy: func(
				context.Context,
				projection.PoisonedDelivery,
			) (projection.PoisonDecision, error) {
				return projection.PoisonDecision(99), nil
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint advanced after invalid decision")

				return nil
			},
			want:       handlerFailure,
			alsoWant:   projection.ErrPoisonDecision,
			wantString: "projection poison policy failed",
		},
		"checkpoint failure": {
			policy: func(
				context.Context,
				projection.PoisonedDelivery,
			) (projection.PoisonDecision, error) {
				return projection.SkipPoison, nil
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				return checkpointFailure
			},
			want:       handlerFailure,
			alsoWant:   checkpointFailure,
			wantString: "projection poison skip checkpoint failed",
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := poisonRunnerConfig(t, 1)
			config.Handler = func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				return handlerFailure
			}
			config.PoisonPolicy = testCase.policy
			config.Checkpoints = projectionCheckpointStore{
				load: func(
					context.Context,
					string,
				) (eventsourcing.GlobalPosition, error) {
					return 0, projection.ErrCheckpointNotFound
				},
				save: testCase.save,
			}
			runner, runnerErr := projection.NewRunner(config)
			if runnerErr != nil {
				t.Fatal(runnerErr)
			}

			result, runErr := runner.RunBatch(context.Background())
			if !errors.Is(runErr, testCase.want) ||
				(testCase.alsoWant != nil &&
					!errors.Is(runErr, testCase.alsoWant)) ||
				runErr.Error() != testCase.wantString ||
				strings.Contains(runErr.Error(), "secret") ||
				result.Scanned() != 1 ||
				result.Handled() != 0 ||
				result.Skipped() != 0 ||
				result.Checkpointed() != 0 {
				t.Fatalf("RunBatch() = %#v, %v", result, runErr)
			}
			if name == "checkpoint failure" {
				var skipErr *projection.PoisonSkipCheckpointError
				if !errors.As(runErr, &skipErr) ||
					!errors.Is(skipErr.Handler, handlerFailure) ||
					!errors.Is(skipErr.Checkpoint, checkpointFailure) {
					t.Fatalf("skip checkpoint error = %#v", skipErr)
				}
			}
		})
	}
}

func TestRunnerDoesNotApplyPoisonPolicyAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	config := poisonRunnerConfig(t, 1)
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		cancel()

		return errors.New("handler stopped")
	}
	config.PoisonPolicy = func(
		context.Context,
		projection.PoisonedDelivery,
	) (projection.PoisonDecision, error) {
		t.Fatal("poison policy ran after cancellation")

		return projection.SkipPoison, nil
	}
	runner, err := projection.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(ctx)
	if !errors.Is(err, context.Canceled) ||
		result.Scanned() != 1 ||
		result.Skipped() != 0 ||
		result.Checkpointed() != 0 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func TestPoisonDecisionAndZeroPoisonedDeliveryAreExplicit(t *testing.T) {
	t.Parallel()

	if projection.StopOnPoison.String() != "stop" ||
		projection.SkipPoison.String() != "skip" ||
		projection.PoisonDecision(99).String() != "unknown" ||
		!(projection.PoisonedDelivery{}).IsZero() ||
		(projection.PoisonedDelivery{}).Cause() != nil ||
		!(projection.PoisonedDelivery{}).Delivery().IsZero() {
		t.Fatal("poison zero values or diagnostics are ambiguous")
	}
}

func poisonRunnerConfig(t *testing.T, count int) projection.RunnerConfig {
	t.Helper()

	return projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      projectionReader(t, count),
		Checkpoints: missingProjectionCheckpointStore(),
		BatchSize:   uint32(count),
		Handler: func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return nil
		},
	}
}
