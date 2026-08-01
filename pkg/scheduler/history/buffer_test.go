package history_test

import (
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/history"
)

func TestBufferRetainsOnlyBoundedRecentEvents(t *testing.T) {
	t.Parallel()

	buffer, err := history.NewBuffer(2)
	if err != nil {
		t.Fatalf("NewBuffer() error = %v", err)
	}
	for index, eventType := range []scheduler.EventType{
		scheduler.EventBefore,
		scheduler.EventSuccess,
		scheduler.EventCompleted,
	} {
		buffer.Observe(scheduler.Event{Type: eventType, At: time.Unix(int64(index), 0)})
	}
	entries := buffer.Entries()
	if len(entries) != 2 || entries[0].Type != scheduler.EventSuccess || entries[1].Type != scheduler.EventCompleted {
		t.Fatalf("Entries() = %v", entries)
	}
	entries[0].Owner = "changed"
	if buffer.Entries()[0].Owner == "changed" {
		t.Fatal("Entries() exposed mutable storage")
	}
}

func TestBufferRejectsInvalidCapacity(t *testing.T) {
	t.Parallel()

	if _, err := history.NewBuffer(0); err == nil {
		t.Fatal("NewBuffer(0) error = nil")
	}
	if _, err := history.NewBuffer(history.MaxCapacity + 1); err == nil {
		t.Fatal("NewBuffer(too large) error = nil")
	}
}

func TestBufferAcceptsExactCapacityLimits(t *testing.T) {
	t.Parallel()

	minimum, err := history.NewBuffer(1)
	if err != nil {
		t.Fatalf("NewBuffer(1) error = %v", err)
	}
	minimum.Observe(scheduler.Event{Type: scheduler.EventBefore})
	if entries := minimum.Entries(); len(entries) != 1 {
		t.Fatalf("minimum Entries() count = %d", len(entries))
	}

	maximum, err := history.NewBuffer(history.MaxCapacity)
	if err != nil {
		t.Fatalf("NewBuffer(MaxCapacity) error = %v", err)
	}
	maximum.Observe(scheduler.Event{Type: scheduler.EventCompleted})
	if entries := maximum.Entries(); len(entries) != 1 {
		t.Fatalf("maximum Entries() count = %d", len(entries))
	}
}
