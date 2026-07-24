package valkeystream

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	valkey "github.com/valkey-io/valkey-go"
)

var (
	// ErrManagementRecordsDisabled reports a worker without a native record transport.
	ErrManagementRecordsDisabled = fmt.Errorf(
		"valkeystream: management records disabled: %w",
		management.ErrUnsupportedCapability,
	)
	// ErrManagementRecordNotFound reports an unknown failure or dead-letter identifier.
	ErrManagementRecordNotFound = fmt.Errorf(
		"valkeystream: management record not found: %w",
		management.ErrRecordNotFound,
	)
)

var _ management.RecordReader = (*Worker)(nil)

const valkeyRecordSearchFactor int64 = 4

type nativeRecord struct {
	ID                   string
	OriginalID           string
	Body                 []byte
	Attempts             int64
	OccurredAt           time.Time
	EnvelopeVersion      uint16
	Source               string
	Group                string
	Classification       management.Classification
	FailureCode          string
	OriginalDeadLetterID string
	PriorDeadLetterID    string
	ReplayGeneration     uint32
}

type nativeRecordTransport interface {
	RecordFailure(
		context.Context, string, string, string,
		streamqueue.Delivery, streamqueue.FailureMetadata,
	) error
	AppendDeadLetter(
		context.Context, string, string, string,
		streamqueue.Delivery, streamqueue.FailureMetadata,
	) error
	ReadRecords(context.Context, string) ([]nativeRecord, error)
}

type nativeRecordPageTransport interface {
	ReadRecordPage(
		context.Context, string, string, int64, management.SortDirection,
	) ([]nativeRecord, error)
	ReadRecord(context.Context, string, string) (nativeRecord, bool, error)
}

// ListFailures returns bounded failed-attempt metadata without payload bytes.
func (w *Worker) ListFailures(
	ctx context.Context, request management.PageRequest,
) (management.RecordPage, error) {
	return w.listRecords(ctx, request, management.RecordFailure, w.opts.failureStream)
}

// ListDeadLetters returns bounded terminal-delivery metadata without payload bytes.
func (w *Worker) ListDeadLetters(
	ctx context.Context, request management.PageRequest,
) (management.RecordPage, error) {
	return w.listRecords(ctx, request, management.RecordDeadLetter, w.opts.deadLetterStream)
}

func (w *Worker) listRecords(
	ctx context.Context, request management.PageRequest, kind management.RecordKind, stream string,
) (management.RecordPage, error) {
	if err := request.Validate(); err != nil {
		return management.RecordPage{}, err
	}
	if pager, ok := w.transport.(nativeRecordPageTransport); ok {
		return w.listNativeRecordPage(ctx, request, kind, stream, pager)
	}
	records, err := w.nativeRecords(ctx, stream)
	if err != nil {
		return management.RecordPage{}, err
	}
	items, err := w.managementRecords(records, kind, management.PayloadHidden)
	if err != nil {
		return management.RecordPage{}, err
	}
	items = filterRecords(items, request.Search)
	sortRecords(items, request.Sort, request.Direction)
	offset, err := decodeRecordCursor(request.Cursor)
	if err != nil {
		return management.RecordPage{}, err
	}
	if offset >= len(items) {
		return management.RecordPage{Items: []management.JobRecord{}}, nil
	}
	end := min(offset+int(request.Limit), len(items))
	page := management.RecordPage{Items: items[offset:end]}
	if end < len(items) {
		page.NextCursor = encodeRecordCursor(end)
	}
	return page, nil
}

func (w *Worker) listNativeRecordPage(
	ctx context.Context, request management.PageRequest, kind management.RecordKind,
	stream string, transport nativeRecordPageTransport,
) (management.RecordPage, error) {
	if request.Sort != management.SortOccurredAt {
		return management.RecordPage{}, management.ErrInvalidFilter
	}
	cursor, err := decodeNativeRecordCursor(request.Cursor)
	if err != nil {
		return management.RecordPage{}, err
	}
	scanLimit := int64(request.Limit)
	if request.Search != "" {
		scanLimit *= valkeyRecordSearchFactor
	}
	records, err := transport.ReadRecordPage(
		ctx, stream, cursor, scanLimit, request.Direction,
	)
	if err != nil {
		return management.RecordPage{}, err
	}
	converted, err := w.managementRecords(records, kind, management.PayloadHidden)
	if err != nil {
		return management.RecordPage{}, err
	}
	items := make([]management.JobRecord, 0, request.Limit)
	for _, item := range converted {
		if len(filterRecords([]management.JobRecord{item}, request.Search)) == 1 &&
			len(items) < int(request.Limit) {
			items = append(items, item)
		}
	}
	page := management.RecordPage{Items: items}
	if len(records) == int(scanLimit) {
		page.NextCursor = encodeNativeRecordCursor(records[len(records)-1].ID)
	}
	return page, nil
}

