package eventtest_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
)

type snapshotState struct {
	owner   string
	version uint64
}

func TestSnapshotEquivalenceAcceptsEquivalentState(t *testing.T) {
	t.Parallel()

	err := eventtest.CheckSnapshotEquivalence(
		context.Background(),
		eventtest.SnapshotEquivalenceConfig[snapshotState]{
			FullHistory: func(context.Context) (snapshotState, error) {
				return snapshotState{owner: "Ada", version: 4}, nil
			},
			Snapshot: func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{owner: "Ada", version: 4}, 3, nil
			},
			Version: func(state snapshotState) uint64 {
				return state.version
			},
			Equal: func(left, right snapshotState) bool {
				return left == right
			},
		},
	)
	if err != nil {
		t.Fatalf("CheckSnapshotEquivalence() error = %v", err)
	}
}

func TestSnapshotEquivalenceRejectsInvalidAndDifferentResults(t *testing.T) {
	t.Parallel()

	loaderFailure := errors.New("snapshot load failed")
	valid := eventtest.SnapshotEquivalenceConfig[snapshotState]{
		FullHistory: func(context.Context) (snapshotState, error) {
			return snapshotState{owner: "Ada", version: 4}, nil
		},
		Snapshot: func(context.Context) (snapshotState, uint64, error) {
			return snapshotState{owner: "Ada", version: 4}, 3, nil
		},
		Version: func(state snapshotState) uint64 { return state.version },
		Equal:   func(left, right snapshotState) bool { return left == right },
	}
	testCases := map[string]struct {
		ctx    context.Context
		config eventtest.SnapshotEquivalenceConfig[snapshotState]
		want   error
	}{
		"nil context": {config: valid, want: eventsourcing.ErrInvalidArgument},
		"empty config": {
			ctx:  context.Background(),
			want: eventsourcing.ErrInvalidArgument,
		},
		"full load failure": {
			ctx: context.Background(),
			config: replaceFullLoader(valid, func(context.Context) (
				snapshotState,
				error,
			) {
				return snapshotState{}, loaderFailure
			}),
			want: loaderFailure,
		},
		"snapshot load failure": {
			ctx: context.Background(),
			config: replaceSnapshotLoader(valid, func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{}, 0, loaderFailure
			}),
			want: loaderFailure,
		},
		"snapshot not used": {
			ctx: context.Background(),
			config: replaceSnapshotLoader(valid, func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{owner: "Ada", version: 4}, 0, nil
			}),
			want: eventtest.ErrConformance,
		},
		"snapshot ahead": {
			ctx: context.Background(),
			config: replaceSnapshotLoader(valid, func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{owner: "Ada", version: 4}, 5, nil
			}),
			want: eventtest.ErrConformance,
		},
		"version differs": {
			ctx: context.Background(),
			config: replaceSnapshotLoader(valid, func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{owner: "Ada", version: 5}, 3, nil
			}),
			want: eventtest.ErrConformance,
		},
		"state differs": {
			ctx: context.Background(),
			config: replaceSnapshotLoader(valid, func(context.Context) (
				snapshotState,
				uint64,
				error,
			) {
				return snapshotState{owner: "Grace", version: 4}, 3, nil
			}),
			want: eventtest.ErrConformance,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckSnapshotEquivalence(
				testCase.ctx,
				testCase.config,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func replaceFullLoader(
	config eventtest.SnapshotEquivalenceConfig[snapshotState],
	loader func(context.Context) (snapshotState, error),
) eventtest.SnapshotEquivalenceConfig[snapshotState] {
	config.FullHistory = loader

	return config
}

func replaceSnapshotLoader(
	config eventtest.SnapshotEquivalenceConfig[snapshotState],
	loader func(context.Context) (snapshotState, uint64, error),
) eventtest.SnapshotEquivalenceConfig[snapshotState] {
	config.Snapshot = loader

	return config
}
