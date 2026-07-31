package eventsourcing

import (
	"context"
	"errors"
	"fmt"
)

const defaultRepositoryReadBatchSize = 256

// Repository loads and saves one aggregate type behind a small replaceable
// contract.
type Repository[ID, Aggregate any] interface {
	Load(context.Context, ID) (Aggregate, error)
	Save(context.Context, Aggregate) (SaveResult, error)
}

// IdentifierEncoder maps an application identifier to stable stream data.
type IdentifierEncoder[ID any] func(ID) (string, error)

// AggregateIdentifier returns an aggregate's application identifier.
type AggregateIdentifier[ID, Aggregate any] func(Aggregate) ID

// AggregateFactory creates a fresh aggregate before reconstitution.
type AggregateFactory[ID, Aggregate any] func(ID) (Aggregate, error)

// LifecycleAccessor returns the aggregate-owned lifecycle helper.
type LifecycleAccessor[Aggregate any] func(Aggregate) *Lifecycle

// AggregateApply invokes the aggregate's explicit historical-event switch.
type AggregateApply[Aggregate any] func(Aggregate, DecodedEvent) error

// AggregateRestorer decodes one owned aggregate state snapshot.
//
// The repository takes ownership of a successful result. Callers must not
// retain or reuse it when Restore returns an error.
type AggregateRestorer[Aggregate any] func() (Aggregate, error)

// MessageContext contains application-supplied envelope context for one
// pending aggregate event.
type MessageContext struct {
	Metadata      map[string]string
	CorrelationID string
	CausationID   string
	Tenant        string
	Partition     string
}

// MessageContextProvider supplies application envelope context in pending
// event order. A nil provider means empty context.
type MessageContextProvider[Aggregate any] func(
	Aggregate,
	DecodedEvent,
	int,
) (MessageContext, error)

// RepositoryConfig supplies every replaceable aggregate repository boundary.
type RepositoryConfig[ID, Aggregate any] struct {
	AggregateType  string
	EncodeID       IdentifierEncoder[ID]
	Identify       AggregateIdentifier[ID, Aggregate]
	NewAggregate   AggregateFactory[ID, Aggregate]
	Lifecycle      LifecycleAccessor[Aggregate]
	Apply          AggregateApply[Aggregate]
	Store          EventStore
	Codec          PayloadCodec
	Upcasters      Upcaster
	Clock          Clock
	MessageIDs     MessageIDGenerator
	Decorators     *MessageDecoratorChain
	Dispatcher     Dispatcher
	MessageContext MessageContextProvider[Aggregate]
	ReadBatchSize  uint32
}

// AggregateRepository is the reference storage-independent repository.
//
// It starts no goroutines and owns no transaction. One aggregate instance must
// not be saved concurrently.
type AggregateRepository[ID, Aggregate any] struct {
	saveOwner      *repositorySaveOwner
	aggregateType  string
	encodeID       IdentifierEncoder[ID]
	identify       AggregateIdentifier[ID, Aggregate]
	newAggregate   AggregateFactory[ID, Aggregate]
	lifecycle      LifecycleAccessor[Aggregate]
	apply          AggregateApply[Aggregate]
	store          EventStore
	codec          PayloadCodec
	decoder        *EventDecoder
	clock          Clock
	messageIDs     MessageIDGenerator
	decorators     *MessageDecoratorChain
	dispatcher     Dispatcher
	messageContext MessageContextProvider[Aggregate]
	readBatchSize  uint32
}

// NewRepository validates and owns a reference aggregate repository.
func NewRepository[ID, Aggregate any](
	config RepositoryConfig[ID, Aggregate],
) (*AggregateRepository[ID, Aggregate], error) {
	if !validName(config.AggregateType, MaxAggregateTypeBytes) {
		return nil, invalid("aggregate_type", "must be a non-empty canonical name")
	}
	if config.EncodeID == nil ||
		config.Identify == nil ||
		config.NewAggregate == nil ||
		config.Lifecycle == nil ||
		config.Apply == nil {
		return nil, invalid("aggregate_callback", "must be assigned")
	}
	if config.Store == nil ||
		config.Codec == nil ||
		config.Upcasters == nil ||
		config.Clock == nil ||
		config.MessageIDs == nil ||
		config.Decorators == nil ||
		config.Dispatcher == nil {
		return nil, invalid("dependency", "must be assigned")
	}
	if config.ReadBatchSize > MaxReadMessages {
		return nil, invalid("read_batch_size", "must be within the read limit")
	}
	if config.ReadBatchSize == 0 {
		config.ReadBatchSize = defaultRepositoryReadBatchSize
	}
	decoder := &EventDecoder{codec: config.Codec, upcasters: config.Upcasters}

	return &AggregateRepository[ID, Aggregate]{
		saveOwner:      &repositorySaveOwner{},
		aggregateType:  config.AggregateType,
		encodeID:       config.EncodeID,
		identify:       config.Identify,
		newAggregate:   config.NewAggregate,
		lifecycle:      config.Lifecycle,
		apply:          config.Apply,
		store:          config.Store,
		codec:          config.Codec,
		decoder:        decoder,
		clock:          config.Clock,
		messageIDs:     config.MessageIDs,
		decorators:     config.Decorators,
		dispatcher:     config.Dispatcher,
		messageContext: config.MessageContext,
		readBatchSize:  config.ReadBatchSize,
	}, nil
}

