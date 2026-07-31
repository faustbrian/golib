package eventsourcing_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestAggregateRepositoryValidatesConfiguration(t *testing.T) {
	t.Parallel()

	config := completeRepositoryConfig(t, memory.NewStore())
	callbackCases := map[string]func(*eventsourcing.RepositoryConfig[string, *repositoryAccount]){
		"encode ID": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.EncodeID = nil
		},
		"identify": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Identify = nil
		},
		"factory": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.NewAggregate = nil
		},
		"lifecycle": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Lifecycle = nil
		},
		"apply": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Apply = nil
		},
	}
	for name, mutate := range callbackCases {
		mutate := mutate
		t.Run("callback "+name, func(t *testing.T) {
			t.Parallel()

			input := config
			mutate(&input)
			if _, err := eventsourcing.NewRepository(input); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewRepository() error = %v", err)
			}
		})
	}

	dependencyCases := map[string]func(*eventsourcing.RepositoryConfig[string, *repositoryAccount]){
		"store": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Store = nil
		},
		"codec": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Codec = nil
		},
		"upcasters": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Upcasters = nil
		},
		"clock": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Clock = nil
		},
		"message IDs": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.MessageIDs = nil
		},
		"decorators": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Decorators = nil
		},
		"dispatcher": func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
			config.Dispatcher = nil
		},
	}
	for name, mutate := range dependencyCases {
		mutate := mutate
		t.Run("dependency "+name, func(t *testing.T) {
			t.Parallel()

			input := config
			mutate(&input)
			if _, err := eventsourcing.NewRepository(input); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewRepository() error = %v", err)
			}
		})
	}

	invalidType := config
	invalidType.AggregateType = "Invalid Type"
	if _, err := eventsourcing.NewRepository(invalidType); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewRepository(type) error = %v", err)
	}
	oversizedBatch := config
	oversizedBatch.ReadBatchSize = eventsourcing.MaxReadMessages + 1
	if _, err := eventsourcing.NewRepository(oversizedBatch); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewRepository(batch) error = %v", err)
	}
	defaultBatch := config
	defaultBatch.ReadBatchSize = 0
	if _, err := eventsourcing.NewRepository(defaultBatch); err != nil {
		t.Fatalf("NewRepository(default batch) error = %v", err)
	}
}

func TestAggregateRepositoryValidatesLoadBoundaries(t *testing.T) {
	t.Parallel()

	repository := newAccountRepository(
		t,
		memory.NewStore(),
		repositoryCodec(t),
		mustEmptyUpcasters(t),
		nil,
		nil,
	)
	var nilContext context.Context
	if aggregate, err := repository.Load(nilContext, "account-42"); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) || aggregate != nil {
		t.Fatalf("Load(nil) = %#v, %v", aggregate, err)
	}
	var nilRepository *eventsourcing.AggregateRepository[string, *repositoryAccount]
	if aggregate, err := nilRepository.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) || aggregate != nil {
		t.Fatalf("nil Load() = %#v, %v", aggregate, err)
	}
	if aggregate, err := repository.Load(
		context.Background(),
		"",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) || aggregate != nil {
		t.Fatalf("Load(invalid ID) = %#v, %v", aggregate, err)
	}
	if aggregate, err := repository.Load(
		context.Background(),
		"missing",
	); !errors.Is(err, eventsourcing.ErrStreamNotFound) || aggregate != nil {
		t.Fatalf("Load(missing) = %#v, %v", aggregate, err)
	}

	factoryFailure := errors.New("factory failed")
	config := completeRepositoryConfig(t, memory.NewStore())
	config.NewAggregate = func(string) (*repositoryAccount, error) {
		return nil, factoryFailure
	}
	failingFactory, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate, err := failingFactory.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, factoryFailure) || aggregate != nil {
		t.Fatalf("Load(factory) = %#v, %v", aggregate, err)
	}

	config = completeRepositoryConfig(t, memory.NewStore())
	config.Lifecycle = func(*repositoryAccount) *eventsourcing.Lifecycle {
		return nil
	}
	missingLifecycle, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate, err := missingLifecycle.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) || aggregate != nil {
		t.Fatalf("Load(nil lifecycle) = %#v, %v", aggregate, err)
	}
}

