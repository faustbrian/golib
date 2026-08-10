package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/jackc/pgx/v5"
)

type iterator struct {
	rows                   pgx.Rows
	current                eventsourcing.Message
	err                    error
	expectedStreamVersion  uint64
	expectedGlobalPosition eventsourcing.GlobalPosition
	closed                 bool
	done                   bool
	checkStreamVersion     bool
	checkGlobalPosition    bool
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
		iterator.rows.Close()

		return false
	}
	if err := ctx.Err(); err != nil {
		iterator.err = err
		iterator.done = true
		iterator.rows.Close()

		return false
	}
	if !iterator.rows.Next() {
		iterator.done = true
		iterator.err = iterator.rows.Err()
		iterator.rows.Close()

		return false
	}

	message, err := scanMessage(iterator.rows)
	if err != nil {
		iterator.err = err
		iterator.done = true
		iterator.rows.Close()

		return false
	}
	if iterator.checkStreamVersion &&
		message.StreamVersion() != iterator.expectedStreamVersion {
		iterator.err = eventsourcing.ErrCorruptHistory
		iterator.done = true
		iterator.rows.Close()

		return false
	}
	position, hasPosition := message.GlobalPosition()
	if iterator.checkGlobalPosition &&
		(!hasPosition ||
			position != iterator.expectedGlobalPosition) {
		iterator.err = eventsourcing.ErrCorruptHistory
		iterator.done = true
		iterator.rows.Close()

		return false
	}
	iterator.current = message
	if iterator.expectedStreamVersion != ^uint64(0) {
		iterator.expectedStreamVersion++
	}
	if iterator.expectedGlobalPosition !=
		eventsourcing.GlobalPosition(^uint64(0)) {
		iterator.expectedGlobalPosition++
	}

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
	iterator.current = eventsourcing.Message{}
	iterator.rows.Close()
	if iterator.err == nil {
		iterator.err = iterator.rows.Err()
	}

	return iterator.err
}

type rowScanner interface {
	Scan(...any) error
}

func scanMessage(row rowScanner) (eventsourcing.Message, error) {
	var (
		position      int64
		messageID     string
		aggregateType string
		aggregateID   string
		streamVersion int64
		eventName     string
		schemaVersion int64
		contentType   string
		payload       []byte
		metadataJSON  []byte
		recordedAt    time.Time
		correlationID *string
		causationID   *string
		tenant        *string
		partition     *string
	)
	if err := row.Scan(
		&position,
		&messageID,
		&aggregateType,
		&aggregateID,
		&streamVersion,
		&eventName,
		&schemaVersion,
		&contentType,
		&payload,
		&metadataJSON,
		&recordedAt,
		&correlationID,
		&causationID,
		&tenant,
		&partition,
	); err != nil {
		return eventsourcing.Message{}, err
	}
	if position <= 0 ||
		streamVersion <= 0 ||
		schemaVersion <= 0 ||
		schemaVersion > math.MaxUint32 {
		return eventsourcing.Message{}, eventsourcing.ErrCorruptHistory
	}
	if len(metadataJSON) > maximumStoredMetadataJSONBytes {
		return eventsourcing.Message{}, eventsourcing.ErrCorruptHistory
	}

	metadata := make(map[string]string)
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return eventsourcing.Message{}, errors.Join(
			eventsourcing.ErrCorruptHistory,
			err,
		)
	}
	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		return eventsourcing.Message{}, corrupt(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        eventName,
			Version:     eventsourcing.SchemaVersion(schemaVersion),
			ContentType: contentType,
			Payload:     payload,
		},
	)
	if err != nil {
		return eventsourcing.Message{}, corrupt(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            messageID,
			Stream:        stream,
			Event:         event,
			Metadata:      metadata,
			RecordedAt:    recordedAt,
			CorrelationID: dereference(correlationID),
			CausationID:   dereference(causationID),
			Tenant:        dereference(tenant),
			Partition:     dereference(partition),
		},
	)
	if err != nil {
		return eventsourcing.Message{}, corrupt(err)
	}
	return eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  uint64(streamVersion),
		GlobalPosition: eventsourcing.GlobalPosition(position),
	})
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func corrupt(cause error) error {
	return errors.Join(eventsourcing.ErrCorruptHistory, cause)
}