type repositorySaveOwner struct {
	_ byte
}

// SavePlan is an immutable prepared aggregate append for caller-owned
// transaction composition. Preparing a plan does not append, acknowledge,
// dispatch, or release pending aggregate changes.
//
// Its zero value is invalid. A non-empty plan belongs to the repository and
// aggregate lifecycle that created it and must not be reused after confirmation
// or an unknown commit outcome.
type SavePlan struct {
	owner    *repositorySaveOwner
	changes  ChangeSet
	stream   StreamID
	expected ExpectedVersion
	prepared []PendingMessage
}

// Empty reports a valid plan with no pending changes and no persistence work.
func (plan SavePlan) Empty() bool {
	return plan.owner != nil && plan.changes.Empty()
}

// Stream returns the aggregate stream to stage. It is zero for an empty plan.
func (plan SavePlan) Stream() StreamID {
	return plan.stream
}

// ExpectedVersion returns the append precondition. It is invalid for an empty
// plan.
func (plan SavePlan) ExpectedVersion() ExpectedVersion {
	return plan.expected
}

// PreparedMessages returns a defensive copy of the exact envelopes to stage.
func (plan SavePlan) PreparedMessages() []PendingMessage {
	return clonePendingMessages(plan.prepared)
}

// Load creates and reconstitutes one aggregate through bounded stream pages.
func (repository *AggregateRepository[ID, Aggregate]) Load(
	ctx context.Context,
	id ID,
) (Aggregate, error) {
	var zero Aggregate
	if ctx == nil || repository == nil {
		return zero, ErrInvalidArgument
	}
	stream, err := repository.streamID(id)
	if err != nil {
		return zero, err
	}
	aggregate, err := repository.newAggregate(id)
	if err != nil {
		return zero, fmt.Errorf("create aggregate: %w", err)
	}
	lifecycle := repository.lifecycle(aggregate)
	if lifecycle == nil {
		return zero, invalid("lifecycle", "accessor returned nil")
	}

	nextVersion := uint64(1)
	loaded := false
	for {
		count, err := repository.loadPage(
			ctx,
			stream,
			nextVersion,
			aggregate,
			lifecycle,
		)
		if err != nil {
			return zero, err
		}
		if count == 0 {
			if !loaded {
				return zero, ErrStreamNotFound
			}

			return aggregate, nil
		}
		loaded = true
		if count < repository.readBatchSize || lifecycle.committed == ^uint64(0) {
			return aggregate, nil
		}
		nextVersion += uint64(count)
	}
}

// Restore validates an authoritative stream at baseVersion, takes ownership of
// decoded snapshot state, and applies only later stored events.
func (repository *AggregateRepository[ID, Aggregate]) Restore(
	ctx context.Context,
	id ID,
	baseVersion uint64,
	restore AggregateRestorer[Aggregate],
) (Aggregate, error) {
	var zero Aggregate
	if ctx == nil || repository == nil || baseVersion == 0 || restore == nil {
		return zero, ErrInvalidArgument
	}
	stream, err := repository.streamID(id)
	if err != nil {
		return zero, err
	}
	if err := repository.verifySnapshotVersion(ctx, stream, baseVersion); err != nil {
		return zero, err
	}

	aggregate, err := restore()
	if err != nil {
		return zero, &SnapshotRestorationError{Cause: err}
	}
	aggregateStream, err := repository.streamID(repository.identify(aggregate))
	if err != nil || aggregateStream != stream {
		return zero, invalid("aggregate", "restored identifier does not match")
	}
	lifecycle := repository.lifecycle(aggregate)
	if lifecycle == nil {
		return zero, invalid("lifecycle", "accessor returned nil")
	}
	if err := lifecycle.RestoreSnapshotVersion(baseVersion); err != nil {
		return zero, err
	}
	if baseVersion == ^uint64(0) {
		return aggregate, nil
	}

	nextVersion := baseVersion + 1
	for {
		count, err := repository.loadPage(
			ctx,
			stream,
			nextVersion,
			aggregate,
			lifecycle,
		)
		if err != nil {
			return zero, err
		}
		if count == 0 {
			return aggregate, nil
		}
		if count < repository.readBatchSize ||
			lifecycle.committed == ^uint64(0) {
			return aggregate, nil
		}
		nextVersion += uint64(count)
	}
}

