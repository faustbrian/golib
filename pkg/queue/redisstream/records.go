package redisdb

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
)

const redisRecordSearchFactor int64 = 4

var _ management.RecordReader = (*Worker)(nil)

// ListFailures returns a bounded page of package-managed Redis failure records.
func (w *Worker) ListFailures(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	return w.listRecords(ctx, request, management.RecordFailure, w.opts.failureStream)
}

// ListDeadLetters returns a bounded page of package-managed Redis dead letters.
func (w *Worker) ListDeadLetters(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	return w.listRecords(ctx, request, management.RecordDeadLetter, w.opts.deadLetterStream)
}

func (w *Worker) listRecords(
	ctx context.Context,
	request management.PageRequest,
	kind management.RecordKind,
	stream string,
) (management.RecordPage, error) {
	if err := request.Validate(); err != nil {
		return management.RecordPage{}, err
	}
	if request.Sort != management.SortOccurredAt {
		return management.RecordPage{}, management.ErrInvalidFilter
	}
	if w.rdb == nil {
		return management.RecordPage{}, management.ErrUnsupportedCapability
	}
	cursor, err := decodeRedisRecordCursor(request.Cursor)
	if err != nil {
		return management.RecordPage{}, err
	}
	scanLimit := int64(request.Limit)
	if request.Search != "" {
		scanLimit *= redisRecordSearchFactor
	}
	messages, err := w.readRecordPage(ctx, stream, cursor, scanLimit, request.Direction)
	if err != nil {
		return management.RecordPage{}, redisRecordReadError(
			"redisstream: read management records", err,
		)
	}
	items := make([]management.JobRecord, 0, request.Limit)
	for _, message := range messages {
		record, convertErr := redisManagementRecord(message, kind, management.PayloadHidden)
		if convertErr != nil {
			return management.RecordPage{}, convertErr
		}
		w.enrichManagementRecord(&record)
		if matchesRedisRecord(record, request.Search) && len(items) < int(request.Limit) {
			items = append(items, record)
		}
	}
	page := management.RecordPage{Items: items}
	if len(messages) == int(scanLimit) {
		page.NextCursor = encodeRedisRecordCursor(messages[len(messages)-1].ID)
	}
	return page, nil
}

func (w *Worker) readRecordPage(
	ctx context.Context,
	stream string,
	cursor string,
	limit int64,
	direction management.SortDirection,
) ([]redis.XMessage, error) {
	if direction == management.SortDescending {
		end := "+"
		if cursor != "" {
			end = "(" + cursor
		}
		return w.rdb.XRevRangeN(ctx, stream, end, "-", limit).Result()
	}
	start := "-"
	if cursor != "" {
		start = "(" + cursor
	}

	return w.rdb.XRangeN(ctx, stream, start, "+", limit).Result()
}

// Inspect returns one Redis failure or dead letter at explicit visibility.
func (w *Worker) Inspect(
	ctx context.Context,
	request management.InspectRequest,
) (management.JobRecord, error) {
	if err := request.Validate(); err != nil {
		return management.JobRecord{}, err
	}
	if w.rdb == nil {
		return management.JobRecord{}, management.ErrUnsupportedCapability
	}
	stream := w.opts.failureStream
	if request.Kind == management.RecordDeadLetter {
		stream = w.opts.deadLetterStream
	}
	messages, err := w.rdb.XRangeN(ctx, stream, request.ID, request.ID, 1).Result()
	if err != nil {
		return management.JobRecord{}, redisRecordReadError(
			"redisstream: inspect management record", err,
		)
	}
	if len(messages) != 1 || messages[0].ID != request.ID {
		return management.JobRecord{}, management.ErrRecordNotFound
	}

	record, err := redisManagementRecord(messages[0], request.Kind, request.Visibility)
	if err != nil {
		return management.JobRecord{}, err
	}
	w.enrichManagementRecord(&record)
	return record, nil
}

func (w *Worker) enrichManagementRecord(record *management.JobRecord) {
	if record.EnvelopeVersion != management.CurrentEnvelopeVersion {
		return
	}
	lastDeliveryAt := record.OccurredAt
	record.LastDeliveryAt = &lastDeliveryAt
	if w.opts.management != nil {
		record.WorkerVersion = w.opts.management.Version
	}
}

