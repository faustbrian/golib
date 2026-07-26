package eventtest_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestEventStoreConformanceAcceptsMemoryStore(t *testing.T) {
	t.Parallel()

	err := eventtest.CheckEventStore(
		context.Background(),
		func() (eventsourcing.EventStore, error) {
			return memory.NewStore(), nil
		},
	)
	if err != nil {
		t.Fatalf("CheckEventStore() error = %v", err)
	}
}

func TestEventStoreConformanceRejectsInvalidFactories(t *testing.T) {
	t.Parallel()

	factoryFailure := errors.New("store factory failed")
	testCases := map[string]struct {
		ctx     context.Context
		factory eventtest.EventStoreFactory
		want    error
	}{
		"nil context": {
			factory: func() (eventsourcing.EventStore, error) {
				return memory.NewStore(), nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"nil factory": {
			ctx:  context.Background(),
			want: eventsourcing.ErrInvalidArgument,
		},
		"factory failure": {
			ctx: context.Background(),
			factory: func() (eventsourcing.EventStore, error) {
				return nil, factoryFailure
			},
			want: factoryFailure,
		},
		"nil store": {
			ctx: context.Background(),
			factory: func() (eventsourcing.EventStore, error) {
				return nil, nil
			},
			want: eventtest.ErrConformance,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckEventStore(
				testCase.ctx,
				testCase.factory,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