func (repository *AggregateRepository[ID, Aggregate]) verifySnapshotVersion(
	ctx context.Context,
	stream StreamID,
	baseVersion uint64,
) error {
	options := ReadStreamOptions{
		fromVersion: baseVersion,
		toVersion:   baseVersion,
		limit:       1,
	}
	iterator, err := repository.store.ReadStream(ctx, stream, options)
	if err != nil {
		return err
	}
	if iterator == nil {
		return invalid("iterator", "store returned nil")
	}

	var verifyErr error
	if !iterator.Next(ctx) {
		verifyErr = ErrCorruptHistory
	} else {
		message := iterator.Message()
		if message.Stream() != stream ||
			message.StreamVersion() != baseVersion ||
			iterator.Next(ctx) {
			verifyErr = ErrCorruptHistory
		}
	}

	return errors.Join(verifyErr, iterator.Err(), iterator.Close())
}

func (repository *AggregateRepository[ID, Aggregate]) loadPage(
	ctx context.Context,
	stream StreamID,
	fromVersion uint64,
	aggregate Aggregate,
	lifecycle *Lifecycle,
) (uint32, error) {
	options := ReadStreamOptions{
		fromVersion: fromVersion,
		limit:       repository.readBatchSize,
	}
	iterator, err := repository.store.ReadStream(ctx, stream, options)
	if err != nil {
		return 0, err
	}
	if iterator == nil {
		return 0, invalid("iterator", "store returned nil")
	}

	var count uint32
	var consumeErr error
	for iterator.Next(ctx) {
		if count >= repository.readBatchSize {
			consumeErr = ErrCorruptHistory

			break
		}
		message := iterator.Message()
		expected := fromVersion + uint64(count)
		if expected < fromVersion ||
			message.Stream() != stream ||
			message.StreamVersion() != expected {
			consumeErr = ErrCorruptHistory

			break
		}
		if err := repository.reconstituteMessage(
			ctx,
			aggregate,
			lifecycle,
			message,
		); err != nil {
			consumeErr = err

			break
		}
		count++
	}

	return count, errors.Join(consumeErr, iterator.Err(), iterator.Close())
}

func (repository *AggregateRepository[ID, Aggregate]) reconstituteMessage(
	ctx context.Context,
	aggregate Aggregate,
	lifecycle *Lifecycle,
	message Message,
) error {
	logical, err := repository.decoder.DecodeContext(ctx, message)
	if err != nil {
		return err
	}

	history := make([]HistoricalEvent, len(logical))
	for index, event := range logical {
		history[index] = HistoricalEvent{
			sourceVersion: message.StreamVersion(),
			segmentIndex:  event.segmentIndex,
			segmentCount:  event.segmentCount,
			event:         event.event,
		}
	}

	return lifecycle.ReconstituteNext(
		message.StreamVersion(),
		history,
		func(event DecodedEvent) error {
			return repository.apply(aggregate, event)
		},
	)
}

// Save atomically appends current pending events, acknowledges them after a
// confirmed commit, and then performs explicit live dispatch.
func (repository *AggregateRepository[ID, Aggregate]) Save(
	ctx context.Context,
	aggregate Aggregate,
) (SaveResult, error) {
	plan, err := repository.PrepareSave(ctx, aggregate)
	if err != nil {
		return SaveResult{}, err
	}
	if plan.Empty() {
		return SaveResult{outcome: CommitNotCommitted}, nil
	}

	messages, appendErr := repository.store.Append(
		ctx,
		plan.stream,
		plan.expected,
		plan.prepared,
	)
	outcome := CommitCommitted
	if appendErr != nil {
		outcome = AppendCommitOutcome(appendErr)
	}
	result := SaveResult{
		owner:    repository.saveOwner,
		outcome:  outcome,
		prepared: clonePendingMessages(plan.prepared),
		messages: cloneMessages(messages),
	}
	switch outcome {
	case CommitNotCommitted:
		result.messages = nil

		return result, appendErr
	case CommitUnknown:
		lifecycle := plan.changes.owner
		if err := lifecycle.MarkPersistenceUnknown(plan.changes); err != nil {
			lifecycle.poisoned = true

			return result, errors.Join(appendErr, err)
		}

		return result, appendErr
	case CommitCommitted:
	default:
		return result, ErrInvalidArgument
	}

	result, err = repository.ConfirmCommitted(aggregate, plan, messages)
	if err != nil {
		return result, errors.Join(appendErr, err)
	}
	result, dispatchErr := repository.DispatchCommitted(ctx, result)

	return result, errors.Join(appendErr, dispatchErr)
}

