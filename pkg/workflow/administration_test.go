package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestInstanceListPageUsesStableCreationCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	reference := mustDefinitionReference(t, "orders", "1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	query, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: workflow.ListActiveInstances, Limit: 2,
	})
	if err != nil {
		t.Fatalf("construct instance list query: %v", err)
	}
	first, err := workflow.NewInstanceRecord(workflow.InstanceRecordSpec{
		InstanceID: "instance-1", Definition: reference, Sequence: 3,
		CreatedAt: now, UpdatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("construct first record: %v", err)
	}
	second, err := workflow.NewInstanceRecord(workflow.InstanceRecordSpec{
		InstanceID: "instance-2", Definition: reference, Sequence: 4,
		CreatedAt: now, UpdatedAt: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct second record: %v", err)
	}
	page, err := workflow.NewInstanceListPage(query, []workflow.InstanceRecord{first, second}, true)
	if err != nil {
		t.Fatalf("construct instance list page: %v", err)
	}
	items := page.Items()
	cursor := page.NextCursor()
	if len(items) != 2 || items[0].InstanceID() != "instance-1" ||
		items[0].Definition() != reference || items[0].Sequence() != 3 ||
		items[0].CreatedAt() != now || items[0].UpdatedAt() != now.Add(time.Second) ||
		!items[0].ArchivedAt().IsZero() || !page.HasMore() ||
		cursor.CreatedAt() != now || cursor.InstanceID() != "instance-2" {
		t.Fatalf("instance page = %#v cursor %#v", page, cursor)
	}
	items[0] = workflow.InstanceRecord{}
	if page.Items()[0].InstanceID() != "instance-1" {
		t.Fatal("instance page exposed mutable storage")
	}
	next, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: workflow.ListActiveInstances, After: cursor, Limit: 2,
	})
	if err != nil || next.After() != cursor || next.Selection() != workflow.ListActiveInstances ||
		next.Limit() != 2 || !next.Valid() {
		t.Fatalf("next instance query = %#v, %v", next, err)
	}
}

func TestInstanceListCursorCanBeReconstructedAtAPIBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	cursor, err := workflow.NewInstanceListCursor(workflow.InstanceListCursorSpec{
		CreatedAt: now, InstanceID: "instance-1",
	})
	if err != nil || cursor.CreatedAt() != now || cursor.InstanceID() != "instance-1" {
		t.Fatalf("instance cursor = %#v, %v", cursor, err)
	}
	for _, spec := range []workflow.InstanceListCursorSpec{
		{},
		{CreatedAt: now},
		{InstanceID: "instance-1"},
		{CreatedAt: now, InstanceID: " spaces "},
	} {
		if _, err := workflow.NewInstanceListCursor(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid cursor error = %v for %#v", err, spec)
		}
	}
}

func TestInstanceAdministrationValuesRejectInvalidAdapterData(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	reference := mustDefinitionReference(t, "orders", "1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	archived, err := workflow.NewInstanceRecord(workflow.InstanceRecordSpec{
		InstanceID: "instance-1", Definition: reference, Sequence: 1,
		CreatedAt: now, UpdatedAt: now, ArchivedAt: now.Add(time.Second),
	})
	if err != nil || archived.ArchivedAt() != now.Add(time.Second) {
		t.Fatalf("archived record = %#v, %v", archived, err)
	}
	invalidRecords := []workflow.InstanceRecordSpec{
		{},
		{InstanceID: " spaces ", Definition: reference, Sequence: 1, CreatedAt: now, UpdatedAt: now},
		{InstanceID: "instance-1", Sequence: 1, CreatedAt: now, UpdatedAt: now},
		{InstanceID: "instance-1", Definition: reference, CreatedAt: now, UpdatedAt: now},
		{InstanceID: "instance-1", Definition: reference, Sequence: 1, UpdatedAt: now},
		{InstanceID: "instance-1", Definition: reference, Sequence: 1, CreatedAt: now, UpdatedAt: now.Add(-time.Second)},
		{InstanceID: "instance-1", Definition: reference, Sequence: 1, CreatedAt: now, UpdatedAt: now, ArchivedAt: now.Add(-time.Second)},
	}
	for _, spec := range invalidRecords {
		if _, err := workflow.NewInstanceRecord(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid record error = %v for %#v", err, spec)
		}
	}
	invalidQueries := []workflow.InstanceListQuerySpec{
		{},
		{Selection: workflow.ListAllInstances},
		{Selection: workflow.InstanceListSelection(255), Limit: 1},
		{Selection: workflow.ListAllInstances, Limit: workflow.MaxInstanceListItems + 1},
	}
	for _, spec := range invalidQueries {
		if _, err := workflow.NewInstanceListQuery(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid list query error = %v for %#v", err, spec)
		}
	}
	query, _ := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{Selection: workflow.ListArchivedInstances, Limit: 1})
	if _, err := workflow.NewInstanceListPage(query, []workflow.InstanceRecord{archived, archived}, false); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("oversized page error = %v", err)
	}
	activeQuery, _ := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{Selection: workflow.ListActiveInstances, Limit: 1})
	if _, err := workflow.NewInstanceListPage(activeQuery, []workflow.InstanceRecord{archived}, false); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("selection mismatch error = %v", err)
	}
	archivedPage, err := workflow.NewInstanceListPage(query, []workflow.InstanceRecord{archived}, false)
	if err != nil || len(archivedPage.Items()) != 1 {
		t.Fatalf("archived page = %#v, %v", archivedPage, err)
	}
	allQuery, _ := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{Selection: workflow.ListAllInstances, Limit: 1})
	allPage, err := workflow.NewInstanceListPage(allQuery, []workflow.InstanceRecord{archived}, false)
	if err != nil || len(allPage.Items()) != 1 {
		t.Fatalf("all-instance page = %#v, %v", allPage, err)
	}
	if _, err := workflow.NewInstanceListPage(workflow.InstanceListQuery{}, nil, false); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("invalid page query error = %v", err)
	}
}

func TestTransitionReconciliationIdentityIsExplicit(t *testing.T) {
	t.Parallel()

	identity, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: "transition-1",
		Fingerprint:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil || identity.TransitionID() != "transition-1" ||
		identity.Fingerprint() != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		!identity.Valid() {
		t.Fatalf("transition reconciliation = %#v, %v", identity, err)
	}
	for _, spec := range []workflow.TransitionReconciliationSpec{
		{},
		{TransitionID: " spaces ", Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{TransitionID: "transition-1", Fingerprint: "short"},
	} {
		if _, err := workflow.NewTransitionReconciliation(spec); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
			t.Fatalf("invalid reconciliation error = %v for %#v", err, spec)
		}
	}
}
