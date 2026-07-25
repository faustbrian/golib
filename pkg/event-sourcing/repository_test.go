package eventsourcing_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

type repositoryAccount struct {
	id        string
	owner     string
	email     string
	lifecycle eventsourcing.Lifecycle
}

type repositoryAccountOpened struct {
	Owner string `json:"owner"`
}

type repositoryOwnerSet struct {
	Owner string `json:"owner"`
}

type repositoryEmailChanged struct {
	Email string `json:"email"`
}

func TestAggregateRepositoryLoadsUpcastHistoryIncrementally(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	codec := repositoryCodec(t)
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	legacy := mustEncodedEvent(
		t,
		"legacy.account-created",
		1,
		[]byte(`{"owner":"Ada"}`),
	)
	obsolete := mustEncodedEvent(t, "legacy.audit-only", 1, []byte(`{}`))
	first := mustPendingForRepository(t, "history-1", stream, legacy)
	second := mustPendingForRepository(t, "history-2", stream, obsolete)
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first, second},
	); err != nil {
		t.Fatal(err)
	}

	split := mustUpcastRule(
		t,
		"legacy.account-created",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(
					t,
					"account.opened",
					1,
					input.Event().Payload(),
					input.Metadata(),
				),
				mustUpcastEvent(
					t,
					"account.owner-set",
					1,
					input.Event().Payload(),
					input.Metadata(),
				),
			}, nil
		},
	)
	policy, err := eventsourcing.NewReviewedDropPolicy(
		"legacy audit event is represented outside aggregate state",
		"maintainer",
		time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	drop := mustUpcastRule(
		t,
		"legacy.audit-only",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return nil, nil
		},
		eventsourcing.AllowUpcastDrop(policy),
	)
	upcasters, err := eventsourcing.NewUpcasterChain(split, drop)
	if err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(t, store, codec, upcasters, nil, nil)

	account, err := repository.Load(context.Background(), "account-42")
	if err != nil {
		t.Fatal(err)
	}
	if account.id != "account-42" ||
		account.owner != "Ada" ||
		account.lifecycle.CommittedVersion() != 2 {
		t.Fatalf("loaded account = %#v", account)
	}
}