// PrepareSave validates and encodes the aggregate's current pending changes
// without performing I/O. The returned plan may be staged by a caller-owned
// transaction and then passed to ConfirmCommitted or MarkCommitUnknown.
func (repository *AggregateRepository[ID, Aggregate]) PrepareSave(
	ctx context.Context,
	aggregate Aggregate,
) (SavePlan, error) {
	if ctx == nil || repository == nil {
		return SavePlan{}, ErrInvalidArgument
	}
	lifecycle := repository.lifecycle(aggregate)
	if lifecycle == nil {
		return SavePlan{}, invalid("lifecycle", "accessor returned nil")
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		return SavePlan{}, err
	}
	if changes.Empty() {
		return SavePlan{owner: repository.saveOwner, changes: changes}, nil
	}
	if changes.Len() > MaxAppendMessages {
		return SavePlan{}, invalid("changes", "exceeds append batch limit")
	}

	stream, err := repository.streamID(repository.identify(aggregate))
	if err != nil {
		return SavePlan{}, err
	}
	prepared, err := repository.prepareMessages(ctx, aggregate, stream, changes)
	if err != nil {
		return SavePlan{}, err
	}
	expected := ExpectNewStream()
	if changes.BaseVersion() != 0 {
		expected = ExpectExactVersion(changes.BaseVersion())
	}

	return SavePlan{
		owner:    repository.saveOwner,
		changes:  changes,
		stream:   stream,
		expected: expected,
		prepared: clonePendingMessages(prepared),
	}, nil
}

// ConfirmCommitted acknowledges a plan after the caller has confirmed that
// its transaction committed. It does not dispatch the persisted messages.
func (repository *AggregateRepository[ID, Aggregate]) ConfirmCommitted(
	aggregate Aggregate,
	plan SavePlan,
	messages []Message,
) (SaveResult, error) {
	if repository == nil || plan.owner != repository.saveOwner {
		return SaveResult{}, ErrInvalidArgument
	}
	lifecycle := repository.lifecycle(aggregate)
	if lifecycle == nil {
		return SaveResult{}, invalid("lifecycle", "accessor returned nil")
	}
	if plan.changes.owner != lifecycle {
		return SaveResult{}, ErrInvalidChangeSet
	}
	if plan.Empty() {
		if len(messages) != 0 {
			return SaveResult{}, ErrInvalidArgument
		}

		return SaveResult{
			owner:   repository.saveOwner,
			outcome: CommitNotCommitted,
		}, nil
	}
	result := SaveResult{
		owner:    repository.saveOwner,
		outcome:  CommitCommitted,
		prepared: clonePendingMessages(plan.prepared),
		messages: cloneMessages(messages),
	}
	if err := lifecycle.Acknowledge(plan.changes, plan.prepared, messages); err != nil {
		return result, err
	}

	return result, nil
}

// MarkCommitUnknown poisons the aggregate after an ambiguous caller-owned
// transaction commit. The returned append error retains cause and classifies
// the outcome as unknown so the prepared message IDs can be reconciled.
func (repository *AggregateRepository[ID, Aggregate]) MarkCommitUnknown(
	aggregate Aggregate,
	plan SavePlan,
	messages []Message,
	cause error,
) (SaveResult, error) {
	if repository == nil ||
		plan.owner != repository.saveOwner ||
		plan.Empty() ||
		cause == nil {
		return SaveResult{}, ErrInvalidArgument
	}
	lifecycle := repository.lifecycle(aggregate)
	if lifecycle == nil {
		return SaveResult{}, invalid("lifecycle", "accessor returned nil")
	}
	if plan.changes.owner != lifecycle {
		return SaveResult{}, ErrInvalidChangeSet
	}
	if err := validatePlannedMessages(plan, messages); err != nil {
		return SaveResult{}, err
	}
	result := SaveResult{
		owner:    repository.saveOwner,
		outcome:  CommitUnknown,
		prepared: clonePendingMessages(plan.prepared),
		messages: cloneMessages(messages),
	}
	if err := lifecycle.MarkPersistenceUnknown(plan.changes); err != nil {
		lifecycle.poisoned = true

		return result, errors.Join(NewAppendError(CommitUnknown, cause), err)
	}

	return result, NewAppendError(CommitUnknown, cause)
}