// Inspect returns one record with no more payload disclosure than requested.
func (w *Worker) Inspect(
	ctx context.Context, request management.InspectRequest,
) (management.JobRecord, error) {
	if err := request.Validate(); err != nil {
		return management.JobRecord{}, err
	}
	stream := w.opts.failureStream
	if request.Kind == management.RecordDeadLetter {
		stream = w.opts.deadLetterStream
	}
	if transport, ok := w.transport.(nativeRecordPageTransport); ok {
		record, found, err := transport.ReadRecord(ctx, stream, request.ID)
		if err != nil {
			return management.JobRecord{}, err
		}
		if !found {
			return management.JobRecord{}, ErrManagementRecordNotFound
		}
		items, err := w.managementRecords(
			[]nativeRecord{record}, request.Kind, request.Visibility,
		)
		if err != nil {
			return management.JobRecord{}, err
		}
		return items[0], nil
	}
	records, err := w.nativeRecords(ctx, stream)
	if err != nil {
		return management.JobRecord{}, err
	}
	items, err := w.managementRecords(records, request.Kind, request.Visibility)
	if err != nil {
		return management.JobRecord{}, err
	}
	for _, item := range items {
		if item.ID == request.ID {
			return item, nil
		}
	}
	return management.JobRecord{}, ErrManagementRecordNotFound
}

func (w *Worker) nativeRecords(ctx context.Context, stream string) ([]nativeRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transport, ok := w.transport.(nativeRecordTransport)
	if !ok || stream == "" {
		return nil, ErrManagementRecordsDisabled
	}
	return transport.ReadRecords(ctx, stream)
}

