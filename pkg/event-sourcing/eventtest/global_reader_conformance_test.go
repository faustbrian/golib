package eventtest_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestGlobalReaderConformanceAcceptsMemoryStore(t *testing.T) {
	t.Parallel()

	err := eventtest.CheckGlobalReader(
		context.Background(),
		func() (eventtest.GlobalEventStore, error) {
			return memory.NewStore(), nil
		},
	)
	if err != nil {
		t.Fatalf("CheckGlobalReader() error = %v", err)
	}
}

func TestGlobalReaderConformanceRejectsInvalidFactories(t *testing.T) {
	t.Parallel()

	factoryFailure := errors.New("global factory failed")
	testCases := map[string]struct {
		ctx     context.Context
		factory eventtest.GlobalStoreFactory
		want    error
	}{
		"nil context": {
			factory: func() (eventtest.GlobalEventStore, error) {
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
			factory: func() (eventtest.GlobalEventStore, error) {
				return nil, factoryFailure
			},
			want: factoryFailure,
		},
		"nil store": {
			ctx: context.Background(),
			factory: func() (eventtest.GlobalEventStore, error) {
				return nil, nil
			},
			want: eventtest.ErrConformance,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckGlobalReader(
				testCase.ctx,
				testCase.factory,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
