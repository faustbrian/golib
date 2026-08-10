package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestDeadLetterResolutionIsAuditedFencedAndIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	retry, err := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "dead-letter-command-1", WorkID: "work-1", Token: 3,
		Action: workflow.DeadLetterRetry, Actor: "operator-1", Reason: "payload-repaired",
		OccurredAt: now, RetryAt: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct retry resolution: %v", err)
	}
	if retry.CommandID() != "dead-letter-command-1" || retry.WorkID() != "work-1" ||
		retry.Token() != 3 || retry.Action() != workflow.DeadLetterRetry ||
		retry.Actor() != "operator-1" || retry.Reason() != "payload-repaired" ||
		retry.OccurredAt() != now || retry.RetryAt() != now.Add(time.Minute) ||
		retry.Deadline() != now.Add(time.Hour) || len(retry.Fingerprint()) != 64 || !retry.Valid() {
		t.Fatalf("retry resolution = %#v", retry)
	}
	same, err := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "dead-letter-command-1", WorkID: "work-1", Token: 3,
		Action: workflow.DeadLetterRetry, Actor: "operator-1", Reason: "payload-repaired",
		OccurredAt: now, RetryAt: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	})
	if err != nil || same.Fingerprint() != retry.Fingerprint() {
		t.Fatalf("same resolution fingerprint = %q, %v", same.Fingerprint(), err)
	}
	discard, err := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "dead-letter-command-2", WorkID: "work-2", Token: 4,
		Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "obsolete-work",
		OccurredAt: now,
	})
	if err != nil || !discard.RetryAt().IsZero() || !discard.Deadline().IsZero() || !discard.Valid() {
		t.Fatalf("discard resolution = %#v, %v", discard, err)
	}
	if discard.Fingerprint() == retry.Fingerprint() {
		t.Fatal("different resolution reused a fingerprint")
	}
}

func TestDeadLetterPageUsesStableFailureCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	first := mustDeadLetterRecord(t, now, "work-1", 2, 3)
	second := mustDeadLetterRecord(t, now, "work-2", 4, 5)
	query, err := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{Limit: 2})
	if err != nil {
		t.Fatalf("construct query: %v", err)
	}
	page, err := workflow.NewDeadLetterPage(query, []workflow.DeadLetterRecord{first, second}, true)
	if err != nil {
		t.Fatalf("construct page: %v", err)
	}
	items := page.Items()
	cursor := page.NextCursor()
	if len(items) != 2 || items[0].Work().ID() != "work-1" || items[0].Attempt() != 2 ||
		items[0].Token() != 3 || items[0].FailureCode() != "poison" ||
		items[0].FailedAt() != now || !page.HasMore() || cursor.FailedAt() != now ||
		cursor.WorkID() != "work-2" {
		t.Fatalf("dead-letter page = %#v cursor %#v", page, cursor)
	}
	work := items[0].Work()
	payload := work.Payload()
	payload[0] = 'X'
	items[0] = workflow.DeadLetterRecord{}
	if string(page.Items()[0].Work().Payload()) != "payload" {
		t.Fatal("dead-letter page exposed mutable work payload")
	}
	next, err := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{After: cursor, Limit: 2})
	if err != nil || next.After() != cursor || next.Limit() != 2 || !next.Valid() {
		t.Fatalf("next dead-letter query = %#v, %v", next, err)
	}
	reconstructed, err := workflow.NewDeadLetterCursor(workflow.DeadLetterCursorSpec{
		FailedAt: cursor.FailedAt(), WorkID: cursor.WorkID(),
	})
	if err != nil || reconstructed != cursor {
		t.Fatalf("reconstructed cursor = %#v, %v", reconstructed, err)
	}
}

