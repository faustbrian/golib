// Package memory provides a bounded process-local audit sink for tests and
// development. It is not durable and must not be used as a compliance store.
package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/faustbrian/golib/pkg/audit"
)

// Config declares all process-local capacity limits. Its zero value is
// invalid so tests cannot accidentally create an unbounded store.
type Config struct {
	MaxRecords      int
	MaxBytes        int
	MaxBatchRecords int
}

type entry struct {
	record        audit.Record
	encoded       []byte
	acceptedOrder uint64
}

// Store is a bounded concurrency-safe in-memory sink. Its context-aware gate
// owns records, insertion order, and the byte counter; no caller callback runs
// while the gate is held.
type Store struct {
	gate      chan struct{}
	config    Config
	records   map[string]entry
	order     []string
	usedBytes int
	nextOrder uint64
}

var _ audit.Sink = (*Store)(nil)
var _ audit.Reader = (*Store)(nil)
var _ audit.Exporter = (*Store)(nil)

// New creates an empty bounded store.
func New(config Config) (*Store, error) {
	if config.MaxRecords <= 0 {
		return nil, fmt.Errorf("%w: memory limits must be positive and within core ceilings", audit.ErrInvalidArgument)
	}
	if config.MaxBytes <= 0 {
		return nil, fmt.Errorf("%w: memory limits must be positive and within core ceilings", audit.ErrInvalidArgument)
	}
	if config.MaxBatchRecords <= 0 {
		return nil, fmt.Errorf("%w: memory limits must be positive and within core ceilings", audit.ErrInvalidArgument)
	}
	if config.MaxBatchRecords > audit.MaxAppendBatchRecords {
		return nil, fmt.Errorf("%w: memory limits must be positive and within core ceilings", audit.ErrInvalidArgument)
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &Store{gate: gate, config: config, records: make(map[string]entry)}, nil
}

// Query returns records ordered by recording time then record ID. The cursor
// is exclusive and stable across insertion order.
func (store *Store) Query(ctx context.Context, query audit.Query) (audit.Page, error) {
	if store == nil || ctx == nil || !query.Valid() {
		return audit.Page{}, fmt.Errorf("%w: invalid memory query", audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.Page{}, err
	}

	if err := store.lock(ctx); err != nil {
		return audit.Page{}, err
	}
	candidates := make([]audit.Record, 0, len(store.records))
	snapshot := query.After().Snapshot()
	if snapshot == 0 {
		snapshot = store.nextOrder
	}
	for _, value := range store.records {
		if value.acceptedOrder <= snapshot {
			candidates = append(candidates, value.record)
		}
	}
	store.unlock()

	sort.Slice(candidates, func(left, right int) bool {
		leftTime, rightTime := candidates[left].RecordedAt(), candidates[right].RecordedAt()
		if leftTime.Equal(rightTime) {
			switch strings.Compare(candidates[left].ID(), candidates[right].ID()) {
			case -1:
				return true
			default:
				return false
			}
		}
		return leftTime.Before(rightTime)
	})
	limit := int(query.Limit())
	var matched []audit.Record
	for _, record := range candidates {
		if err := ctx.Err(); err != nil {
			return audit.Page{}, err
		}
		if !matches(query, record) {
			continue
		}
		matched = append(matched, record)
		if len(matched) > limit {
			return pageFromMatches(matched, limit, snapshot)
		}
	}
	return audit.Page{Records: matched}, nil
}

func pageFromMatches(matched []audit.Record, limit int, snapshot uint64) (audit.Page, error) {
	page := audit.Page{Records: matched[:limit]}
	last := page.Records[len(page.Records)-1]
	next, err := audit.NewSnapshotCursor(last.RecordedAt(), last.ID(), snapshot)
	if err != nil {
		return audit.Page{}, err
	}
	page.Next = next
	return page, nil
}

// Export invokes consume outside the store gate for at most Query.Limit
// records in stable query order.
func (store *Store) Export(ctx context.Context, query audit.Query, consume func(audit.Record) error) error {
	if consume == nil {
		return fmt.Errorf("%w: export callback", audit.ErrInvalidArgument)
	}
	page, err := store.Query(ctx, query)
	if err != nil {
		return err
	}
	for _, record := range page.Records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consumeSafely(consume, record); err != nil {
			return err
		}
	}
	return nil
}

func consumeSafely(consume func(audit.Record) error, record audit.Record) (err error) {
	defer func() {
		if recover() != nil {
			err = audit.ErrExportConsumerFailed
		}
	}()
	err = consume(record)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return audit.ErrExportConsumerFailed
	}
}