func (w *Worker) managementRecords(
	records []nativeRecord, kind management.RecordKind, visibility management.PayloadVisibility,
) ([]management.JobRecord, error) {
	items := make([]management.JobRecord, 0, len(records))
	for _, record := range records {
		if record.Attempts < 1 || record.Attempts > math.MaxUint32 {
			return nil, fmt.Errorf("valkeystream: invalid management record attempts")
		}
		payload := management.Payload{Visibility: visibility, Size: int64(len(record.Body))}
		if visibility == management.PayloadRevealed {
			payload.ContentType = "application/octet-stream"
			payload.Data = append([]byte(nil), record.Body...)
		}
		failureCode := "handler_failed"
		if kind == management.RecordDeadLetter {
			failureCode = "terminal_delivery"
		}
		item := management.JobRecord{
			Kind: kind, ID: record.ID, Backend: w.BackendName(), Queue: w.opts.stream,
			OccurredAt: record.OccurredAt, Attempts: uint32(record.Attempts),
			FailureCode: failureCode, Payload: payload,
		}
		if record.EnvelopeVersion == management.CurrentEnvelopeVersion {
			item.EnvelopeVersion = record.EnvelopeVersion
			item.OriginalID = record.OriginalID
			item.SourceRecordID = record.OriginalID
			item.Stream = record.Source
			item.ConsumerGroup = record.Group
			item.Classification = record.Classification
			item.FailureCode = record.FailureCode
			item.OriginalDeadLetterID = record.OriginalDeadLetterID
			item.PriorDeadLetterID = record.PriorDeadLetterID
			item.ReplayGeneration = record.ReplayGeneration
			lastDeliveryAt := record.OccurredAt
			item.LastDeliveryAt = &lastDeliveryAt
			if w.opts.management != nil {
				item.WorkerVersion = w.opts.management.Version
			}
			if enqueuedAt, err := recordTime(record.OriginalID); err == nil {
				item.EnqueuedAt = &enqueuedAt
			}
			if kind == management.RecordDeadLetter {
				deadLetteredAt := record.OccurredAt
				item.DeadLetteredAt = &deadLetteredAt
			}
			streamqueue.ApplyMessageMetadata(
				&item, streamqueue.MessageMetadata(record.Body),
			)
		}
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("valkeystream: invalid management record: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func convertNativeRecords(entries []valkey.XRangeEntry) ([]nativeRecord, error) {
	records := make([]nativeRecord, 0, len(entries))
	for _, entry := range entries {
		body, bodyOK := entry.FieldValues[streamBodyField]
		originalID, idOK := entry.FieldValues[originalIDField]
		attemptText, attemptsOK := entry.FieldValues[deliveryAttemptsField]
		attempts, err := strconv.ParseInt(attemptText, 10, 64)
		occurredAt, timeErr := recordTime(entry.ID)
		if !bodyOK || !idOK || !attemptsOK || originalID == "" || err != nil || attempts < 1 || timeErr != nil {
			return nil, fmt.Errorf("valkeystream: malformed management record")
		}
		records = append(records, nativeRecord{
			ID: entry.ID, OriginalID: originalID, Body: []byte(body),
			Attempts: attempts, OccurredAt: occurredAt,
		})
		versionText, versioned := entry.FieldValues[envelopeVersionField]
		if !versioned {
			continue
		}
		version, versionErr := strconv.ParseUint(versionText, 10, 16)
		classification := management.Classification(entry.FieldValues[classificationField])
		failureCode := entry.FieldValues[failureCodeField]
		source := entry.FieldValues[sourceStreamField]
		group := entry.FieldValues[consumerGroupField]
		if versionErr != nil || uint16(version) != management.CurrentEnvelopeVersion ||
			management.NewFailure(classification, failureCode, nil).Validate() != nil ||
			source == "" || group == "" {
			return nil, fmt.Errorf("valkeystream: malformed management record")
		}
		current := &records[len(records)-1]
		current.EnvelopeVersion = uint16(version)
		current.Source = source
		current.Group = group
		current.Classification = classification
		current.FailureCode = failureCode
		original, prior, generation, lineageErr := parseReplayLineage(entry.FieldValues)
		if lineageErr != nil {
			return nil, fmt.Errorf("valkeystream: malformed replay lineage")
		}
		current.OriginalDeadLetterID = original
		current.PriorDeadLetterID = prior
		current.ReplayGeneration = generation
	}
	return records, nil
}

func recordTime(id string) (time.Time, error) {
	milliseconds, _, ok := strings.Cut(id, "-")
	if !ok {
		return time.Time{}, errors.New("invalid stream identifier")
	}
	value, err := strconv.ParseInt(milliseconds, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(value).UTC(), nil
}

func filterRecords(items []management.JobRecord, search string) []management.JobRecord {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return items
	}
	filtered := make([]management.JobRecord, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(item.ID + " " + item.Queue + " " + item.FailureCode)
		if strings.Contains(haystack, needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func sortRecords(items []management.JobRecord, field management.SortField, direction management.SortDirection) {
	sort.SliceStable(items, func(left, right int) bool {
		comparison := strings.Compare(items[left].ID, items[right].ID)
		switch field {
		case management.SortOccurredAt:
			comparison = items[left].OccurredAt.Compare(items[right].OccurredAt)
		case management.SortQueue:
			comparison = strings.Compare(items[left].Queue, items[right].Queue)
		case management.SortAttempts:
			comparison = int(items[left].Attempts) - int(items[right].Attempts)
		}
		if comparison == 0 {
			comparison = strings.Compare(items[left].ID, items[right].ID)
		}
		if direction == management.SortDescending {
			return comparison > 0
		}
		return comparison < 0
	})
}

func encodeRecordCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeRecordCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != cursor {
		return 0, fmt.Errorf(
			"valkeystream: invalid management record cursor: %w",
			management.ErrMalformedCursor,
		)
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf(
			"valkeystream: invalid management record cursor: %w",
			management.ErrMalformedCursor,
		)
	}
	return offset, nil
}

func encodeNativeRecordCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeNativeRecordCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != cursor ||
		len(decoded) == 0 || len(decoded) > management.MaxIdentityBytes {
		return "", management.ErrMalformedCursor
	}
	if _, err := recordTime(string(decoded)); err != nil {
		return "", management.ErrMalformedCursor
	}
	return string(decoded), nil
}