func TestDeadLetterValuesRejectInvalidAndUnboundedInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	validRetry := workflow.DeadLetterResolutionSpec{
		CommandID: "command-1", WorkID: "work-1", Token: 1,
		Action: workflow.DeadLetterRetry, Actor: "operator-1", Reason: "retry-work",
		OccurredAt: now, RetryAt: now.Add(time.Second), Deadline: now.Add(time.Minute),
	}
	invalidResolutions := []workflow.DeadLetterResolutionSpec{
		{},
		{CommandID: " spaces ", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work", OccurredAt: now},
		{CommandID: "command-1", WorkID: " spaces ", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work", OccurredAt: now},
		{CommandID: "command-1", WorkID: "work-1", Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work", OccurredAt: now},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterResolutionAction(255), Actor: "operator-1", Reason: "discard-work", OccurredAt: now},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: " spaces ", Reason: "discard-work", OccurredAt: now},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: " spaces ", OccurredAt: now},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work"},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work", OccurredAt: now, RetryAt: now.Add(time.Second)},
		{CommandID: "command-1", WorkID: "work-1", Token: 1, Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work", OccurredAt: now, Deadline: now.Add(time.Minute)},
	}
	for _, spec := range invalidResolutions {
		if _, err := workflow.NewDeadLetterResolution(spec); !errors.Is(err, workflow.ErrInvalidOperatorCommand) {
			t.Fatalf("invalid resolution error = %v for %#v", err, spec)
		}
	}
	for _, mutate := range []func(*workflow.DeadLetterResolutionSpec){
		func(spec *workflow.DeadLetterResolutionSpec) { spec.RetryAt = spec.OccurredAt.Add(-time.Nanosecond) },
		func(spec *workflow.DeadLetterResolutionSpec) { spec.Deadline = spec.RetryAt },
	} {
		spec := validRetry
		mutate(&spec)
		if _, err := workflow.NewDeadLetterResolution(spec); !errors.Is(err, workflow.ErrInvalidOperatorCommand) {
			t.Fatalf("invalid retry resolution error = %v for %#v", err, spec)
		}
	}
	invalidCursors := []workflow.DeadLetterCursorSpec{
		{}, {FailedAt: now}, {WorkID: "work-1"}, {FailedAt: now, WorkID: " spaces "},
	}
	for _, spec := range invalidCursors {
		if _, err := workflow.NewDeadLetterCursor(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid cursor error = %v for %#v", err, spec)
		}
	}
	validRecord := mustDeadLetterRecord(t, now, "work-1", 1, 1)
	for _, spec := range []workflow.DeadLetterRecordSpec{
		{},
		{Work: validRecord.Work(), Token: 1, FailureCode: "poison", FailedAt: now},
		{Work: validRecord.Work(), Attempt: 1, FailureCode: "poison", FailedAt: now},
		{Work: validRecord.Work(), Attempt: 1, Token: 1, FailureCode: " spaces ", FailedAt: now},
		{Work: validRecord.Work(), Attempt: 1, Token: 1, FailureCode: "poison"},
	} {
		if _, err := workflow.NewDeadLetterRecord(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid record error = %v for %#v", err, spec)
		}
	}
	for _, spec := range []workflow.DeadLetterQuerySpec{
		{}, {Limit: workflow.MaxDeadLetterPageItems + 1},
	} {
		if _, err := workflow.NewDeadLetterQuery(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid query error = %v for %#v", err, spec)
		}
	}
	maximumQuery, err := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{
		Limit: workflow.MaxDeadLetterPageItems,
	})
	if err != nil || maximumQuery.Limit() != workflow.MaxDeadLetterPageItems {
		t.Fatalf("maximum query = %#v, %v", maximumQuery, err)
	}
	query, _ := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{Limit: 1})
	second := mustDeadLetterRecord(t, now, "work-2", 1, 1)
	for _, page := range []struct {
		items   []workflow.DeadLetterRecord
		hasMore bool
	}{
		{items: []workflow.DeadLetterRecord{validRecord, second}},
		{hasMore: true},
		{items: []workflow.DeadLetterRecord{{}}},
	} {
		if _, err := workflow.NewDeadLetterPage(query, page.items, page.hasMore); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid page error = %v for %#v", err, page)
		}
	}
	pageQuery, _ := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{Limit: 2})
	if _, err := workflow.NewDeadLetterPage(pageQuery, []workflow.DeadLetterRecord{second, validRecord}, false); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("out-of-order page error = %v", err)
	}
	if _, err := workflow.NewDeadLetterPage(pageQuery, []workflow.DeadLetterRecord{validRecord, validRecord}, false); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("duplicate page cursor error = %v", err)
	}
}

func mustDeadLetterRecord(
	t *testing.T,
	failedAt time.Time,
	workID string,
	attempt uint32,
	token uint64,
) workflow.DeadLetterRecord {
	t.Helper()
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: workID, Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: failedAt.Add(-time.Minute), Deadline: failedAt.Add(time.Hour),
		Payload: []byte("payload"), TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	record, err := workflow.NewDeadLetterRecord(workflow.DeadLetterRecordSpec{
		Work: work, Attempt: attempt, Token: token, FailureCode: "poison", FailedAt: failedAt,
	})
	if err != nil {
		t.Fatalf("construct dead letter: %v", err)
	}
	return record
}