func matches(query audit.Query, record audit.Record) bool {
	if !query.Tenant().Includes(record.Context().TenantID()) {
		return false
	}
	if query.RecordID() != "" && record.ID() != query.RecordID() {
		return false
	}
	if !query.From().IsZero() && record.RecordedAt().Before(query.From()) {
		return false
	}
	if !query.Through().IsZero() && record.RecordedAt().After(query.Through()) {
		return false
	}
	after := query.After()
	if !after.IsZero() && (record.RecordedAt().Before(after.RecordedAt()) ||
		(record.RecordedAt().Equal(after.RecordedAt()) && record.ID() <= after.RecordID())) {
		return false
	}
	if query.ActorID() != "" && record.Actor().ID() != query.ActorID() {
		return false
	}
	if query.SubjectType() != "" && record.Subject().Type() != query.SubjectType() {
		return false
	}
	if query.SubjectID() != "" && record.Subject().ID() != query.SubjectID() {
		return false
	}
	if query.Action() != "" && record.Action() != query.Action() {
		return false
	}
	if query.CorrelationID() != "" && record.Context().CorrelationID() != query.CorrelationID() {
		return false
	}
	return query.Outcome() == 0 || record.Outcome() == query.Outcome()
}

// Append stores one record or reports an idempotent duplicate. Capacity and
// duplicate conflicts are confirmed rejections.
func (store *Store) Append(ctx context.Context, record audit.Record) (audit.AppendResult, error) {
	batch, err := store.AppendBatch(ctx, []audit.Record{record})
	if err != nil {
		return audit.AppendResult{}, err
	}
	return batch.Results[0], nil
}

// AppendBatch validates and commits the entire batch atomically in input
// order. A returned error means no member of that call was added.
func (store *Store) AppendBatch(ctx context.Context, records []audit.Record) (audit.BatchResult, error) {
	if store == nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
	}
	if ctx == nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	if len(records) == 0 || len(records) > store.config.MaxBatchRecords {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBatchTooLarge)
	}

	prepared := make([]entry, len(records))
	for index, record := range records {
		encoded, _ := audit.CanonicalJSON(record)
		if record.ID() == "" {
			return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
		}
		if !record.RedactionApplied() {
			return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrRedactionRequired)
		}
		if len(encoded) > store.config.MaxBytes {
			return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBackpressure)
		}
		prepared[index] = entry{record: record, encoded: encoded}
	}

	if err := store.lock(ctx); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	defer store.unlock()
	if err := ctx.Err(); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}

	results := make([]audit.AppendResult, len(records))
	pending := make(map[string]entry, len(records))
	addedBytes := 0
	addedRecords := 0
	for index, candidate := range prepared {
		id := candidate.record.ID()
		existing, exists := store.records[id]
		if !exists {
			existing, exists = pending[id]
		}
		if exists {
			if !bytes.Equal(existing.encoded, candidate.encoded) {
				return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrDuplicateConflict)
			}
			results[index] = audit.AppendResult{RecordID: id, Status: audit.AppendDuplicate}
		} else {
			pending[id] = candidate
			addedRecords++
			addedBytes = addedBytes + len(candidate.encoded)
			results[index] = audit.AppendResult{RecordID: id, Status: audit.AppendAccepted}
		}
	}
	if len(store.records)+addedRecords > store.config.MaxRecords {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBackpressure)
	}
	if store.usedBytes+addedBytes > store.config.MaxBytes {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBackpressure)
	}
	for index, candidate := range prepared {
		if results[index].Status == audit.AppendAccepted {
			id := candidate.record.ID()
			store.nextOrder++
			candidate.acceptedOrder = store.nextOrder
			store.records[id] = candidate
			store.order = append(store.order, id)
			store.usedBytes = store.usedBytes + len(candidate.encoded)
		}
	}
	return audit.BatchResult{Results: results}, nil
}

func (store *Store) lock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-store.gate:
		return nil
	}
}

func (store *Store) unlock() { store.gate <- struct{}{} }