func TestAggregateRepositorySavesAcknowledgesAndDispatches(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	codec := repositoryCodec(t)
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	var delivered []eventsourcing.Delivery
	consumer, err := eventsourcing.NewConsumer(
		"capture",
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			delivered = append(delivered, delivery)

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		t.Fatal(err)
	}
	decorator, err := eventsourcing.NewMetadataDecorator(
		map[string]string{"source": "repository"},
	)
	if err != nil {
		t.Fatal(err)
	}
	decorators, err := eventsourcing.NewMessageDecoratorChain(decorator)
	if err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(
		t,
		store,
		codec,
		upcasters,
		decorators,
		dispatcher,
	)
	account := &repositoryAccount{id: "account-42"}
	if err := account.lifecycle.Record(
		repositoryOpenedEvent(t),
		account.apply,
	); err != nil {
		t.Fatal(err)
	}
	email, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "account.email-changed",
		Version: 1,
		Value:   repositoryEmailChanged{Email: "ada@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := account.lifecycle.Record(email, account.apply); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != eventsourcing.CommitCommitted ||
		!result.Persisted() ||
		!result.DispatchAttempted() ||
		len(result.Messages()) != 2 ||
		len(result.PreparedMessages()) != 2 {
		t.Fatalf("SaveResult = outcome %v persisted %v dispatched %v messages %d prepared %d",
			result.Outcome(),
			result.Persisted(),
			result.DispatchAttempted(),
			len(result.Messages()),
			len(result.PreparedMessages()),
		)
	}
	if account.lifecycle.CommittedVersion() != 2 {
		t.Fatalf("CommittedVersion() = %d", account.lifecycle.CommittedVersion())
	}
	changes, err := account.lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Empty() {
		t.Fatalf("pending changes = %d", changes.Len())
	}
	if len(delivered) != 2 {
		t.Fatalf("deliveries = %d", len(delivered))
	}
	for index, delivery := range delivered {
		message := delivery.Message()
		correlationID, hasCorrelationID := message.CorrelationID()
		tenant, hasTenant := message.Tenant()
		if delivery.Mode() != eventsourcing.DeliveryLive ||
			message.StreamVersion() != uint64(index+1) ||
			message.Metadata()["source"] != "repository" ||
			message.RecordedAt() != repositoryFixedTime() ||
			message.ID().String() != "generated-"+strconv.Itoa(index+1) ||
			!hasCorrelationID ||
			correlationID.String() != "correlation-"+strconv.Itoa(index+1) ||
			!hasTenant ||
			tenant != "tenant-a" {
			t.Fatalf("delivery %d = %#v", index, delivery)
		}
	}
	returned := result.Messages()
	returned[0] = eventsourcing.Message{}
	if result.Messages()[0].ID().IsZero() {
		t.Fatal("SaveResult.Messages() aliases caller-owned slice")
	}
	prepared := result.PreparedMessages()
	prepared[0] = eventsourcing.PendingMessage{}
	if result.PreparedMessages()[0].ID().IsZero() {
		t.Fatal("SaveResult.PreparedMessages() aliases caller-owned slice")
	}

	delivered = nil
	result, err = repository.Save(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != eventsourcing.CommitNotCommitted ||
		result.Persisted() ||
		result.DispatchAttempted() ||
		len(result.Messages()) != 0 ||
		len(result.PreparedMessages()) != 0 ||
		len(delivered) != 0 {
		t.Fatalf("no-op SaveResult = %#v, deliveries %d", result, len(delivered))
	}
}

func TestAggregateRepositoryPreservesCommittedResultOnDispatchFailure(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	codec := repositoryCodec(t)
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	dispatchFailure := errors.New("dispatch failed")
	consumer, err := eventsourcing.NewConsumer(
		"failure",
		func(context.Context, eventsourcing.Delivery) error {
			return dispatchFailure
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(
		t,
		store,
		codec,
		upcasters,
		nil,
		dispatcher,
	)
	account := &repositoryAccount{id: "account-42"}
	if err := account.lifecycle.Record(
		repositoryOpenedEvent(t),
		account.apply,
	); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), account)
	if !errors.Is(err, dispatchFailure) {
		t.Fatalf("Save() error = %v", err)
	}
	if !result.Persisted() ||
		!result.DispatchAttempted() ||
		account.lifecycle.CommittedVersion() != 1 {
		t.Fatalf("SaveResult = %#v, version %d", result, account.lifecycle.CommittedVersion())
	}
}

func TestAggregateRepositoryMarksUnknownAppendForReconciliation(t *testing.T) {
	t.Parallel()

	appendFailure := errors.New("commit outcome unknown")
	store := &repositoryFaultStore{appendErr: appendFailure}
	codec := repositoryCodec(t)
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(t, store, codec, upcasters, nil, nil)
	account := &repositoryAccount{id: "account-42"}
	if err := account.lifecycle.Record(
		repositoryOpenedEvent(t),
		account.apply,
	); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), account)
	if !errors.Is(err, appendFailure) {
		t.Fatalf("Save() error = %v", err)
	}
	if result.Outcome() != eventsourcing.CommitUnknown ||
		result.Persisted() ||
		len(result.PreparedMessages()) != 1 ||
		result.PreparedMessages()[0].ID().String() != "generated-1" ||
		!account.lifecycle.Poisoned() {
		t.Fatalf("SaveResult = %#v, poisoned %v", result, account.lifecycle.Poisoned())
	}
}

func TestAggregateRepositoryLeavesChangesPendingOnConcurrencyConflict(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	codec := repositoryCodec(t)
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(t, store, codec, upcasters, nil, nil)
	first := &repositoryAccount{id: "account-42"}
	second := &repositoryAccount{id: "account-42"}
	for _, account := range []*repositoryAccount{first, second} {
		if err := account.lifecycle.Record(
			repositoryOpenedEvent(t),
			account.apply,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), second)
	if !errors.Is(err, eventsourcing.ErrConcurrencyConflict) {
		t.Fatalf("Save() error = %v", err)
	}
	if result.Outcome() != eventsourcing.CommitNotCommitted {
		t.Fatalf("Outcome() = %v", result.Outcome())
	}
	changes, changesErr := second.lifecycle.Changes()
	if changesErr != nil {
		t.Fatal(changesErr)
	}
	if changes.Len() != 1 || second.lifecycle.Poisoned() {
		t.Fatalf("pending = %d, poisoned %v", changes.Len(), second.lifecycle.Poisoned())
	}
}

func TestAggregateRepositorySavesAgainstLoadedExactVersion(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	codec := repositoryCodec(t)
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	opened := mustEncodedEvent(
		t,
		"account.opened",
		1,
		[]byte(`{"owner":"Ada"}`),
	)
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPendingForRepository(t, "history-1", stream, opened),
		},
	); err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(
		t,
		store,
		codec,
		mustEmptyUpcasters(t),
		nil,
		nil,
	)
	account, err := repository.Load(context.Background(), "account-42")
	if err != nil {
		t.Fatal(err)
	}
	email, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "account.email-changed",
		Version: 1,
		Value:   repositoryEmailChanged{Email: "ada@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := account.lifecycle.Record(email, account.apply); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted() || account.lifecycle.CommittedVersion() != 2 {
		t.Fatalf("SaveResult = %#v, version %d", result, account.lifecycle.CommittedVersion())
	}
}