func TestAggregateRepositoryRejectsInvalidStoreReads(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	event := mustEncodedEvent(t, "account.opened", 1, []byte(`{"owner":"Ada"}`))
	pending := mustPendingForRepository(t, "history-1", stream, event)
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongStream, err := eventsourcing.NewStreamID("account", "other")
	if err != nil {
		t.Fatal(err)
	}
	wrongPending := mustPendingForRepository(t, "history-2", wrongStream, event)
	wrongMessage, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       wrongPending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	readFailure := errors.New("read failed")
	iteratorFailure := errors.New("iterator failed")
	closeFailure := errors.New("close failed")
	cases := map[string]struct {
		store eventsourcing.EventStore
		want  error
	}{
		"read error": {
			store: &repositoryStoreFuncs{
				read: func(
					context.Context,
					eventsourcing.StreamID,
					eventsourcing.ReadStreamOptions,
				) (eventsourcing.MessageIterator, error) {
					return nil, readFailure
				},
			},
			want: readFailure,
		},
		"nil iterator": {
			store: &repositoryStoreFuncs{
				read: func(
					context.Context,
					eventsourcing.StreamID,
					eventsourcing.ReadStreamOptions,
				) (eventsourcing.MessageIterator, error) {
					return nil, nil
				},
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"empty missing stream": {
			store: repositoryReadStore(&repositorySliceIterator{}),
			want:  eventsourcing.ErrStreamNotFound,
		},
		"wrong stream": {
			store: repositoryReadStore(&repositorySliceIterator{
				messages: []eventsourcing.Message{wrongMessage},
			}),
			want: eventsourcing.ErrCorruptHistory,
		},
		"too many messages": {
			store: repositoryReadStore(&repositorySliceIterator{
				messages: []eventsourcing.Message{message, message},
			}),
			want: eventsourcing.ErrCorruptHistory,
		},
		"iterator error": {
			store: repositoryReadStore(&repositorySliceIterator{
				messages: []eventsourcing.Message{message},
				err:      iteratorFailure,
			}),
			want: iteratorFailure,
		},
		"close error": {
			store: repositoryReadStore(&repositorySliceIterator{
				messages: []eventsourcing.Message{message},
				closeErr: closeFailure,
			}),
			want: closeFailure,
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := completeRepositoryConfig(t, testCase.store)
			repository, err := eventsourcing.NewRepository(config)
			if err != nil {
				t.Fatal(err)
			}
			aggregate, err := repository.Load(context.Background(), "account-42")
			if !errors.Is(err, testCase.want) || aggregate != nil {
				t.Fatalf("Load() = %#v, %v, want %v", aggregate, err, testCase.want)
			}
		})
	}
}

func TestAggregateRepositoryStopsAfterShortLoadPage(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	unexpectedRead := errors.New("unexpected read after short page")
	store := &repositoryStoreFuncs{
		read: func(
			_ context.Context,
			_ eventsourcing.StreamID,
			options eventsourcing.ReadStreamOptions,
		) (eventsourcing.MessageIterator, error) {
			reads++
			if reads != 1 {
				return nil, unexpectedRead
			}
			if options.FromVersion() != 1 || options.Limit() != 2 {
				t.Fatalf("ReadStream() options = %#v", options)
			}

			return &repositorySliceIterator{messages: []eventsourcing.Message{
				repositoryMessageAt(t, stream, 1),
			}}, nil
		},
	}
	config := completeRepositoryConfig(t, store)
	config.ReadBatchSize = 2
	repository := repositoryFromConfig(t, config)
	aggregate, err := repository.Load(context.Background(), "account-42")
	if err != nil || aggregate == nil || aggregate.owner != "Ada" || reads != 1 {
		t.Fatalf("Load() = %#v, %v, reads %d", aggregate, err, reads)
	}
}

func TestAggregateRepositoryValidatesSnapshotRestoreBoundaries(t *testing.T) {
	t.Parallel()

	backing := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backing.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{mustPendingForRepository(
			t,
			"history-1",
			stream,
			mustEncodedEvent(t, "account.opened", 1, []byte(`{"owner":"Ada"}`)),
		)},
	); err != nil {
		t.Fatal(err)
	}
	repository := repositoryFromConfig(
		t,
		completeRepositoryConfig(t, backing),
	)
	restore := func() (*repositoryAccount, error) {
		return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
	}
	var nilContext context.Context
	if _, err := repository.Restore(
		nilContext,
		"account-42",
		1,
		restore,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Restore(nil context) error = %v", err)
	}
	if _, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		nil,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Restore(nil factory) error = %v", err)
	}
	var nilRepository *eventsourcing.AggregateRepository[string, *repositoryAccount]
	if _, err := nilRepository.Restore(
		context.Background(),
		"account-42",
		1,
		restore,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Restore() error = %v", err)
	}

	restoreFailure := errors.New("private snapshot state")
	if _, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		func() (*repositoryAccount, error) {
			return nil, restoreFailure
		},
	); !errors.Is(err, restoreFailure) ||
		strings.Contains(err.Error(), restoreFailure.Error()) {
		t.Fatalf("Restore(factory failure) error = %v", err)
	}

	nilLifecycleConfig := completeRepositoryConfig(t, backing)
	nilLifecycleConfig.Lifecycle = func(*repositoryAccount) *eventsourcing.Lifecycle {
		return nil
	}
	nilLifecycle := repositoryFromConfig(t, nilLifecycleConfig)
	if _, err := nilLifecycle.Restore(
		context.Background(),
		"account-42",
		1,
		restore,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Restore(nil lifecycle) error = %v", err)
	}

	nonPristine := &repositoryAccount{id: "account-42", owner: "Ada"}
	if err := nonPristine.lifecycle.Record(
		repositoryOpenedEvent(t),
		nonPristine.apply,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		func() (*repositoryAccount, error) { return nonPristine, nil },
	); !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("Restore(non-pristine) error = %v", err)
	}

	encodeFailure := errors.New("encode failed")
	encodeConfig := completeRepositoryConfig(t, backing)
	encodeConfig.EncodeID = func(string) (string, error) {
		return "", encodeFailure
	}
	encodeRepository := repositoryFromConfig(t, encodeConfig)
	if _, err := encodeRepository.Restore(
		context.Background(),
		"account-42",
		1,
		restore,
	); !errors.Is(err, encodeFailure) {
		t.Fatalf("Restore(encode failure) error = %v", err)
	}
}

