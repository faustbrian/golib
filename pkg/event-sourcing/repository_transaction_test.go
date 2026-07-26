package eventsourcing_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestAggregateRepositoryPlansConfirmsAndDispatchesCallerTransaction(
	t *testing.T,
) {
	t.Parallel()

	store := memory.NewStore()
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	var delivered []eventsourcing.Delivery
	consumer, err := eventsourcing.NewConsumer(
		"capture-transactional-save",
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
	repository := newAccountRepository(
		t,
		store,
		repositoryCodec(t),
		upcasters,
		nil,
		dispatcher,
	)
	account := accountWithPendingOpened(t)

	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Empty() ||
		plan.Stream().AggregateID() != "account-42" ||
		plan.ExpectedVersion().Mode() != eventsourcing.ExpectedVersionNew ||
		len(plan.PreparedMessages()) != 1 ||
		plan.PreparedMessages()[0].ID().String() != "generated-1" ||
		account.lifecycle.CommittedVersion() != 0 ||
		len(delivered) != 0 {
		t.Fatalf(
			"PrepareSave() = empty %v stream %s expected %v prepared %d version %d delivered %d",
			plan.Empty(),
			plan.Stream(),
			plan.ExpectedVersion(),
			len(plan.PreparedMessages()),
			account.lifecycle.CommittedVersion(),
			len(delivered),
		)
	}

	messages, err := store.Append(
		context.Background(),
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.ConfirmCommitted(account, plan, messages)
	if err != nil {
		t.Fatal(err)
	}
	changes, changesErr := account.lifecycle.Changes()
	if changesErr != nil {
		t.Fatal(changesErr)
	}
	if !result.Persisted() ||
		result.DispatchAttempted() ||
		len(result.Messages()) != 1 ||
		account.lifecycle.CommittedVersion() != 1 ||
		!changes.Empty() ||
		len(delivered) != 0 {
		t.Fatalf(
			"ConfirmCommitted() = %#v version %d pending %d delivered %d",
			result,
			account.lifecycle.CommittedVersion(),
			changes.Len(),
			len(delivered),
		)
	}

	result, err = repository.DispatchCommitted(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted() ||
		!result.DispatchAttempted() ||
		len(delivered) != 1 ||
		delivered[0].Mode() != eventsourcing.DeliveryLive ||
		!delivered[0].Message().Equal(messages[0]) {
		t.Fatalf("DispatchCommitted() = %#v delivered %#v", result, delivered)
	}
}

func TestAggregateRepositoryCallerTransactionNoOp(t *testing.T) {
	t.Parallel()

	repository, _ := newMemoryAccountRepository(t)
	account := &repositoryAccount{id: "account-42"}

	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() || len(plan.PreparedMessages()) != 0 {
		t.Fatalf("PrepareSave() = %#v", plan)
	}
	result, err := repository.ConfirmCommitted(account, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != eventsourcing.CommitNotCommitted ||
		result.DispatchAttempted() ||
		len(result.Messages()) != 0 {
		t.Fatalf("ConfirmCommitted() = %#v", result)
	}
}

func TestAggregateRepositoryMarksCallerTransactionCommitUnknown(t *testing.T) {
	t.Parallel()

	repository, store := newMemoryAccountRepository(t)
	account := accountWithPendingOpened(t)
	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Append(
		context.Background(),
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("commit acknowledgement lost")
	result, err := repository.MarkCommitUnknown(account, plan, staged, cause)
	if !errors.Is(err, cause) ||
		eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitUnknown {
		t.Fatalf("MarkCommitUnknown() error = %v", err)
	}
	if result.Outcome() != eventsourcing.CommitUnknown ||
		len(result.PreparedMessages()) != 1 ||
		len(result.Messages()) != 1 ||
		result.DispatchAttempted() {
		t.Fatalf("MarkCommitUnknown() = %#v", result)
	}
	if _, changesErr := account.lifecycle.Changes(); !errors.Is(changesErr, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Changes() error = %v", changesErr)
	}
}

func TestAggregateRepositoryRejectsForeignCallerTransactionArtifacts(
	t *testing.T,
) {
	t.Parallel()

	repository, store := newMemoryAccountRepository(t)
	foreign, _ := newMemoryAccountRepository(t)
	account := accountWithPendingOpened(t)
	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Append(
		context.Background(),
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.ConfirmCommitted(account, plan, staged); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ConfirmCommitted(foreign) error = %v", err)
	}
	result, err := repository.ConfirmCommitted(account, plan, staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.DispatchCommitted(context.Background(), result); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("DispatchCommitted(foreign) error = %v", err)
	}
	var nilContext context.Context
	if _, err := repository.DispatchCommitted(nilContext, result); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("DispatchCommitted(nil) error = %v", err)
	}
}

func TestAggregateRepositoryRejectsInvalidCallerTransactionTransitions(
	t *testing.T,
) {
	t.Parallel()

	repository, store := newMemoryAccountRepository(t)
	account := accountWithPendingOpened(t)
	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Append(
		context.Background(),
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
	if err != nil {
		t.Fatal(err)
	}

	var nilRepository *eventsourcing.AggregateRepository[
		string,
		*repositoryAccount,
	]
	if _, err := nilRepository.ConfirmCommitted(account, plan, staged); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil ConfirmCommitted() error = %v", err)
	}
	if _, err := repository.ConfirmCommitted(
		&repositoryAccount{id: "account-42"},
		plan,
		staged,
	); !errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
		t.Fatalf("ConfirmCommitted(other aggregate) error = %v", err)
	}
	if _, err := repository.MarkCommitUnknown(account, plan, staged, nil); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("MarkCommitUnknown(nil cause) error = %v", err)
	}
	if _, err := nilRepository.MarkCommitUnknown(
		account,
		plan,
		staged,
		errors.New("unknown"),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil MarkCommitUnknown() error = %v", err)
	}
	if _, err := repository.MarkCommitUnknown(
		&repositoryAccount{id: "account-42"},
		plan,
		staged,
		errors.New("unknown"),
	); !errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
		t.Fatalf("MarkCommitUnknown(other aggregate) error = %v", err)
	}
	if _, err := repository.MarkCommitUnknown(
		account,
		plan,
		nil,
		errors.New("unknown"),
	); !errors.Is(err, eventsourcing.ErrPersistenceMismatch) {
		t.Fatalf("MarkCommitUnknown(missing messages) error = %v", err)
	}
	if _, err := repository.MarkCommitUnknown(
		account,
		plan,
		[]eventsourcing.Message{{}},
		errors.New("unknown"),
	); !errors.Is(err, eventsourcing.ErrPersistenceMismatch) {
		t.Fatalf("MarkCommitUnknown(mismatched message) error = %v", err)
	}

	result, err := repository.ConfirmCommitted(account, plan, staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ConfirmCommitted(account, plan, staged); !errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
		t.Fatalf("ConfirmCommitted(reused plan) error = %v", err)
	}
	unknownCause := errors.New("late ambiguity")
	unknownResult, err := repository.MarkCommitUnknown(
		account,
		plan,
		staged,
		unknownCause,
	)
	if !errors.Is(err, unknownCause) ||
		!errors.Is(err, eventsourcing.ErrInvalidChangeSet) ||
		unknownResult.Outcome() != eventsourcing.CommitUnknown {
		t.Fatalf("MarkCommitUnknown(reused plan) = %#v, %v", unknownResult, err)
	}
	result, err = repository.DispatchCommitted(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DispatchCommitted(context.Background(), result); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("DispatchCommitted(reused result) error = %v", err)
	}
}

func TestAggregateRepositoryRejectsMissingCallerTransactionLifecycle(
	t *testing.T,
) {
	t.Parallel()

	store := memory.NewStore()
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
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
	config := accountRepositoryConfig(
		store,
		repositoryCodec(t),
		upcasters,
		decorators,
		dispatcher,
		clock,
	)
	lifecycleAvailable := true
	config.Lifecycle = func(account *repositoryAccount) *eventsourcing.Lifecycle {
		if !lifecycleAvailable {
			return nil
		}

		return &account.lifecycle
	}
	repository, err := eventsourcing.NewRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	account := accountWithPendingOpened(t)
	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Append(
		context.Background(),
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
	if err != nil {
		t.Fatal(err)
	}
	lifecycleAvailable = false
	if _, err := repository.ConfirmCommitted(account, plan, staged); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ConfirmCommitted() error = %v", err)
	}
	if _, err := repository.MarkCommitUnknown(
		account,
		plan,
		staged,
		errors.New("unknown"),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("MarkCommitUnknown() error = %v", err)
	}
}

func TestAggregateRepositoryRejectsMessagesForEmptyCallerTransaction(
	t *testing.T,
) {
	t.Parallel()

	repository, _ := newMemoryAccountRepository(t)
	account := &repositoryAccount{id: "account-42"}
	plan, err := repository.PrepareSave(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ConfirmCommitted(
		account,
		plan,
		[]eventsourcing.Message{{}},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ConfirmCommitted(empty with message) error = %v", err)
	}
}

func newMemoryAccountRepository(
	t *testing.T,
) (*eventsourcing.AggregateRepository[string, *repositoryAccount], *memory.Store) {
	t.Helper()

	store := memory.NewStore()
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}

	return newAccountRepository(
		t,
		store,
		repositoryCodec(t),
		upcasters,
		nil,
		nil,
	), store
}
