package eventtest_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
)

func TestSynchronousDispatcherConformanceAcceptsCoreDispatcher(t *testing.T) {
	t.Parallel()

	err := eventtest.CheckSynchronousDispatcher(
		context.Background(),
		func(
			registrations []eventtest.DispatcherRegistration,
		) (eventsourcing.Dispatcher, error) {
			options := make(
				[]eventsourcing.SyncDispatcherOption,
				0,
				len(registrations),
			)
			for _, registration := range registrations {
				consumerOptions := make(
					[]eventsourcing.ConsumerOption,
					0,
					len(registration.Filters),
				)
				for _, filter := range registration.Filters {
					consumerOptions = append(
						consumerOptions,
						eventsourcing.FilterDelivery(filter),
					)
				}
				consumer, err := eventsourcing.NewConsumer(
					registration.ID,
					registration.Handler,
					consumerOptions...,
				)
				if err != nil {
					return nil, err
				}
				options = append(options, consumer)
			}

			dispatcher, err := eventsourcing.NewSyncDispatcher(options...)
			if err != nil {
				return nil, err
			}

			return dispatcher, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckSynchronousDispatcher() error = %v", err)
	}
}

func TestSynchronousDispatcherConformanceRejectsInvalidFactories(
	t *testing.T,
) {
	t.Parallel()

	factoryFailure := errors.New("factory failed")
	testCases := map[string]struct {
		ctx     context.Context
		factory eventtest.DispatcherFactory
		want    error
	}{
		"nil context": {
			factory: func(
				[]eventtest.DispatcherRegistration,
			) (eventsourcing.Dispatcher, error) {
				return nil, nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"nil factory": {
			ctx:  context.Background(),
			want: eventsourcing.ErrInvalidArgument,
		},
		"factory failure": {
			ctx: context.Background(),
			factory: func(
				[]eventtest.DispatcherRegistration,
			) (eventsourcing.Dispatcher, error) {
				return nil, factoryFailure
			},
			want: factoryFailure,
		},
		"nil dispatcher": {
			ctx: context.Background(),
			factory: func(
				[]eventtest.DispatcherRegistration,
			) (eventsourcing.Dispatcher, error) {
				return nil, nil
			},
			want: eventtest.ErrConformance,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckSynchronousDispatcher(
				testCase.ctx,
				testCase.factory,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestSynchronousDispatcherConformanceDetectsBehaviorMismatch(
	t *testing.T,
) {
	t.Parallel()

	err := eventtest.CheckSynchronousDispatcher(
		context.Background(),
		func(
			[]eventtest.DispatcherRegistration,
		) (eventsourcing.Dispatcher, error) {
			return dispatcherFunc(func(
				context.Context,
				[]eventsourcing.Delivery,
			) error {
				return nil
			}), nil
		},
	)
	if !errors.Is(err, eventtest.ErrConformance) {
		t.Fatalf("CheckSynchronousDispatcher() error = %v", err)
	}
}

type dispatcherFunc func(context.Context, []eventsourcing.Delivery) error

func (dispatch dispatcherFunc) Dispatch(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
) error {
	return dispatch(ctx, deliveries)
}
