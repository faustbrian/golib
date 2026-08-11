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

func TestProjectionEventRejectsInvalidUTF8Identities(t *testing.T) {
	t.Parallel()

	invalid := string([]byte{0xff})
	for _, values := range [][4]string{
		{invalid, "events", "event-1", "outbox-1"},
		{"tenant-a", invalid, "event-1", "outbox-1"},
		{"tenant-a", "events", invalid, "outbox-1"},
		{"tenant-a", "events", "event-1", invalid},
	} {
		if _, err := search.NewProjectionEvent(values[0], values[1], values[2], 1,
			search.ProjectionDelete, nil, values[3], search.DefaultLimits()); !errors.Is(err, search.ErrInvalidProjectionEvent) {
			t.Fatalf("NewProjectionEvent(%q, %q, %q, %q) error = %v, want ErrInvalidProjectionEvent",
				values[0], values[1], values[2], values[3], err)
		}
	}
	if _, err := search.NewProjectionEvent("tenant-a", "events", "event-1", 1,
		search.ProjectionDelete, nil, "outbox-1", search.Limits{}); !errors.Is(err, search.ErrInvalidProjectionEvent) {
		t.Fatalf("NewProjectionEvent() invalid-limits error = %v, want ErrInvalidProjectionEvent", err)
	}
}
