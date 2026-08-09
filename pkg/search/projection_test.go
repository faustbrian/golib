package search_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestProjectionEventProducesIdempotentExternallyVersionedOperations(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	event, err := search.NewProjectionEvent("tenant-a", "events", "event-1", 12, search.ProjectionUpsert, json.RawMessage(`{"status":"delivered"}`), "outbox-42", limits)
	if err != nil {
		t.Fatal(err)
	}
	first := event.Operation()
	second := event.Operation()
	if first.Action != search.ActionUpsert || first.Version != 12 || first.ID != "event-1" || string(first.Source) != string(second.Source) {
		t.Fatalf("Operation() = %#v/%#v", first, second)
	}
	first.Source[2] = 'X'
	if string(event.Operation().Source) != `{"status":"delivered"}` {
		t.Fatal("Operation() exposed event payload ownership")
	}

	deletion, err := search.NewProjectionEvent("tenant-a", "events", "event-1", 13, search.ProjectionDelete, nil, "outbox-43", limits)
	if err != nil || deletion.Operation().Action != search.ActionDelete {
		t.Fatalf("delete event = %#v/%v", deletion, err)
	}
	if _, err := search.NewProjectionEvent("tenant-a", "events", "event-1", 13, search.ProjectionUpsert, nil, "outbox-44", limits); !errors.Is(err, search.ErrInvalidProjectionEvent) {
		t.Fatalf("NewProjectionEvent() error = %v", err)
	}
}