func validatePlannedMessages(plan SavePlan, messages []Message) error {
	if len(messages) != len(plan.prepared) {
		return ErrPersistenceMismatch
	}
	for index, message := range messages {
		if !pendingMessagesEqual(plan.prepared[index], message.pending) ||
			message.StreamVersion() != plan.changes.base+uint64(index)+1 {
			return ErrPersistenceMismatch
		}
	}

	return nil
}

// DispatchCommitted performs live dispatch for a previously confirmed result.
// Persistence remains committed when dispatch fails.
func (repository *AggregateRepository[ID, Aggregate]) DispatchCommitted(
	ctx context.Context,
	result SaveResult,
) (SaveResult, error) {
	if ctx == nil || repository == nil ||
		result.owner != repository.saveOwner ||
		result.outcome != CommitCommitted ||
		result.dispatchAttempted {
		return result, ErrInvalidArgument
	}

	deliveries := make([]Delivery, len(result.messages))
	for index, message := range result.messages {
		deliveries[index] = Delivery{message: message, mode: DeliveryLive}
	}
	result.dispatchAttempted = true

	return result, repository.dispatcher.Dispatch(ctx, deliveries)
}

func (repository *AggregateRepository[ID, Aggregate]) streamID(
	id ID,
) (StreamID, error) {
	encoded, err := repository.encodeID(id)
	if err != nil {
		return StreamID{}, fmt.Errorf("encode aggregate identifier: %w", err)
	}

	return NewStreamID(repository.aggregateType, encoded)
}

func (repository *AggregateRepository[ID, Aggregate]) prepareMessages(
	ctx context.Context,
	aggregate Aggregate,
	stream StreamID,
	changes ChangeSet,
) ([]PendingMessage, error) {
	events := changes.Events()
	messages := make([]PendingMessage, len(events))
	for index, event := range events {
		encoded, err := encodePayload(ctx, repository.codec, event)
		if err != nil {
			return nil, err
		}
		if encoded.Name() != event.Name() || encoded.Version() != event.Version() {
			return nil, ErrPersistenceMismatch
		}
		id, err := repository.messageIDs.NewMessageID(ctx)
		if err != nil {
			return nil, err
		}
		messageContext := MessageContext{}
		if repository.messageContext != nil {
			messageContext, err = repository.messageContext(aggregate, event, index)
			if err != nil {
				return nil, fmt.Errorf("prepare message context: %w", err)
			}
		}
		pending, err := NewPendingMessage(PendingMessageInput{
			ID:            id.String(),
			Stream:        stream,
			Event:         encoded,
			Metadata:      messageContext.Metadata,
			RecordedAt:    repository.clock.Now(),
			CorrelationID: messageContext.CorrelationID,
			CausationID:   messageContext.CausationID,
			Tenant:        messageContext.Tenant,
			Partition:     messageContext.Partition,
		})
		if err != nil {
			return nil, err
		}
		messages[index], err = repository.decorators.Decorate(pending)
		if err != nil {
			return nil, err
		}
	}

	return messages, nil
}

// SaveResult reports the durable outcome independently from post-commit
// dispatch success.
type SaveResult struct {
	owner             *repositorySaveOwner
	outcome           CommitOutcome
	prepared          []PendingMessage
	messages          []Message
	dispatchAttempted bool
}

// Outcome returns the append durability classification.
func (result SaveResult) Outcome() CommitOutcome {
	return result.outcome
}

// Persisted reports whether append definitely committed.
func (result SaveResult) Persisted() bool {
	return result.outcome == CommitCommitted
}

// PreparedMessages returns the exact attempted envelopes, including message
// IDs required to reconcile an unknown commit outcome.
func (result SaveResult) PreparedMessages() []PendingMessage {
	return clonePendingMessages(result.prepared)
}

// Messages returns a defensive copy of store-returned persisted messages.
func (result SaveResult) Messages() []Message {
	return cloneMessages(result.messages)
}

// DispatchAttempted reports whether live post-commit dispatch began.
func (result SaveResult) DispatchAttempted() bool {
	return result.dispatchAttempted
}

func cloneMessages(messages []Message) []Message {
	if messages == nil {
		return nil
	}
	output := make([]Message, len(messages))
	copy(output, messages)

	return output
}

func clonePendingMessages(messages []PendingMessage) []PendingMessage {
	if messages == nil {
		return nil
	}
	output := make([]PendingMessage, len(messages))
	for index, message := range messages {
		output[index] = clonePendingMessage(message)
	}

	return output
}

var _ Repository[string, any] = (*AggregateRepository[string, any])(nil)
