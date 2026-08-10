package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHistoryTraversalValidationSeparatesEveryBound(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		instanceID string
		pageSize   uint32
		maxEvents  uint32
		valid      bool
	}{
		"exact maxima": {instanceID: "instance-1", pageSize: MaxHistoryPageEvents, maxEvents: MaxInspectionHistoryEvents, valid: true},
		"invalid id":   {instanceID: " spaces ", pageSize: 1, maxEvents: 1},
		"zero page":    {instanceID: "instance-1", maxEvents: 1},
		"large page":   {instanceID: "instance-1", pageSize: MaxHistoryPageEvents + 1, maxEvents: 1},
		"zero events":  {instanceID: "instance-1", pageSize: 1},
		"large events": {instanceID: "instance-1", pageSize: 1, maxEvents: MaxInspectionHistoryEvents + 1},
	} {
		if got := validHistoryTraversal(test.instanceID, test.pageSize, test.maxEvents); got != test.valid {
			t.Fatalf("%s validity = %t, want %t", name, got, test.valid)
		}
	}
}

func TestHistoryTraversalRejectsAValidPageWithTheWrongCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 14, 0, 0, 0, time.UTC)
	definition := mustInternalDefinition(t, "orders", "1")
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	if err != nil {
		t.Fatalf("construct history event: %v", err)
	}
	reader := internalHistoryReaderFunc(func(context.Context, HistoryQuery) (HistoryPage, error) {
		return HistoryPage{events: []HistoryEvent{event}, nextAfterSequence: 2}, nil
	})
	sinkCalled := false
	err = traverseHistory(context.Background(), reader, "instance-1", 1, 1,
		func(context.Context, []HistoryEvent) error {
			sinkCalled = true
			return nil
		})
	if !errors.Is(err, ErrInvalidStoreRequest) || sinkCalled {
		t.Fatalf("wrong cursor error = %v, sink called = %t", err, sinkCalled)
	}
}

type internalHistoryReaderFunc func(context.Context, HistoryQuery) (HistoryPage, error)

func (reader internalHistoryReaderFunc) History(ctx context.Context, query HistoryQuery) (HistoryPage, error) {
	return reader(ctx, query)
}
