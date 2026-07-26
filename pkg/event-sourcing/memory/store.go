// Package memory provides conformant in-memory stores for tests and local
// development.
package memory

import (
	"context"
	"sync"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// Store is a concurrency-safe in-memory event store.
//
// It provides process-local durability only but otherwise enforces the core
// append, ordering, ownership, and optimistic-concurrency contract.
// Its zero value is invalid; construct it with NewStore.
type Store struct {
	mutex          sync.RWMutex
	streams        map[eventsourcing.StreamID][]eventsourcing.Message
	messageIDs     map[string]struct{}
	globalMessages []eventsourcing.Message
	globalPosition eventsourcing.GlobalPosition
}

// NewStore constructs an empty in-memory event store.
func NewStore() *Store {
	return &Store{
		streams:        make(map[eventsourcing.StreamID][]eventsourcing.Message),
		messageIDs:     make(map[string]struct{}),
		globalMessages: make([]eventsourcing.Message, 0),
	}
}

// Append atomically stores one non-empty ordered batch in one stream.
func (store *Store) Append(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if store.streams == nil || store.messageIDs == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	if ctx == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, notCommitted(err)
	}
	if stream.IsZero() ||
		!expected.Valid() ||
		len(pending) == 0 ||
		len(pending) > eventsourcing.MaxAppendMessages {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}

	batchIDs := make(map[string]struct{}, len(pending))
	for _, message := range pending {
		id := message.ID().String()
		if id == "" || message.Stream() != stream || message.Event().IsZero() {
			return nil, notCommitted(eventsourcing.ErrInvalidArgument)
		}
		if _, duplicate := batchIDs[id]; duplicate {
			return nil, notCommitted(eventsourcing.ErrDuplicateMessageID)
		}
		batchIDs[id] = struct{}{}
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, notCommitted(err)
	}

	current := store.streams[stream]
	actual := uint64(len(current))
	if !matchesExpectedVersion(expected, actual) {
		return nil, notCommitted(&eventsourcing.ConcurrencyError{
			Stream:        stream,
			Expected:      expected,
			ActualVersion: actual,
		})
	}
	for id := range batchIDs {
		if _, duplicate := store.messageIDs[id]; duplicate {
			return nil, notCommitted(eventsourcing.ErrDuplicateMessageID)
		}
	}
	if uint64(len(pending)) > ^uint64(0)-uint64(store.globalPosition) {
		return nil, notCommitted(eventsourcing.ErrVersionOverflow)
	}

	stored := make([]eventsourcing.Message, len(pending))
	for index, message := range pending {
		if err := ctx.Err(); err != nil {
			return nil, notCommitted(err)
		}
		persisted, _ := eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending:        message,
			StreamVersion:  actual + uint64(index) + 1,
			GlobalPosition: store.globalPosition + eventsourcing.GlobalPosition(index) + 1,
		})
		stored[index] = persisted
	}

	store.streams[stream] = append(current, stored...)
	store.globalMessages = append(store.globalMessages, stored...)
	for id := range batchIDs {
		store.messageIDs[id] = struct{}{}
	}
	store.globalPosition += eventsourcing.GlobalPosition(len(stored))

	return append([]eventsourcing.Message(nil), stored...), nil
}

// ReadStream returns a bounded snapshot iterator over one existing stream.
func (store *Store) ReadStream(
	ctx context.Context,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	if store.streams == nil || store.messageIDs == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if ctx == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stream.IsZero() || !options.Valid() {
		return nil, eventsourcing.ErrInvalidArgument
	}

	store.mutex.RLock()
	defer store.mutex.RUnlock()

	messages, exists := store.streams[stream]
	if !exists {
		return nil, eventsourcing.ErrStreamNotFound
	}

	start := options.FromVersion() - 1
	if start >= uint64(len(messages)) {
		return &iterator{}, nil
	}
	end := uint64(len(messages))
	if options.ToVersion() != 0 && options.ToVersion() < end {
		end = options.ToVersion()
	}
	if limitEnd := start + uint64(options.Limit()); limitEnd < end {
		end = limitEnd
	}

	snapshot := append([]eventsourcing.Message(nil), messages[start:end]...)

	return &iterator{messages: snapshot}, nil
}

// ReadGlobal returns a bounded snapshot iterator in assigned global-position
// order. An empty store or a start beyond its end returns an empty iterator.
func (store *Store) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	if store.streams == nil || store.messageIDs == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if ctx == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !options.Valid() {
		return nil, eventsourcing.ErrInvalidArgument
	}

	store.mutex.RLock()
	defer store.mutex.RUnlock()

	start := uint64(options.FromPosition() - 1)
	if start >= uint64(len(store.globalMessages)) {
		return &iterator{}, nil
	}
	end := uint64(len(store.globalMessages))
	if options.ToPosition() != 0 &&
		uint64(options.ToPosition()) < end {
		end = uint64(options.ToPosition())
	}
	if limitEnd := start + uint64(options.Limit()); limitEnd < end {
		end = limitEnd
	}

	snapshot := append(
		[]eventsourcing.Message(nil),
		store.globalMessages[start:end]...,
	)

	return &iterator{messages: snapshot}, nil
}

func matchesExpectedVersion(
	expected eventsourcing.ExpectedVersion,
	actual uint64,
) bool {
	switch expected.Mode() {
	case eventsourcing.ExpectedVersionNew:
		return actual == 0
	case eventsourcing.ExpectedVersionExisting:
		return actual != 0
	case eventsourcing.ExpectedVersionExact:
		return actual == expected.Version()
	default:
		return true
	}
}

func notCommitted(cause error) error {
	return eventsourcing.NewAppendError(eventsourcing.CommitNotCommitted, cause)
}

type iterator struct {
	messages []eventsourcing.Message
	index    int
	current  eventsourcing.Message
	err      error
	closed   bool
	done     bool
}

func (iterator *iterator) Next(ctx context.Context) bool {
	iterator.current = eventsourcing.Message{}
	if iterator.closed {
		iterator.err = eventsourcing.ErrIteratorClosed

		return false
	}
	if iterator.done {
		return false
	}
	if ctx == nil {
		iterator.err = eventsourcing.ErrInvalidArgument
		iterator.done = true

		return false
	}
	if err := ctx.Err(); err != nil {
		iterator.err = err
		iterator.done = true

		return false
	}
	if iterator.index >= len(iterator.messages) {
		iterator.done = true

		return false
	}

	iterator.current = iterator.messages[iterator.index]
	iterator.index++

	return true
}

func (iterator *iterator) Message() eventsourcing.Message {
	return iterator.current
}

func (iterator *iterator) Err() error {
	return iterator.err
}

func (iterator *iterator) Close() error {
	if iterator.closed {
		return nil
	}

	iterator.closed = true
	iterator.done = true
	iterator.messages = nil
	iterator.current = eventsourcing.Message{}

	return nil
}

var _ eventsourcing.EventStore = (*Store)(nil)
var _ eventsourcing.GlobalReader = (*Store)(nil)
var _ eventsourcing.MessageIterator = (*iterator)(nil)
