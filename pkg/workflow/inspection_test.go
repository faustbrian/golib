package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

type inspectionHistoryReader struct {
	events       []workflow.HistoryEvent
	queries      []workflow.HistoryQuery
	err          error
	pageOverride *workflow.HistoryPage
}

func (reader *inspectionHistoryReader) History(_ context.Context, query workflow.HistoryQuery) (workflow.HistoryPage, error) {
	reader.queries = append(reader.queries, query)
	if reader.err != nil {
		return workflow.HistoryPage{}, reader.err
	}
	if reader.pageOverride != nil {
		return *reader.pageOverride, nil
	}
	start := int(query.AfterSequence())
	end := min(start+int(query.Limit()), len(reader.events))
	return workflow.NewHistoryPage(query, reader.events[start:end], end < len(reader.events))
}

func TestInspectInstanceReplaysBoundedStableHistoryPages(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	reader := &inspectionHistoryReader{events: []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(2 * time.Second)}),
	}}
	instance, err := workflow.InspectInstance(context.Background(), reader, registry, workflow.InstanceInspectionSpec{
		InstanceID: "instance-1", PageSize: 2, MaxEvents: 3,
	})
	if err != nil {
		t.Fatalf("inspect instance: %v", err)
	}
	if instance.ID() != "instance-1" || instance.Sequence() != 3 || instance.Status() != workflow.StatusRunning ||
		len(reader.queries) != 2 || reader.queries[1].AfterSequence() != 2 || reader.queries[1].Limit() != 1 {
		t.Fatalf("inspection = %#v queries %#v", instance, reader.queries)
	}
}

func TestExportHistoryStreamsBoundedOwnedPages(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	reader := &inspectionHistoryReader{events: []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(2 * time.Second)}),
	}}
	var batches [][]workflow.HistoryEvent
	err := workflow.ExportHistory(context.Background(), reader, workflow.HistoryExportSpec{
		InstanceID: "instance-1", PageSize: 2, MaxEvents: 3,
	}, func(_ context.Context, events []workflow.HistoryEvent) error {
		batches = append(batches, append([]workflow.HistoryEvent(nil), events...))
		events[0] = workflow.HistoryEvent{}
		return nil
	})
	if err != nil {
		t.Fatalf("export history: %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 ||
		batches[1][0].Sequence() != 3 {
		t.Fatalf("export batches = %#v", batches)
	}
}

func TestInspectionAndExportRejectInvalidOrUnboundedRequests(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}),
	}
	reader := &inspectionHistoryReader{events: events}
	if _, err := workflow.InspectInstance(context.Background(), reader, registry, workflow.InstanceInspectionSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 1,
	}); !errors.Is(err, workflow.ErrHistoryLimitExceeded) {
		t.Fatalf("inspection limit error = %v", err)
	}
	if err := workflow.ExportHistory(context.Background(), reader, workflow.HistoryExportSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 1,
	}, func(context.Context, []workflow.HistoryEvent) error { return nil }); !errors.Is(err, workflow.ErrHistoryLimitExceeded) {
		t.Fatalf("export limit error = %v", err)
	}
	sinkError := errors.New("sink failed")
	if err := workflow.ExportHistory(context.Background(), reader, workflow.HistoryExportSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 2,
	}, func(context.Context, []workflow.HistoryEvent) error { return sinkError }); !errors.Is(err, sinkError) {
		t.Fatalf("sink error = %v", err)
	}
	reader.err = errors.New("store failed")
	if _, err := workflow.InspectInstance(context.Background(), reader, registry, workflow.InstanceInspectionSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 2,
	}); !errors.Is(err, reader.err) {
		t.Fatalf("reader error = %v", err)
	}
	reader.err = nil
	otherQuery, _ := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{
		InstanceID: "instance-2", Limit: 1,
	})
	otherDefinition := mustDefinition(t, "other", "1")
	otherPage, err := workflow.NewHistoryPage(otherQuery, []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-2", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: otherDefinition.Reference(),
		}),
	}, false)
	if err != nil {
		t.Fatalf("construct mismatched page: %v", err)
	}
	reader.pageOverride = &otherPage
	if _, err := workflow.InspectInstance(context.Background(), reader, registry, workflow.InstanceInspectionSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 2,
	}); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("mismatched page error = %v", err)
	}
	reader.pageOverride = nil

	invalidInspection := []workflow.InstanceInspectionSpec{
		{},
		{InstanceID: "instance-1", PageSize: 0, MaxEvents: 1},
		{InstanceID: "instance-1", PageSize: workflow.MaxHistoryPageEvents + 1, MaxEvents: 1},
		{InstanceID: "instance-1", PageSize: 1, MaxEvents: 0},
		{InstanceID: "instance-1", PageSize: 1, MaxEvents: workflow.MaxInspectionHistoryEvents + 1},
	}
	for _, spec := range invalidInspection {
		if _, err := workflow.InspectInstance(context.Background(), reader, registry, spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid inspection error = %v for %#v", err, spec)
		}
	}
	validInspection := workflow.InstanceInspectionSpec{InstanceID: "instance-1", PageSize: 1, MaxEvents: 2}
	if _, err := workflow.InspectInstance(nil, reader, registry, validInspection); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := workflow.InspectInstance(context.Background(), nil, registry, validInspection); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("nil reader error = %v", err)
	}
	if _, err := workflow.InspectInstance(context.Background(), reader, nil, validInspection); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("nil registry error = %v", err)
	}
	validExport := workflow.HistoryExportSpec{InstanceID: "instance-1", PageSize: 1, MaxEvents: 2}
	validSink := func(context.Context, []workflow.HistoryEvent) error { return nil }
	for name, err := range map[string]error{
		"nil context": workflow.ExportHistory(nil, reader, validExport, validSink),
		"nil reader":  workflow.ExportHistory(context.Background(), nil, validExport, validSink),
		"nil sink":    workflow.ExportHistory(context.Background(), reader, validExport, nil),
		"invalid spec": workflow.ExportHistory(
			context.Background(), reader, workflow.HistoryExportSpec{}, validSink,
		),
	} {
		if !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("%s export error = %v", name, err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := workflow.InspectInstance(cancelled, reader, registry, workflow.InstanceInspectionSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 2,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection error = %v", err)
	}
}

func TestExportHistoryDoesNotInvokeSinkForEmptyHistory(t *testing.T) {
	t.Parallel()

	reader := &inspectionHistoryReader{}
	calls := 0
	err := workflow.ExportHistory(context.Background(), reader, workflow.HistoryExportSpec{
		InstanceID: "instance-1", PageSize: 1, MaxEvents: 1,
	}, func(context.Context, []workflow.HistoryEvent) error {
		calls++
		return nil
	})
	if err != nil || calls != 0 || len(reader.queries) != 1 {
		t.Fatalf("empty export error = %v, calls = %d, queries = %#v", err, calls, reader.queries)
	}
}