func TestAggregateRepositoryRejectsInvalidSnapshotVersionReads(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	message := repositoryMessageAt(t, stream, 1)
	otherStream, err := eventsourcing.NewStreamID("account", "account-43")
	if err != nil {
		t.Fatal(err)
	}
	iteratorFailure := errors.New("iterator failed")
	closeFailure := errors.New("close failed")
	cases := map[string]struct {
		iterator eventsourcing.MessageIterator
		want     error
		also     error
	}{
		"nil iterator": {
			want: eventsourcing.ErrInvalidArgument,
		},
		"wrong stream": {
			iterator: &repositorySliceIterator{
				messages: []eventsourcing.Message{
					repositoryMessageAt(t, otherStream, 1),
				},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"wrong version": {
			iterator: &repositorySliceIterator{
				messages: []eventsourcing.Message{
					repositoryMessageAt(t, stream, 2),
				},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"too many": {
			iterator: &repositorySliceIterator{
				messages: []eventsourcing.Message{message, message},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"iterator and close": {
			iterator: &repositorySliceIterator{
				messages: []eventsourcing.Message{message},
				err:      iteratorFailure,
				closeErr: closeFailure,
			},
			want: iteratorFailure,
			also: closeFailure,
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &repositoryStoreFuncs{
				read: func(
					context.Context,
					eventsourcing.StreamID,
					eventsourcing.ReadStreamOptions,
				) (eventsourcing.MessageIterator, error) {
					return testCase.iterator, nil
				},
			}
			repository := repositoryFromConfig(
				t,
				completeRepositoryConfig(t, store),
			)
			result, err := repository.Restore(
				context.Background(),
				"account-42",
				1,
				func() (*repositoryAccount, error) {
					t.Fatal("restore factory ran after invalid version read")

					return nil, nil
				},
			)
			if !errors.Is(err, testCase.want) ||
				(testCase.also != nil && !errors.Is(err, testCase.also)) ||
				result != nil {
				t.Fatalf("Restore() = %#v, %v", result, err)
			}
		})
	}
}

func TestAggregateRepositoryRestorationHandlesReadFailureAndMaximumVersion(
	t *testing.T,
) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	readFailure := errors.New("read newer history failed")
	store := &repositoryStoreFuncs{
		read: func(
			_ context.Context,
			_ eventsourcing.StreamID,
			options eventsourcing.ReadStreamOptions,
		) (eventsourcing.MessageIterator, error) {
			if options.FromVersion() == 1 {
				return &repositorySliceIterator{
					messages: []eventsourcing.Message{
						repositoryMessageAt(t, stream, 1),
					},
				}, nil
			}

			return nil, readFailure
		},
	}
	repository := repositoryFromConfig(t, completeRepositoryConfig(t, store))
	if result, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		func() (*repositoryAccount, error) {
			return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
		},
	); !errors.Is(err, readFailure) || result != nil {
		t.Fatalf("Restore(read failure) = %#v, %v", result, err)
	}

	maximum := ^uint64(0)
	maxStore := repositoryReadStore(
		&repositorySliceIterator{
			messages: []eventsourcing.Message{
				repositoryMessageAt(t, stream, maximum),
			},
		},
	)
	maxRepository := repositoryFromConfig(
		t,
		completeRepositoryConfig(t, maxStore),
	)
	result, err := maxRepository.Restore(
		context.Background(),
		"account-42",
		maximum,
		func() (*repositoryAccount, error) {
			return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
		},
	)
	if err != nil ||
		result == nil ||
		result.lifecycle.CommittedVersion() != maximum {
		t.Fatalf("Restore(maximum) = %#v, %v", result, err)
	}
}

func TestAggregateRepositoryStopsAfterShortRestorePage(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	unexpectedRead := errors.New("unexpected read after short restore page")
	store := &repositoryStoreFuncs{
		read: func(
			_ context.Context,
			_ eventsourcing.StreamID,
			options eventsourcing.ReadStreamOptions,
		) (eventsourcing.MessageIterator, error) {
			reads++
			switch reads {
			case 1:
				if options.FromVersion() != 1 || options.ToVersion() != 1 ||
					options.Limit() != 1 {
					t.Fatalf("snapshot ReadStream() options = %#v", options)
				}

				return &repositorySliceIterator{messages: []eventsourcing.Message{
					repositoryMessageAt(t, stream, 1),
				}}, nil
			case 2:
				if options.FromVersion() != 2 || options.ToVersion() != 0 ||
					options.Limit() != 2 {
					t.Fatalf("history ReadStream() options = %#v", options)
				}

				return &repositorySliceIterator{messages: []eventsourcing.Message{
					repositoryMessageAt(t, stream, 2),
				}}, nil
			default:
				return nil, unexpectedRead
			}
		},
	}
	config := completeRepositoryConfig(t, store)
	config.ReadBatchSize = 2
	repository := repositoryFromConfig(t, config)
	restored, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		func() (*repositoryAccount, error) {
			return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
		},
	)
	if err != nil || restored == nil ||
		restored.lifecycle.CommittedVersion() != 2 || reads != 2 {
		t.Fatalf("Restore() = %#v, %v, reads %d", restored, err, reads)
	}
}

func TestAggregateRepositoryRejectsInvalidEvolutionAndDecoding(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	unknown := mustEncodedEvent(t, "account.unknown", 1, []byte(`{}`))
	unknownMessage := persistedRepositoryMessage(t, stream, unknown)
	upcastFailure := errors.New("upcast failed")
	failingRule := mustUpcastRule(
		t,
		"account.unknown",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return nil, upcastFailure
		},
	)
	failingUpcasters, err := eventsourcing.NewUpcasterChain(failingRule)
	if err != nil {
		t.Fatal(err)
	}
	config := completeRepositoryConfig(
		t,
		repositoryReadStore(&repositorySliceIterator{
			messages: []eventsourcing.Message{unknownMessage},
		}),
	)
	config.Upcasters = failingUpcasters
	repository, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate, err := repository.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, upcastFailure) || aggregate != nil {
		t.Fatalf("Load(upcast) = %#v, %v", aggregate, err)
	}

	config = completeRepositoryConfig(
		t,
		repositoryReadStore(&repositorySliceIterator{
			messages: []eventsourcing.Message{unknownMessage},
		}),
	)
	repository, err = eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate, err := repository.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, eventsourcing.ErrUnknownEvent) || aggregate != nil {
		t.Fatalf("Load(decode) = %#v, %v", aggregate, err)
	}

	config = completeRepositoryConfig(
		t,
		repositoryReadStore(&repositorySliceIterator{
			messages: []eventsourcing.Message{unknownMessage},
		}),
	)
	config.Codec = repositoryCodecFuncs{
		decode: func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error) {
			return eventsourcing.DecodedEvent{}, nil
		},
	}
	repository, err = eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate, err := repository.Load(
		context.Background(),
		"account-42",
	); !errors.Is(err, eventsourcing.ErrCorruptHistory) || aggregate != nil {
		t.Fatalf("Load(zero decoded) = %#v, %v", aggregate, err)
	}
}