func TestAggregateRepositoryRestoresSnapshotThenAppliesNewerHistory(
	t *testing.T,
) {
	t.Parallel()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	opened := mustPendingForRepository(
		t,
		"history-1",
		stream,
		mustEncodedEvent(
			t,
			"account.opened",
			1,
			[]byte(`{"owner":"Ada"}`),
		),
	)
	changed := mustPendingForRepository(
		t,
		"history-2",
		stream,
		mustEncodedEvent(
			t,
			"account.email-changed",
			1,
			[]byte(`{"email":"ada@example.com"}`),
		),
	)
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{opened, changed},
	); err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(
		t,
		store,
		repositoryCodec(t),
		mustEmptyUpcasters(t),
		nil,
		nil,
	)

	snapshotState := &repositoryAccount{id: "account-42", owner: "Ada"}
	restored, err := repository.Restore(
		context.Background(),
		"account-42",
		1,
		func() (*repositoryAccount, error) {
			return snapshotState, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if restored != snapshotState ||
		restored.owner != "Ada" ||
		restored.email != "ada@example.com" ||
		restored.lifecycle.CommittedVersion() != 2 {
		t.Fatalf("restored account = %#v", restored)
	}

	completeState := &repositoryAccount{
		id:    "account-42",
		owner: "Ada",
		email: "ada@example.com",
	}
	restored, err = repository.Restore(
		context.Background(),
		"account-42",
		2,
		func() (*repositoryAccount, error) {
			return completeState, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if restored != completeState ||
		restored.lifecycle.CommittedVersion() != 2 {
		t.Fatalf("complete restored account = %#v", restored)
	}
}

func TestAggregateRepositoryRejectsInvalidSnapshotRestoration(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{mustPendingForRepository(
			t,
			"history-1",
			stream,
			mustEncodedEvent(
				t,
				"account.opened",
				1,
				[]byte(`{"owner":"Ada"}`),
			),
		)},
	); err != nil {
		t.Fatal(err)
	}
	repository := newAccountRepository(
		t,
		store,
		repositoryCodec(t),
		mustEmptyUpcasters(t),
		nil,
		nil,
	)
	cases := map[string]struct {
		id      string
		restore func() (*repositoryAccount, error)
		version uint64
		want    error
	}{
		"zero version": {
			id: "account-42",
			restore: func() (*repositoryAccount, error) {
				return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
		"wrong aggregate": {
			id: "account-42",
			restore: func() (*repositoryAccount, error) {
				return &repositoryAccount{id: "account-43", owner: "Ada"}, nil
			},
			version: 1,
			want:    eventsourcing.ErrInvalidArgument,
		},
		"invalid restored identifier": {
			id: "account-42",
			restore: func() (*repositoryAccount, error) {
				return &repositoryAccount{owner: "Ada"}, nil
			},
			version: 1,
			want:    eventsourcing.ErrInvalidArgument,
		},
		"ahead of history": {
			id: "account-42",
			restore: func() (*repositoryAccount, error) {
				return &repositoryAccount{id: "account-42", owner: "Ada"}, nil
			},
			version: 2,
			want:    eventsourcing.ErrCorruptHistory,
		},
		"missing stream": {
			id: "missing",
			restore: func() (*repositoryAccount, error) {
				return &repositoryAccount{id: "missing", owner: "Ada"}, nil
			},
			version: 1,
			want:    eventsourcing.ErrStreamNotFound,
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			restored, err := repository.Restore(
				context.Background(),
				testCase.id,
				testCase.version,
				testCase.restore,
			)
			if !errors.Is(err, testCase.want) || restored != nil {
				t.Fatalf("Restore() = %#v, %v", restored, err)
			}
		})
	}
}

func (account *repositoryAccount) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case accountOpened:
		account.owner = value.Owner
	case repositoryAccountOpened:
		account.owner = value.Owner
	case repositoryOwnerSet:
		account.owner = value.Owner
	case repositoryEmailChanged:
		account.email = value.Email
	default:
		return eventsourcing.ErrUnknownEvent
	}

	return nil
}

func newAccountRepository(
	t *testing.T,
	store eventsourcing.EventStore,
	codec eventsourcing.PayloadCodec,
	upcasters *eventsourcing.UpcasterChain,
	decorators *eventsourcing.MessageDecoratorChain,
	dispatcher eventsourcing.Dispatcher,
) *eventsourcing.AggregateRepository[string, *repositoryAccount] {
	t.Helper()

	if decorators == nil {
		var err error
		decorators, err = eventsourcing.NewMessageDecoratorChain()
		if err != nil {
			t.Fatal(err)
		}
	}
	if dispatcher == nil {
		var err error
		dispatcher, err = eventsourcing.NewSyncDispatcher()
		if err != nil {
			t.Fatal(err)
		}
	}
	clock, err := eventsourcing.NewFixedClock(repositoryFixedTime())
	if err != nil {
		t.Fatal(err)
	}
	nextID := 0
	config := accountRepositoryConfig(
		store,
		codec,
		upcasters,
		decorators,
		dispatcher,
		clock,
	)
	config.MessageIDs = eventsourcing.MessageIDGeneratorFunc(
		func(context.Context) (eventsourcing.MessageID, error) {
			nextID++

			return eventsourcing.NewMessageID("generated-" + strconv.Itoa(nextID))
		},
	)
	repository, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}

	return repository
}

