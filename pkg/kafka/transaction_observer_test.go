package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducerObserversReportSuccessfulTransactionLifecycle(t *testing.T) {

	var observations []Observation
	producer := transactionalProducer(&recordingProducerBackend{})
	producer.clientID = "transaction-producer"
	producer.observers = transactionObserverDispatcher(t, &observations)

	err := producer.RunTransaction(
		context.Background(),
		func(Transaction) error { return nil },
	)

	if err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("transaction observations = %#v", observations)
	}
	for index, want := range []ObservationKind{
		ObservationTransactionBegin,
		ObservationTransactionCommit,
	} {
		got := observations[index]
		if got.Kind != want ||
			got.ClientID != "transaction-producer" ||
			got.GroupID != "" ||
			!got.Succeeded ||
			got.Category != ErrorUnknown ||
			got.StartedAt.IsZero() ||
			got.Duration < 0 {
			t.Fatalf("transaction observation %d = %#v", index, got)
		}
	}
}

func TestProducerObserversClassifyTransactionFailures(t *testing.T) {

	callbackErr := errors.New("application failed")
	for name, test := range map[string]struct {
		backend  *recordingProducerBackend
		callback func(Transaction) error
		want     []Observation
	}{
		"begin authorization": {
			backend: &recordingProducerBackend{
				beginErr: kerr.TransactionalIDAuthorizationFailed,
			},
			callback: func(Transaction) error { return nil },
			want: []Observation{{
				Kind:     ObservationTransactionBegin,
				Category: ErrorAuthorization,
			}},
		},
		"callback abort": {
			backend:  &recordingProducerBackend{},
			callback: func(Transaction) error { return callbackErr },
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{Kind: ObservationTransactionAbort, Succeeded: true},
			},
		},
		"abortable commit": {
			backend: &recordingProducerBackend{
				endErrors: []error{kerr.TransactionAbortable, nil},
			},
			callback: func(Transaction) error { return nil },
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionCommit,
					Category: ErrorRetryable,
				},
				{Kind: ObservationTransactionAbort, Succeeded: true},
			},
		},
		"unknown commit": {
			backend: &recordingProducerBackend{
				endErr: errors.New("commit response lost"),
			},
			callback: func(Transaction) error { return nil },
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionCommit,
					Category: ErrorAmbiguous,
				},
			},
		},
		"unknown abort": {
			backend: &recordingProducerBackend{
				endErr: errors.New("abort response lost"),
			},
			callback: func(Transaction) error { return callbackErr },
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionAbort,
					Category: ErrorAmbiguous,
				},
			},
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {

			var observations []Observation
			producer := transactionalProducer(test.backend)
			producer.observers = transactionObserverDispatcher(t, &observations)

			_ = producer.RunTransaction(context.Background(), test.callback)

			assertTransactionObservations(t, observations, test.want)
		})
	}
}

func TestTransactionObservationCategoryClassifiesPlainError(t *testing.T) {

	if category := transactionObservationCategory(context.DeadlineExceeded); category !=
		ErrorTimeout {
		t.Fatalf("transaction observation category = %v", category)
	}
}

func TestTransactionProcessorConfigValidatesObserversBeforeConstruction(
	t *testing.T,
) {

	config := validTransactionProcessorConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(context.Context, Observation) error { return nil },
		},
	}
	called := false
	processor, err := newTransactionProcessor(
		config,
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			called = true

			return &recordingTransactionProcessorBackend{}, nil
		},
	)

	if processor != nil ||
		!errors.Is(err, ErrInvalidTransactionProcessorConfig) ||
		!errors.Is(err, ErrObserverFailureHandlerRequired) ||
		called {
		t.Fatalf(
			"newTransactionProcessor() = (%#v, %v), factory called = %t",
			processor,
			err,
			called,
		)
	}
}

func TestTransactionProcessorWiresBrokerObservers(t *testing.T) {

	config := validTransactionProcessorConfig()
	config.Observers = transactionObserverPolicy(&[]Observation{})
	processor, err := newTransactionProcessor(
		config,
		func(options ...kgo.Opt) (transactionProcessorBackend, error) {
			client, clientErr := kgo.NewClient(options...)
			if clientErr != nil {
				t.Fatalf("apply transaction processor options: %v", clientErr)
			}
			defer client.Close()

			hooks := reflect.ValueOf(client.OptValue(kgo.WithHooks))
			if hooks.Kind() != reflect.Slice || hooks.Len() != 1 {
				t.Fatalf("transaction processor hooks = %#v", hooks)
			}
			hook, ok := hooks.Index(0).Interface().(*franzObserverHook)
			if !ok ||
				hook.clientID != "transaction-worker" ||
				hook.groupID != "transaction-worker" ||
				hook.before == nil ||
				hook.after == nil {
				t.Fatalf(
					"transaction processor observer hook = %#v",
					hooks.Index(0).Interface(),
				)
			}

			return &recordingTransactionProcessorBackend{}, nil
		},
	)

	if err != nil || processor == nil {
		t.Fatalf("newTransactionProcessor() = (%#v, %v)", processor, err)
	}
}