func TestAggregateRepositoryValidatesSaveBoundaries(t *testing.T) {
	t.Parallel()

	repository := newAccountRepository(
		t,
		memory.NewStore(),
		repositoryCodec(t),
		mustEmptyUpcasters(t),
		nil,
		nil,
	)
	account := &repositoryAccount{id: "account-42"}
	var nilContext context.Context
	if _, err := repository.Save(nilContext, account); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Save(nil) error = %v", err)
	}
	var nilRepository *eventsourcing.AggregateRepository[string, *repositoryAccount]
	if _, err := nilRepository.Save(context.Background(), account); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil Save() error = %v", err)
	}

	config := completeRepositoryConfig(t, memory.NewStore())
	config.Lifecycle = func(*repositoryAccount) *eventsourcing.Lifecycle {
		return nil
	}
	missingLifecycle, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missingLifecycle.Save(
		context.Background(),
		account,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(nil lifecycle) error = %v", err)
	}

	poisoned := &repositoryAccount{id: "account-42"}
	if err := poisoned.lifecycle.Record(
		repositoryOpenedEvent(t),
		func(eventsourcing.DecodedEvent) error { return errors.New("apply failed") },
	); err == nil {
		t.Fatal("Record() unexpectedly succeeded")
	}
	if _, err := repository.Save(
		context.Background(),
		poisoned,
	); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Save(poisoned) error = %v", err)
	}

	invalidID := &repositoryAccount{}
	if err := invalidID.lifecycle.Record(
		repositoryOpenedEvent(t),
		invalidID.apply,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Save(
		context.Background(),
		invalidID,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(invalid ID) error = %v", err)
	}

	oversized := &repositoryAccount{id: "account-oversized"}
	for range eventsourcing.MaxAppendMessages + 1 {
		if err := oversized.lifecycle.Record(
			repositoryOpenedEvent(t),
			oversized.apply,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.Save(
		context.Background(),
		oversized,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(oversized) error = %v", err)
	}
}

func TestAggregateRepositoryReportsPreparationFailures(t *testing.T) {
	t.Parallel()

	codecFailure := errors.New("codec failed")
	idFailure := errors.New("ID failed")
	contextFailure := errors.New("context failed")
	cases := map[string]struct {
		configure func(*eventsourcing.RepositoryConfig[string, *repositoryAccount])
		want      error
	}{
		"codec": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				config.Codec = repositoryCodecFuncs{
					encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
						return eventsourcing.EncodedEvent{}, codecFailure
					},
				}
			},
			want: codecFailure,
		},
		"codec identity": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				config.Codec = repositoryCodecFuncs{
					encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
						return mustEncodedEvent(t, "account.other", 1, []byte(`{}`)), nil
					},
				}
			},
			want: eventsourcing.ErrPersistenceMismatch,
		},
		"message ID": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				config.MessageIDs = eventsourcing.MessageIDGeneratorFunc(
					func(context.Context) (eventsourcing.MessageID, error) {
						return eventsourcing.MessageID{}, idFailure
					},
				)
			},
			want: idFailure,
		},
		"message context": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				config.MessageContext = func(
					*repositoryAccount,
					eventsourcing.DecodedEvent,
					int,
				) (eventsourcing.MessageContext, error) {
					return eventsourcing.MessageContext{}, contextFailure
				}
			},
			want: contextFailure,
		},
		"clock": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				config.Clock = eventsourcing.ClockFunc(func() time.Time {
					return time.Time{}
				})
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"decorator": {
			configure: func(config *eventsourcing.RepositoryConfig[string, *repositoryAccount]) {
				decorators, err := eventsourcing.NewMessageDecoratorChain(
					func(eventsourcing.PendingMessage) (eventsourcing.PendingMessage, error) {
						return eventsourcing.PendingMessage{}, io.ErrClosedPipe
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				config.Decorators = decorators
			},
			want: io.ErrClosedPipe,
		},
	}

	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := completeRepositoryConfig(t, memory.NewStore())
			testCase.configure(&config)
			repository, err := eventsourcing.NewRepository(config)
			if err != nil {
				t.Fatal(err)
			}
			account := &repositoryAccount{id: "account-42"}
			if err := account.lifecycle.Record(
				repositoryOpenedEvent(t),
				account.apply,
			); err != nil {
				t.Fatal(err)
			}
			result, err := repository.Save(context.Background(), account)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Save() error = %v, want %v", err, testCase.want)
			}
			if result.Persisted() {
				t.Fatal("preparation failure reported persistence")
			}
			changes, changesErr := account.lifecycle.Changes()
			if changesErr != nil || changes.Len() != 1 {
				t.Fatalf("Changes() = %d, %v", changes.Len(), changesErr)
			}
		})
	}
}

