//go:build integration

package gooutbox_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	outboxrelay "github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPreparedSaveStagesCommitsAndConfirmsOneAtomicLifecycle(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	repository := newRecoveryRepository(t, pool, "lifecycle-message-1", nil)
	account := recoveryAccountWithPendingOpened(t, "lifecycle-account")
	plan, err := repository.PrepareSave(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	prepared := plan.PreparedMessages()
	if plan.Empty() || len(prepared) != 1 ||
		prepared[0].ID().String() != "lifecycle-message-1" ||
		account.lifecycle.CommittedVersion() != 0 {
		t.Fatalf("prepared plan = %#v at version %d", prepared, account.lifecycle.CommittedVersion())
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager := newRecoveryStager(t, tx)
	messages, err := stager.StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID() != prepared[0].ID() ||
		account.lifecycle.CommittedVersion() != 0 {
		t.Fatalf("staged messages = %#v at version %d", messages, account.lifecycle.CommittedVersion())
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if account.lifecycle.CommittedVersion() != 0 {
		t.Fatal("database commit acknowledged aggregate before ConfirmCommitted")
	}
	result, err := repository.ConfirmCommitted(account, plan, messages)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := account.lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted() || result.DispatchAttempted() ||
		account.lifecycle.CommittedVersion() != 1 || !changes.Empty() {
		t.Fatalf("confirmed result = %#v, version %d, pending %d", result, account.lifecycle.CommittedVersion(), changes.Len())
	}
	envelope := loadRecoveryEnvelope(t, ctx, pool, messages[0].ID().String())
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	expectedEnvelope, err := codec.Encode(messages[0])
	if err != nil {
		t.Fatal(err)
	}
	if envelope.ID != expectedEnvelope.ID ||
		envelope.Topic != expectedEnvelope.Topic ||
		!bytes.Equal(envelope.Payload, expectedEnvelope.Payload) ||
		envelope.PayloadVersion != expectedEnvelope.PayloadVersion ||
		!reflect.DeepEqual(envelope.Metadata, expectedEnvelope.Metadata) ||
		envelope.OrderingKey != expectedEnvelope.OrderingKey ||
		envelope.IdempotencyKey != expectedEnvelope.IdempotencyKey ||
		envelope.Attempts != expectedEnvelope.Attempts ||
		!envelope.AvailableAt.Equal(expectedEnvelope.AvailableAt) ||
		!envelope.CreatedAt.Equal(expectedEnvelope.CreatedAt) ||
		!decoded.Equal(messages[0]) {
		t.Fatalf("persisted envelope = %#v, decoded = %s", envelope, decoded)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestCommitResponseLossReconcilesBothPreparedIdentitiesBeforeRetry(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	repository := newRecoveryRepository(t, pool, "ambiguous-message-1", nil)
	account := recoveryAccountWithPendingOpened(t, "ambiguous-account")
	plan, err := repository.PrepareSave(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := newRecoveryStager(t, tx).StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	lostResponse := errors.New("commit response lost")
	result, markErr := repository.MarkCommitUnknown(account, plan, messages, lostResponse)
	if !errors.Is(markErr, lostResponse) ||
		eventsourcing.AppendCommitOutcome(markErr) != eventsourcing.CommitUnknown ||
		result.Outcome() != eventsourcing.CommitUnknown {
		t.Fatalf("MarkCommitUnknown() = %#v, %v", result, markErr)
	}
	if _, err := account.lifecycle.Changes(); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("ambiguous aggregate Changes() error = %v", err)
	}

	prepared := result.PreparedMessages()
	if len(prepared) != 1 {
		t.Fatalf("prepared identities = %#v", prepared)
	}
	messageID := prepared[0].ID().String()
	var matched int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
  FROM event_sourcing.messages AS events
  JOIN outbox_messages AS envelopes
    ON envelopes.id = events.message_id
   AND envelopes.idempotency_key = events.message_id
 WHERE events.message_id = $1`, messageID).Scan(&matched); err != nil {
		t.Fatal(err)
	}
	if matched != 1 {
		t.Fatalf("reconciled event/outbox identity pairs = %d, want 1", matched)
	}
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	readOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := eventStore.ReadStream(ctx, plan.Stream(), readOptions)
	assertSingleReadMessage(t, ctx, iterator, err, messages[0])
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	expectedEnvelope, err := codec.Encode(messages[0])
	if err != nil {
		t.Fatal(err)
	}
	actualEnvelope := loadRecoveryEnvelope(t, ctx, pool, messageID)
	actualEnvelope.AvailableAt = actualEnvelope.AvailableAt.UTC()
	actualEnvelope.CreatedAt = actualEnvelope.CreatedAt.UTC()
	if !bytes.Equal(
		actualEnvelope.CanonicalJSON(),
		expectedEnvelope.CanonicalJSON(),
	) {
		t.Fatalf(
			"reconciled envelope = %s, want %s",
			actualEnvelope.CanonicalJSON(),
			expectedEnvelope.CanonicalJSON(),
		)
	}
	loaded, err := repository.Load(ctx, "ambiguous-account")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.owner != "Ada" || loaded.lifecycle.CommittedVersion() != 1 {
		t.Fatalf("reconciled aggregate = %#v at version %d", loaded, loaded.lifecycle.CommittedVersion())
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestAbsentCommitReconciliationAllowsOnlyTheExactPreparedRetry(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	repository := newRecoveryRepository(t, pool, "absent-message-1", nil)
	account := recoveryAccountWithPendingOpened(t, "absent-account")
	plan, err := repository.PrepareSave(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := newRecoveryStager(t, tx).StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	lostResponse := errors.New("rollback response lost")
	result, markErr := repository.MarkCommitUnknown(account, plan, messages, lostResponse)
	if !errors.Is(markErr, lostResponse) ||
		result.Outcome() != eventsourcing.CommitUnknown {
		t.Fatalf("MarkCommitUnknown() = %#v, %v", result, markErr)
	}

	messageID := result.PreparedMessages()[0].ID().String()
	var events, envelopes int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages WHERE message_id = $1",
		messageID,
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM outbox_messages WHERE id = $1 AND idempotency_key = $1",
		messageID,
	).Scan(&envelopes); err != nil {
		t.Fatal(err)
	}
	if events != 0 || envelopes != 0 {
		t.Fatalf("absent reconciliation = (%d events, %d envelopes)", events, envelopes)
	}

	retryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := newRecoveryStager(t, retryTx).StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || !retried[0].Equal(messages[0]) ||
		retried[0].ID().String() != messageID {
		t.Fatalf("reconciled exact retry = %#v, original %#v", retried, messages)
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Load(ctx, "absent-account")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.owner != "Ada" || loaded.lifecycle.CommittedVersion() != 1 {
		t.Fatalf("retried aggregate = %#v at version %d", loaded, loaded.lifecycle.CommittedVersion())
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestExactPreparedPlanRetryKeepsIdentityAndCannotDuplicateCommittedRows(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	repository := newRecoveryRepository(t, pool, "retry-message-1", nil)
	account := recoveryAccountWithPendingOpened(t, "retry-account")
	plan, err := repository.PrepareSave(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	prepared := plan.PreparedMessages()

	firstTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstMessages, err := newRecoveryStager(t, firstTx).StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	retryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retryMessages, err := newRecoveryStager(t, retryTx).StagePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstMessages) != 1 || len(retryMessages) != 1 ||
		!firstMessages[0].Equal(retryMessages[0]) ||
		retryMessages[0].ID() != prepared[0].ID() {
		t.Fatalf("exact retry changed prepared identity: first %#v retry %#v", firstMessages, retryMessages)
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)

	duplicateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, stageErr := newRecoveryStager(t, duplicateTx).StagePlan(ctx, plan)
	if stageErr == nil || eventsourcing.AppendCommitOutcome(stageErr) != eventsourcing.CommitNotCommitted {
		t.Fatalf("committed exact-plan retry error = %v", stageErr)
	}
	if err := duplicateTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestAcceptedPublicationRetryKeepsIdentityAndConsumerEffectIdempotent(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	messages, err := newRecoveryStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{testPending(t, stream)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	realStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
	if err != nil {
		t.Fatal(err)
	}
	transitionFailure := errors.New("delivery acknowledgement lost")
	store := &failOnceDeliveredStore{
		Store:   realStore,
		failure: transitionFailure,
	}
	consumer := &idempotentAcceptingPublisher{processed: make(map[string]struct{})}
	relay, err := outboxrelay.New(store, consumer, outboxrelay.Config{
		Owner:         "duplicate-proof-relay",
		BatchSize:     1,
		Workers:       1,
		LeaseDuration: 50 * time.Millisecond,
		MaxAttempts:   3,
		PollInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, firstErr := relay.RunOnce(ctx)
	if !errors.Is(firstErr, transitionFailure) || first.Published != 1 ||
		first.Delivered != 0 {
		t.Fatalf("first accepted publication = %#v, %v", first, firstErr)
	}
	second, err := runRecoveryRelayAfterLeaseExpiry(ctx, relay)
	if err != nil || second.Published != 1 || second.Delivered != 1 {
		t.Fatalf("retried accepted publication = %#v, %v", second, err)
	}

	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	if len(consumer.calls) != 2 || consumer.effects != 1 ||
		consumer.calls[0].ID != messages[0].ID().String() ||
		consumer.calls[1].ID != consumer.calls[0].ID ||
		consumer.calls[0].IdempotencyKey != consumer.calls[0].ID ||
		consumer.calls[1].IdempotencyKey != consumer.calls[0].IdempotencyKey {
		t.Fatalf("consumer calls = %#v, effects = %d", consumer.calls, consumer.effects)
	}
	var state string
	if err := pool.QueryRow(
		ctx,
		"SELECT state FROM outbox_messages WHERE id = $1",
		messages[0].ID().String(),
	).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "delivered" {
		t.Fatalf("outbox state = %q, want delivered", state)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

type recoveryAccount struct {
	id        string
	owner     string
	lifecycle eventsourcing.Lifecycle
}

type recoveryAccountOpened struct {
	Owner string `json:"owner"`
}

func (account *recoveryAccount) apply(event eventsourcing.DecodedEvent) error {
	opened, ok := event.Value().(recoveryAccountOpened)
	if !ok {
		return eventsourcing.ErrUnknownEvent
	}
	account.owner = opened.Owner

	return nil
}

func recoveryAccountWithPendingOpened(t *testing.T, id string) *recoveryAccount {
	t.Helper()

	account := &recoveryAccount{id: id}
	event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "account.opened",
		Version: 1,
		Value:   recoveryAccountOpened{Owner: "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := account.lifecycle.Record(event, account.apply); err != nil {
		t.Fatal(err)
	}

	return account
}

func newRecoveryRepository(
	t *testing.T,
	pool *pgxpool.Pool,
	messageID string,
	dispatcher eventsourcing.Dispatcher,
) *eventsourcing.AggregateRepository[string, *recoveryAccount] {
	t.Helper()

	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[recoveryAccountOpened]("account.opened", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	decorators, err := eventsourcing.NewMessageDecoratorChain()
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher == nil {
		dispatcher, err = eventsourcing.NewSyncDispatcher()
		if err != nil {
			t.Fatal(err)
		}
	}
	clock, err := eventsourcing.NewFixedClock(
		time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := eventsourcing.NewRepository(eventsourcing.RepositoryConfig[
		string,
		*recoveryAccount,
	]{
		AggregateType: "account",
		EncodeID:      func(id string) (string, error) { return id, nil },
		Identify:      func(account *recoveryAccount) string { return account.id },
		NewAggregate: func(id string) (*recoveryAccount, error) {
			return &recoveryAccount{id: id}, nil
		},
		Lifecycle: func(account *recoveryAccount) *eventsourcing.Lifecycle {
			return &account.lifecycle
		},
		Apply: func(account *recoveryAccount, event eventsourcing.DecodedEvent) error {
			return account.apply(event)
		},
		Store:      store,
		Codec:      codec,
		Upcasters:  upcasters,
		Clock:      clock,
		MessageIDs: fixedRecoveryMessageID(messageID),
		Decorators: decorators,
		Dispatcher: dispatcher,
	})
	if err != nil {
		t.Fatal(err)
	}

	return repository
}

func fixedRecoveryMessageID(value string) eventsourcing.MessageIDGenerator {
	return eventsourcing.MessageIDGeneratorFunc(
		func(ctx context.Context) (eventsourcing.MessageID, error) {
			if err := ctx.Err(); err != nil {
				return eventsourcing.MessageID{}, err
			}

			return eventsourcing.NewMessageID(value)
		},
	)
}

func newRecoveryStager(t *testing.T, tx pgx.Tx) *gooutbox.Stager {
	t.Helper()

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}

	return stager
}

func loadRecoveryEnvelope(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
) outbox.Envelope {
	t.Helper()

	var envelope outbox.Envelope
	var metadata []byte
	if err := pool.QueryRow(ctx, `
SELECT id, topic, payload, payload_version, metadata, ordering_key,
       idempotency_key, attempts, available_at, created_at
  FROM outbox_messages
 WHERE id = $1`, id).Scan(
		&envelope.ID,
		&envelope.Topic,
		&envelope.Payload,
		&envelope.PayloadVersion,
		&metadata,
		&envelope.OrderingKey,
		&envelope.IdempotencyKey,
		&envelope.Attempts,
		&envelope.AvailableAt,
		&envelope.CreatedAt,
	); err != nil {
		t.Fatal(err)
	}
	envelope.Metadata = decodeMetadata(t, metadata)

	return envelope
}

func runRecoveryRelayAfterLeaseExpiry(
	ctx context.Context,
	relay *outboxrelay.Relay,
) (outboxrelay.Result, error) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return outboxrelay.Result{}, ctx.Err()
		case <-deadline.C:
			return outboxrelay.Result{}, errors.New("lease did not become reclaimable")
		case <-ticker.C:
			result, err := relay.RunOnce(ctx)
			if err != nil || result.Claimed != 0 {
				return result, err
			}
		}
	}
}

type failOnceDeliveredStore struct {
	*outboxpostgres.Store
	mu      sync.Mutex
	failure error
	failed  bool
}

func (store *failOnceDeliveredStore) MarkDelivered(
	ctx context.Context,
	lease outboxpostgres.LeaseRef,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.failed {
		store.failed = true
		return store.failure
	}

	return store.Store.MarkDelivered(ctx, lease)
}

type idempotentAcceptingPublisher struct {
	mu        sync.Mutex
	calls     []outbox.Envelope
	processed map[string]struct{}
	effects   int
}

func (publisher *idempotentAcceptingPublisher) Publish(
	ctx context.Context,
	envelope outbox.Envelope,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.calls = append(publisher.calls, envelope)
	if _, duplicate := publisher.processed[envelope.IdempotencyKey]; duplicate {
		return nil
	}
	publisher.processed[envelope.IdempotencyKey] = struct{}{}
	publisher.effects++

	return nil
}