func TestTransactionProcessorObserversReportTransactionLifecycle(t *testing.T) {

	for name, test := range map[string]struct {
		backend    *recordingTransactionProcessorBackend
		handlerErr error
		want       []Observation
	}{
		"commit": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
				endResults: []transactionEndResult{{committed: true}},
			},
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{Kind: ObservationTransactionCommit, Succeeded: true},
			},
		},
		"handler abort": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
			},
			handlerErr: errors.New("processing failed"),
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{Kind: ObservationTransactionAbort, Succeeded: true},
			},
		},
		"begin authorization": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
				beginErr: kerr.TransactionalIDAuthorizationFailed,
			},
			want: []Observation{{
				Kind:     ObservationTransactionBegin,
				Category: ErrorAuthorization,
			}},
		},
		"unknown abort": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
				endResults: []transactionEndResult{{
					err: errors.New("abort response lost"),
				}},
			},
			handlerErr: errors.New("processing failed"),
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionAbort,
					Category: ErrorAmbiguous,
				},
			},
		},
		"not committed": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
			},
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionCommit,
					Category: ErrorRetryable,
				},
			},
		},
		"unknown commit": {
			backend: &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{
					transactionFetches(transactionSourceRecord(0, "source")),
				},
				endResults: []transactionEndResult{{
					err: errors.New("commit response lost"),
				}},
			},
			want: []Observation{
				{Kind: ObservationTransactionBegin, Succeeded: true},
				{
					Kind:     ObservationTransactionCommit,
					Category: ErrorAmbiguous,
				},
			},
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {

			var observations []Observation
			config := validTransactionProcessorConfig()
			config.Observers = transactionObserverPolicy(&observations)
			processor, err := newTransactionProcessor(
				config,
				func(...kgo.Opt) (transactionProcessorBackend, error) {
					return test.backend, nil
				},
			)
			if err != nil {
				t.Fatalf("newTransactionProcessor() error = %v", err)
			}

			_, _ = processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					context.Context,
					ConsumedRecord,
					Transaction,
				) error {
					return test.handlerErr
				}),
			)

			assertTransactionObservations(t, observations, test.want)
			for index, observation := range observations {
				if observation.ClientID != "transaction-worker" ||
					observation.GroupID != "transaction-worker" {
					t.Fatalf("transaction observation %d = %#v", index, observation)
				}
			}
		})
	}
}

func TestTransactionProcessorObserverCannotReenterLifecycle(t *testing.T) {

	var processor *TransactionProcessor
	var reentryErrors []error
	config := validTransactionProcessorConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, observation Observation) error {
				if observation.Kind != ObservationTransactionBegin {
					return nil
				}
				_, runOnceErr := processor.RunOnce(
					ctx,
					TransactionHandlerFunc(func(
						context.Context,
						ConsumedRecord,
						Transaction,
					) error {
						return nil
					}),
				)
				reentryErrors = append(
					reentryErrors,
					runOnceErr,
					processor.Run(
						ctx,
						TransactionHandlerFunc(func(
							context.Context,
							ConsumedRecord,
							Transaction,
						) error {
							return nil
						}),
					),
					processor.Shutdown(ctx),
					processor.Close(),
				)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{
			transactionFetches(transactionSourceRecord(0, "source")),
		},
		endResults: []transactionEndResult{{committed: true}},
	}
	var err error
	processor, err = newTransactionProcessor(
		config,
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			return backend, nil
		},
	)
	if err != nil {
		t.Fatalf("newTransactionProcessor() error = %v", err)
	}

	_, err = processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	)

	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(reentryErrors) != 4 {
		t.Fatalf("observer reentry errors = %#v", reentryErrors)
	}
	for index, reentryErr := range reentryErrors {
		if !errors.Is(reentryErr, ErrObserverReentry) {
			t.Fatalf("observer reentry error %d = %v", index, reentryErr)
		}
	}
	if backend.closed {
		t.Fatal("observer closed transaction processor backend")
	}
}

func assertTransactionObservations(
	t *testing.T,
	got []Observation,
	want []Observation,
) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("transaction observations = %#v, want %#v", got, want)
	}
	for index, expected := range want {
		observation := got[index]
		if observation.Kind != expected.Kind ||
			observation.Succeeded != expected.Succeeded ||
			observation.Category != expected.Category ||
			observation.StartedAt.IsZero() ||
			observation.Duration < 0 {
			t.Fatalf(
				"transaction observation %d = %#v, want %#v",
				index,
				observation,
				expected,
			)
		}
	}
}

func transactionObserverDispatcher(
	t *testing.T,
	observations *[]Observation,
) observerDispatcher {
	t.Helper()

	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				*observations = append(*observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}

	return newObserverDispatcher(policy)
}

func transactionObserverPolicy(observations *[]Observation) ObserverPolicy {
	return ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				*observations = append(*observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
}
