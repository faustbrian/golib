package nsq

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	nsqgo "github.com/nsqio/go-nsq"
)

const maxNSQDeadLetterEnvelopeBytes = job.DefaultMaxMessageBytes + 32_768

type nsqDeadLetterRecord struct {
	EnvelopeVersion uint16                    `json:"envelope_version"`
	Backend         string                    `json:"backend"`
	SourceTopic     string                    `json:"source_topic"`
	SourceChannel   string                    `json:"source_channel"`
	SourceID        string                    `json:"source_id"`
	EnqueuedAt      time.Time                 `json:"enqueued_at"`
	DeadLetteredAt  time.Time                 `json:"dead_lettered_at"`
	Attempts        uint16                    `json:"attempts"`
	Classification  management.Classification `json:"classification"`
	FailureCode     string                    `json:"failure_code"`
	PayloadSize     int64                     `json:"payload_size"`
	Payload         []byte                    `json:"payload,omitempty"`
	Metadata        *job.Metadata             `json:"metadata,omitempty"`
}

func (w *Worker) settleNSQFailure(message *nsqgo.Message, handlerErr error) error {
	if handlerErr == nil {
		message.Requeue(-1)
		return nil
	}
	resolution := management.ResolveFailure(handlerErr)
	classification := resolution.Classification
	if classification == management.ClassificationCanceled ||
		classification == management.ClassificationInfrastructure {
		message.Requeue(-1)
		return nil
	}
	attempts := message.Attempts
	if attempts == 0 {
		attempts = 1
	}
	if classification == management.ClassificationRetryable &&
		attempts < w.opts.maxDeliveryAttempts {
		message.Requeue(-1)
		return nil
	}
	code := "handler_failed"
	if resolution.Code != "" {
		code = resolution.Code
	}
	if classification == management.ClassificationRetryable {
		code = "attempts_exhausted"
	}
	record, err := w.encodeNSQDeadLetter(message, classification, code, attempts)
	if err != nil {
		message.Requeue(-1)
		return err
	}
	if w.publish == nil {
		message.Requeue(-1)
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeDeadLetterDestinationUnavailable,
			errors.New("NSQ producer unavailable"),
		)
	}
	if err := w.publish(w.opts.deadLetterTopic, record); err != nil {
		message.Requeue(-1)
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeDeadLetterDestinationUnavailable,
			err,
		)
	}
	message.Finish()

	return nil
}

func (w *Worker) encodeNSQDeadLetter(
	message *nsqgo.Message,
	classification management.Classification,
	failureCode string,
	attempts uint16,
) ([]byte, error) {
	payload := message.Body
	if len(payload) > job.DefaultMaxMessageBytes {
		payload = nil
	}
	record := nsqDeadLetterRecord{
		EnvelopeVersion: management.CurrentEnvelopeVersion,
		Backend:         "nsq", SourceTopic: w.opts.topic, SourceChannel: w.opts.channel,
		SourceID:       hex.EncodeToString(message.ID[:]),
		EnqueuedAt:     time.Unix(0, message.Timestamp).UTC(),
		DeadLetteredAt: time.Now().UTC(), Attempts: attempts,
		Classification: classification, FailureCode: failureCode,
		PayloadSize: int64(len(message.Body)), Payload: payload,
		Metadata: streamqueue.MessageMetadata(payload),
	}
	if err := record.validate(); err != nil {
		return nil, err
	}
	// Validation bounds every string and payload field, and this concrete
	// envelope contains no JSON-unsupported values. Marshal therefore cannot
	// fail for a valid record, while the decoder still enforces the wire limit.
	encoded, _ := json.Marshal(record)

	return encoded, nil
}

func decodeNSQDeadLetter(encoded []byte) (nsqDeadLetterRecord, error) {
	if len(encoded) == 0 || len(encoded) > maxNSQDeadLetterEnvelopeBytes {
		return nsqDeadLetterRecord{}, errors.New("decode NSQ dead letter: invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var record nsqDeadLetterRecord
	if err := decoder.Decode(&record); err != nil {
		return nsqDeadLetterRecord{}, fmt.Errorf("decode NSQ dead letter: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nsqDeadLetterRecord{}, errors.New("decode NSQ dead letter: trailing data")
	}
	if err := record.validate(); err != nil {
		return nsqDeadLetterRecord{}, err
	}

	return record, nil
}

// DecodeDeadLetter converts one package-owned NSQ terminal envelope into the
// backend-neutral management record without exposing NSQ client types.
func DecodeDeadLetter(
	encoded []byte,
	visibility management.PayloadVisibility,
) (management.JobRecord, error) {
	record, err := decodeNSQDeadLetter(encoded)
	if err != nil {
		return management.JobRecord{}, err
	}
	payload := management.Payload{
		Visibility: visibility, Size: record.PayloadSize,
	}
	if visibility == management.PayloadRevealed {
		payload.ContentType = "application/octet-stream"
		payload.Data = append([]byte(nil), record.Payload...)
	}
	result := management.JobRecord{
		Kind: management.RecordDeadLetter, ID: record.SourceID,
		Backend: record.Backend, Queue: record.SourceTopic,
		OccurredAt: record.DeadLetteredAt, Attempts: uint32(record.Attempts),
		FailureCode: record.FailureCode, Payload: payload,
		EnvelopeVersion: record.EnvelopeVersion, OriginalID: record.SourceID,
		Topic: record.SourceTopic, ConsumerGroup: record.SourceChannel,
		SourceRecordID: record.SourceID, EnqueuedAt: &record.EnqueuedAt,
		DeadLetteredAt: &record.DeadLetteredAt,
		Classification: record.Classification,
	}
	streamqueue.ApplyMessageMetadata(&result, record.Metadata)
	if err := result.Validate(); err != nil {
		return management.JobRecord{}, fmt.Errorf("decode NSQ dead letter: %w", err)
	}

	return result, nil
}

func (r nsqDeadLetterRecord) validate() error {
	_, idErr := hex.DecodeString(r.SourceID)
	if r.EnvelopeVersion != management.CurrentEnvelopeVersion || r.Backend != "nsq" ||
		r.SourceTopic == "" || r.SourceChannel == "" || len(r.SourceID) != 32 ||
		idErr != nil ||
		r.EnqueuedAt.IsZero() || r.DeadLetteredAt.IsZero() || r.Attempts == 0 ||
		r.DeadLetteredAt.Before(r.EnqueuedAt) ||
		r.PayloadSize < 0 || r.PayloadSize > job.DefaultMaxMessageBytes && len(r.Payload) != 0 ||
		len(r.Payload) > job.DefaultMaxMessageBytes ||
		management.NewFailure(r.Classification, r.FailureCode, nil).Validate() != nil {
		return errors.New("invalid NSQ dead-letter record")
	}
	if r.Metadata != nil && r.Metadata.Validate() != nil {
		return errors.New("invalid NSQ dead-letter record")
	}

	return nil
}
