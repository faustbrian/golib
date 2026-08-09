package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestHistoryPageProvidesStableBoundedPagination(t *testing.T) {
	t.Parallel()

	query, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{
		InstanceID: "instance-1", AfterSequence: 1, Limit: 2,
	})
	if err != nil {
		t.Fatalf("construct history query: %v", err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(time.Second)}),
	}
	page, err := workflow.NewHistoryPage(query, events, true)
	if err != nil {
		t.Fatalf("construct history page: %v", err)
	}
	if query.InstanceID() != "instance-1" || query.AfterSequence() != 1 || query.Limit() != 2 {
		t.Fatal("history query was not preserved")
	}
	if !query.Valid() || (workflow.HistoryQuery{}).Valid() {
		t.Fatal("history query validity was ambiguous")
	}
	if page.NextAfterSequence() != 3 || !page.HasMore() || len(page.Events()) != 2 {
		t.Fatal("history page cursor was not preserved")
	}
	got := page.Events()
	got[0] = workflow.HistoryEvent{}
	if page.Events()[0].Sequence() != 2 {
		t.Fatal("history page returned caller-mutable events")
	}
}

func TestStoreCommitErrorMakesDurabilityExplicit(t *testing.T) {
	t.Parallel()

	cause := errors.New("driver detail containing secret")
	for _, outcome := range []workflow.StoreCommitOutcome{
		workflow.StoreCommitUnknown,
		workflow.StoreCommitNotCommitted,
		workflow.StoreCommitCommitted,
	} {
		err := workflow.NewStoreCommitError(outcome, cause)
		if !errors.Is(err, cause) || workflow.StoreCommitOutcomeOf(err) != outcome {
			t.Fatalf("commit outcome %d was not preserved", outcome)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("commit error exposed driver details")
		}
	}
	if workflow.StoreCommitOutcomeOf(errors.New("unclassified")) != workflow.StoreCommitUnknown {
		t.Fatal("unclassified store error was not conservative")
	}
	if err := workflow.NewStoreCommitError(workflow.StoreCommitUnknown, nil); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("nil cause error = %v", err)
	}
	if err := workflow.NewStoreCommitError(workflow.StoreCommitOutcome(99), cause); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("unknown outcome error = %v", err)
	}
}

func TestHistoryQueryRejectsInvalidOrUnboundedCursors(t *testing.T) {
	t.Parallel()

	tests := []workflow.HistoryQuerySpec{
		{Limit: 1},
		{InstanceID: "instance-1"},
		{InstanceID: "instance-1", Limit: workflow.MaxHistoryPageEvents + 1},
		{InstanceID: "instance-1", AfterSequence: ^uint64(0), Limit: 1},
	}
	for _, spec := range tests {
		if _, err := workflow.NewHistoryQuery(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("query %#v error = %v", spec, err)
		}
	}
	if _, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{
		InstanceID: "instance-1", Limit: workflow.MaxHistoryPageEvents,
	}); err != nil {
		t.Fatalf("exact history limit rejected: %v", err)
	}
}

func TestHistoryPageRejectsUnstableAdapterOutput(t *testing.T) {
	t.Parallel()

	query, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{InstanceID: "instance-1", AfterSequence: 1, Limit: 2})
	if err != nil {
		t.Fatalf("construct query: %v", err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	first := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now})
	second := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(time.Second)})
	tests := []struct {
		query   workflow.HistoryQuery
		events  []workflow.HistoryEvent
		hasMore bool
	}{
		{},
		{events: []workflow.HistoryEvent{first}},
		{query: query, events: []workflow.HistoryEvent{first, second, second}},
		{query: query, events: []workflow.HistoryEvent{first}, hasMore: true},
		{query: query, events: []workflow.HistoryEvent{{}}},
		{query: query, events: []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-2", Kind: workflow.EventInstancePaused, OccurredAt: now})}},
		{query: query, events: []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now})}},
	}
	for index, test := range tests {
		if _, err := workflow.NewHistoryPage(test.query, test.events, test.hasMore); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
	empty, err := workflow.NewHistoryPage(query, nil, false)
	if err != nil {
		t.Fatalf("construct empty page: %v", err)
	}
	if empty.NextAfterSequence() != 1 || empty.HasMore() || empty.Events() != nil {
		t.Fatal("empty history page changed its stable cursor")
	}
}
