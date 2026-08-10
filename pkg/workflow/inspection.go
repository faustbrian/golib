package workflow

import (
	"context"
	"errors"
)

const (
	// MaxInspectionHistoryEvents bounds one inspection or export operation.
	// Larger histories must be segmented with continue-as-new or read through
	// explicit History pages.
	MaxInspectionHistoryEvents uint32 = 100_000
)

var (
	// ErrHistoryLimitExceeded reports that inspection or export reached its
	// caller-selected bound while more durable history remained.
	ErrHistoryLimitExceeded = errors.New("workflow history traversal limit exceeded")
)

// HistoryReader is the narrow read contract consumed by inspection and export.
type HistoryReader interface {
	History(context.Context, HistoryQuery) (HistoryPage, error)
}

// InstanceInspectionSpec supplies one bounded deterministic replay request.
type InstanceInspectionSpec struct {
	InstanceID string
	PageSize   uint32
	MaxEvents  uint32
}

// HistoryExportSpec supplies one bounded streaming history-export request.
type HistoryExportSpec struct {
	InstanceID string
	PageSize   uint32
	MaxEvents  uint32
}

// HistoryExportSink consumes one owned stable forward page. Returning an error
// stops export without acknowledging or mutating durable state.
type HistoryExportSink func(context.Context, []HistoryEvent) error

// InspectInstance reconstructs one instance from bounded stable history pages.
// Replay decisions depend only on persisted history and the pinned registry.
func InspectInstance(
	ctx context.Context,
	reader HistoryReader,
	registry *Registry,
	spec InstanceInspectionSpec,
) (Instance, error) {
	if ctx == nil {
		return Instance{}, ErrInvalidStoreRequest
	}
	if reader == nil {
		return Instance{}, ErrInvalidStoreRequest
	}
	if registry == nil {
		return Instance{}, ErrInvalidStoreRequest
	}
	if !validHistoryTraversal(spec.InstanceID, spec.PageSize, spec.MaxEvents) {
		return Instance{}, ErrInvalidStoreRequest
	}
	events := make([]HistoryEvent, 0, min(spec.MaxEvents, spec.PageSize))
	err := traverseHistory(ctx, reader, spec.InstanceID, spec.PageSize, spec.MaxEvents,
		func(_ context.Context, page []HistoryEvent) error {
			events = append(events, page...)
			return nil
		})
	if err != nil {
		return Instance{}, err
	}
	return Replay(registry, events)
}

// ExportHistory streams owned stable pages without accumulating an unbounded
// in-memory export. It performs no external acknowledgement.
func ExportHistory(
	ctx context.Context,
	reader HistoryReader,
	spec HistoryExportSpec,
	sink HistoryExportSink,
) error {
	if ctx == nil {
		return ErrInvalidStoreRequest
	}
	if reader == nil {
		return ErrInvalidStoreRequest
	}
	if sink == nil {
		return ErrInvalidStoreRequest
	}
	if !validHistoryTraversal(spec.InstanceID, spec.PageSize, spec.MaxEvents) {
		return ErrInvalidStoreRequest
	}
	return traverseHistory(ctx, reader, spec.InstanceID, spec.PageSize, spec.MaxEvents, sink)
}

func validHistoryTraversal(instanceID string, pageSize, maxEvents uint32) bool {
	if !instanceIDPattern.MatchString(instanceID) {
		return false
	}
	if pageSize == 0 || pageSize > MaxHistoryPageEvents {
		return false
	}
	return maxEvents > 0 && maxEvents <= MaxInspectionHistoryEvents
}

func traverseHistory(
	ctx context.Context,
	reader HistoryReader,
	instanceID string,
	pageSize uint32,
	maxEvents uint32,
	sink HistoryExportSink,
) error {
	var after, count uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := uint64(maxEvents) - count
		limit := uint32(min(uint64(pageSize), remaining))
		query, _ := NewHistoryQuery(HistoryQuerySpec{
			InstanceID: instanceID, AfterSequence: after, Limit: limit,
		})
		page, err := reader.History(ctx, query)
		if err != nil {
			return err
		}
		events := page.Events()
		validated, err := NewHistoryPage(query, events, page.HasMore())
		if err != nil {
			return ErrInvalidStoreRequest
		}
		if validated.NextAfterSequence() != page.NextAfterSequence() {
			return ErrInvalidStoreRequest
		}
		if len(events) == 0 {
			return nil
		}
		if err := sink(ctx, events); err != nil {
			return err
		}
		count = count + uint64(len(events))
		after = page.NextAfterSequence()
		if !page.HasMore() {
			return nil
		}
		if count == uint64(maxEvents) {
			return ErrHistoryLimitExceeded
		}
	}
}