func TestAggregateRepositoryHandlesExceptionalAppendOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("unknown state changed during append", func(t *testing.T) {
		t.Parallel()

		appendFailure := errors.New("append unknown")
		account := &repositoryAccount{id: "account-42"}
		store := &repositoryStoreFuncs{
			append: func(
				context.Context,
				eventsourcing.StreamID,
				eventsourcing.ExpectedVersion,
				[]eventsourcing.PendingMessage,
			) ([]eventsourcing.Message, error) {
				if err := account.lifecycle.Record(
					repositoryOpenedEvent(t),
					account.apply,
				); err != nil {
					t.Fatal(err)
				}

				return nil, appendFailure
			},
		}
		config := completeRepositoryConfig(t, store)
		repository, err := eventsourcing.NewRepository(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := account.lifecycle.Record(
			repositoryOpenedEvent(t),
			account.apply,
		); err != nil {
			t.Fatal(err)
		}

		result, err := repository.Save(context.Background(), account)
		if !errors.Is(err, appendFailure) ||
			!errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
			t.Fatalf("Save() error = %v", err)
		}
		if result.Outcome() != eventsourcing.CommitUnknown ||
			!account.lifecycle.Poisoned() {
			t.Fatalf("SaveResult = %#v, poisoned %v", result, account.lifecycle.Poisoned())
		}
	})

	t.Run("invalid outcome", func(t *testing.T) {
		t.Parallel()

		store := &repositoryStoreFuncs{
			append: func(
				context.Context,
				eventsourcing.StreamID,
				eventsourcing.ExpectedVersion,
				[]eventsourcing.PendingMessage,
			) ([]eventsourcing.Message, error) {
				return nil, repositoryOutcomeError(99)
			},
		}
		repository := repositoryFromConfig(t, completeRepositoryConfig(t, store))
		account := accountWithPendingOpened(t)
		if _, err := repository.Save(
			context.Background(),
			account,
		); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("Save() error = %v", err)
		}
	})

	t.Run("committed mismatch", func(t *testing.T) {
		t.Parallel()

		store := &repositoryStoreFuncs{
			append: func(
				context.Context,
				eventsourcing.StreamID,
				eventsourcing.ExpectedVersion,
				[]eventsourcing.PendingMessage,
			) ([]eventsourcing.Message, error) {
				return nil, nil
			},
		}
		repository := repositoryFromConfig(t, completeRepositoryConfig(t, store))
		account := accountWithPendingOpened(t)
		result, err := repository.Save(context.Background(), account)
		if !errors.Is(err, eventsourcing.ErrPersistenceMismatch) ||
			!result.Persisted() ||
			!account.lifecycle.Poisoned() {
			t.Fatalf("Save() = %#v, %v, poisoned %v", result, err, account.lifecycle.Poisoned())
		}
	})

	t.Run("committed post-append error", func(t *testing.T) {
		t.Parallel()

		backing := memory.NewStore()
		postCommitFailure := errors.New("post-commit failure")
		store := &repositoryStoreFuncs{
			append: func(
				ctx context.Context,
				stream eventsourcing.StreamID,
				expected eventsourcing.ExpectedVersion,
				messages []eventsourcing.PendingMessage,
			) ([]eventsourcing.Message, error) {
				persisted, err := backing.Append(ctx, stream, expected, messages)
				if err != nil {
					return nil, err
				}

				return persisted, eventsourcing.NewAppendError(
					eventsourcing.CommitCommitted,
					postCommitFailure,
				)
			},
			read: backing.ReadStream,
		}
		config := completeRepositoryConfig(t, store)
		config.MessageContext = nil
		repository := repositoryFromConfig(t, config)
		account := accountWithPendingOpened(t)
		result, err := repository.Save(context.Background(), account)
		if !errors.Is(err, postCommitFailure) ||
			!result.Persisted() ||
			!result.DispatchAttempted() ||
			account.lifecycle.CommittedVersion() != 1 {
			t.Fatalf("Save() = %#v, %v, version %d", result, err, account.lifecycle.CommittedVersion())
		}
	})
}