func redisRecordReadError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return errors.Join(
		management.ErrManagementUnavailable,
		safeerr.Wrap(operation, err),
	)
}

func redisManagementRecord(
	message redis.XMessage,
	kind management.RecordKind,
	visibility management.PayloadVisibility,
) (management.JobRecord, error) {
	body, bodyOK := redisRecordString(message.Values[streamBodyField])
	originalID, originalOK := redisRecordString(message.Values[originalIDField])
	attemptText, attemptsOK := redisRecordString(message.Values[deliveryAttemptsField])
	versionText, versionOK := redisRecordString(message.Values[envelopeVersionField])
	classificationText, classificationOK := redisRecordString(message.Values[classificationField])
	failureCode, failureOK := redisRecordString(message.Values[failureCodeField])
	stream, streamOK := redisRecordString(message.Values[sourceStreamField])
	group, groupOK := redisRecordString(message.Values[consumerGroupField])
	attempts, attemptsErr := strconv.ParseUint(attemptText, 10, 32)
	version, versionErr := strconv.ParseUint(versionText, 10, 16)
	occurredAt, timeErr := redisRecordTime(message.ID)
	classification := management.Classification(classificationText)
	if !bodyOK || !originalOK || !attemptsOK || !versionOK || !classificationOK ||
		!failureOK || !streamOK || !groupOK || originalID == "" || attemptsErr != nil ||
		attempts == 0 || versionErr != nil || uint16(version) != management.CurrentEnvelopeVersion ||
		management.NewFailure(classification, failureCode, nil).Validate() != nil ||
		stream == "" || group == "" || timeErr != nil {
		return management.JobRecord{}, errors.New("redisstream: malformed management record")
	}
	payload := management.Payload{Visibility: visibility, Size: int64(len(body))}
	if visibility == management.PayloadRevealed {
		payload.ContentType = "application/octet-stream"
		payload.Data = []byte(body)
	}
	record := management.JobRecord{
		Kind: kind, ID: message.ID, Backend: "redis-streams", Queue: stream,
		OccurredAt: occurredAt, Attempts: uint32(attempts), FailureCode: failureCode,
		Payload: payload, EnvelopeVersion: uint16(version), OriginalID: originalID,
		Stream: stream, ConsumerGroup: group, SourceRecordID: originalID,
		Classification: classification,
	}
	streamqueue.ApplyMessageMetadata(
		&record, streamqueue.MessageMetadata([]byte(body)),
	)
	lineage, lineageErr := redisLineageFromValues(message.Values)
	if lineageErr != nil {
		return management.JobRecord{}, errors.New("redisstream: malformed replay lineage")
	}
	if lineage.generation > 0 {
		record.OriginalDeadLetterID = lineage.original
		record.PriorDeadLetterID = lineage.prior
		record.ReplayGeneration = lineage.generation
	}
	if record.EnqueuedAt == nil {
		if enqueuedAt, enqueueErr := redisRecordTime(originalID); enqueueErr == nil {
			record.EnqueuedAt = &enqueuedAt
		}
	}
	if kind == management.RecordDeadLetter {
		deadLetteredAt := occurredAt
		record.DeadLetteredAt = &deadLetteredAt
	}
	if err := record.Validate(); err != nil {
		return management.JobRecord{}, fmt.Errorf("invalid Redis management record: %w", err)
	}

	return record, nil
}

func redisRecordString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

func redisRecordTime(id string) (time.Time, error) {
	milliseconds, _, ok := strings.Cut(id, "-")
	if !ok {
		return time.Time{}, errors.New("invalid Redis stream identifier")
	}
	value, err := strconv.ParseInt(milliseconds, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.UnixMilli(value).UTC(), nil
}

func matchesRedisRecord(record management.JobRecord, search string) bool {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(
		record.ID + " " + record.OriginalID + " " + record.Queue + " " +
			record.FailureCode + " " + string(record.Classification),
	)

	return strings.Contains(haystack, needle)
}

func encodeRedisRecordCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeRedisRecordCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != cursor ||
		len(decoded) == 0 || len(decoded) > management.MaxIdentityBytes {
		return "", management.ErrMalformedCursor
	}
	if _, err := redisRecordTime(string(decoded)); err != nil {
		return "", management.ErrMalformedCursor
	}

	return string(decoded), nil
}