func accountRepositoryConfig(
	store eventsourcing.EventStore,
	codec eventsourcing.PayloadCodec,
	upcasters *eventsourcing.UpcasterChain,
	decorators *eventsourcing.MessageDecoratorChain,
	dispatcher eventsourcing.Dispatcher,
	clock eventsourcing.Clock,
) eventsourcing.RepositoryConfig[string, *repositoryAccount] {
	return eventsourcing.RepositoryConfig[string, *repositoryAccount]{
		AggregateType: "account",
		EncodeID: func(id string) (string, error) {
			if id == "" {
				return "", eventsourcing.ErrInvalidArgument
			}

			return id, nil
		},
		Identify: func(account *repositoryAccount) string {
			return account.id
		},
		NewAggregate: func(id string) (*repositoryAccount, error) {
			return &repositoryAccount{id: id}, nil
		},
		Lifecycle: func(account *repositoryAccount) *eventsourcing.Lifecycle {
			return &account.lifecycle
		},
		Apply: func(
			account *repositoryAccount,
			event eventsourcing.DecodedEvent,
		) error {
			return account.apply(event)
		},
		Store:     store,
		Codec:     codec,
		Upcasters: upcasters,
		Clock:     clock,
		MessageIDs: eventsourcing.MessageIDGeneratorFunc(func(context.Context) (eventsourcing.MessageID, error) {
			return eventsourcing.NewMessageID("generated-default")
		}),
		Decorators: decorators,
		Dispatcher: dispatcher,
		MessageContext: func(
			_ *repositoryAccount,
			_ eventsourcing.DecodedEvent,
			index int,
		) (eventsourcing.MessageContext, error) {
			return eventsourcing.MessageContext{
				CorrelationID: "correlation-" + strconv.Itoa(index+1),
				Tenant:        "tenant-a",
			}, nil
		},
		ReadBatchSize: 1,
	}
}

func repositoryCodec(t *testing.T) eventsourcing.PayloadCodec {
	t.Helper()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[repositoryAccountOpened]("account.opened", 1),
		eventsourcing.JSONEvent[repositoryOwnerSet]("account.owner-set", 1),
		eventsourcing.JSONEvent[repositoryEmailChanged]("account.email-changed", 1),
	)
	if err != nil {
		t.Fatal(err)
	}

	return codec
}

func repositoryFixedTime() time.Time {
	return time.Date(2026, time.July, 25, 8, 0, 0, 0, time.UTC)
}

func repositoryOpenedEvent(t *testing.T) eventsourcing.DecodedEvent {
	t.Helper()

	event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "account.opened",
		Version: 1,
		Value:   repositoryAccountOpened{Owner: "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func mustEncodedEvent(
	t *testing.T,
	name string,
	version eventsourcing.SchemaVersion,
	payload []byte,
) eventsourcing.EncodedEvent {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     version,
		ContentType: eventsourcing.JSONContentType,
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func mustPendingForRepository(
	t *testing.T,
	id string,
	stream eventsourcing.StreamID,
	event eventsourcing.EncodedEvent,
) eventsourcing.PendingMessage {
	t.Helper()

	message, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         id,
		Stream:     stream,
		Event:      event,
		RecordedAt: repositoryFixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

type repositoryFaultStore struct {
	appendErr error
}

func (store *repositoryFaultStore) Append(
	context.Context,
	eventsourcing.StreamID,
	eventsourcing.ExpectedVersion,
	[]eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	return nil, store.appendErr
}

func (*repositoryFaultStore) ReadStream(
	context.Context,
	eventsourcing.StreamID,
	eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	return nil, eventsourcing.ErrStreamNotFound
}