func completeRepositoryConfig(
	t *testing.T,
	store eventsourcing.EventStore,
) eventsourcing.RepositoryConfig[string, *repositoryAccount] {
	t.Helper()

	upcasters := mustEmptyUpcasters(t)
	decorators, err := eventsourcing.NewMessageDecoratorChain()
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher()
	if err != nil {
		t.Fatal(err)
	}
	clock, err := eventsourcing.NewFixedClock(repositoryFixedTime())
	if err != nil {
		t.Fatal(err)
	}

	return accountRepositoryConfig(
		store,
		repositoryCodec(t),
		upcasters,
		decorators,
		dispatcher,
		clock,
	)
}

func mustEmptyUpcasters(t *testing.T) *eventsourcing.UpcasterChain {
	t.Helper()

	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}

	return upcasters
}

func repositoryFromConfig(
	t *testing.T,
	config eventsourcing.RepositoryConfig[string, *repositoryAccount],
) *eventsourcing.AggregateRepository[string, *repositoryAccount] {
	t.Helper()

	repository, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}

	return repository
}

func accountWithPendingOpened(t *testing.T) *repositoryAccount {
	t.Helper()

	account := &repositoryAccount{id: "account-42"}
	if err := account.lifecycle.Record(
		repositoryOpenedEvent(t),
		account.apply,
	); err != nil {
		t.Fatal(err)
	}

	return account
}

func persistedRepositoryMessage(
	t *testing.T,
	stream eventsourcing.StreamID,
	event eventsourcing.EncodedEvent,
) eventsourcing.Message {
	t.Helper()

	pending := mustPendingForRepository(t, "history-1", stream, event)
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func repositoryMessageAt(
	t *testing.T,
	stream eventsourcing.StreamID,
	version uint64,
) eventsourcing.Message {
	t.Helper()

	pending := mustPendingForRepository(
		t,
		"history-versioned",
		stream,
		mustEncodedEvent(
			t,
			"account.opened",
			1,
			[]byte(`{"owner":"Ada"}`),
		),
	)
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: version,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

type repositoryStoreFuncs struct {
	append func(
		context.Context,
		eventsourcing.StreamID,
		eventsourcing.ExpectedVersion,
		[]eventsourcing.PendingMessage,
	) ([]eventsourcing.Message, error)
	read func(
		context.Context,
		eventsourcing.StreamID,
		eventsourcing.ReadStreamOptions,
	) (eventsourcing.MessageIterator, error)
}

func (store *repositoryStoreFuncs) Append(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	messages []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if store.append == nil {
		return nil, errors.New("unexpected append")
	}

	return store.append(ctx, stream, expected, messages)
}

func (store *repositoryStoreFuncs) ReadStream(
	ctx context.Context,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	if store.read == nil {
		return nil, errors.New("unexpected read")
	}

	return store.read(ctx, stream, options)
}

func repositoryReadStore(
	iterator eventsourcing.MessageIterator,
) eventsourcing.EventStore {
	return &repositoryStoreFuncs{
		read: func(
			context.Context,
			eventsourcing.StreamID,
			eventsourcing.ReadStreamOptions,
		) (eventsourcing.MessageIterator, error) {
			return iterator, nil
		},
	}
}

type repositorySliceIterator struct {
	messages []eventsourcing.Message
	index    int
	current  eventsourcing.Message
	err      error
	closeErr error
}

func (iterator *repositorySliceIterator) Next(context.Context) bool {
	if iterator.index >= len(iterator.messages) {
		return false
	}
	iterator.current = iterator.messages[iterator.index]
	iterator.index++

	return true
}

func (iterator *repositorySliceIterator) Message() eventsourcing.Message {
	return iterator.current
}

func (iterator *repositorySliceIterator) Err() error {
	return iterator.err
}

func (iterator *repositorySliceIterator) Close() error {
	return iterator.closeErr
}

type repositoryCodecFuncs struct {
	encode func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error)
	decode func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error)
}

type repositoryOutcomeError eventsourcing.CommitOutcome

func (err repositoryOutcomeError) Error() string {
	return "invalid outcome"
}

func (err repositoryOutcomeError) CommitOutcome() eventsourcing.CommitOutcome {
	return eventsourcing.CommitOutcome(err)
}

func (codec repositoryCodecFuncs) Encode(
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	if codec.encode == nil {
		return eventsourcing.EncodedEvent{}, errors.New("unexpected encode")
	}

	return codec.encode(event)
}

func (codec repositoryCodecFuncs) Decode(
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	if codec.decode == nil {
		return eventsourcing.DecodedEvent{}, errors.New("unexpected decode")
	}

	return codec.decode(event)
}
