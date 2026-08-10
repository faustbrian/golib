package workflow

import (
	"testing"
	"time"
)

func TestInstanceListCursorValiditySeparatesInitialAndContinuationCursors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		cursor InstanceListCursor
		valid  bool
	}{
		"initial":             {cursor: InstanceListCursor{}, valid: true},
		"initial with id":     {cursor: InstanceListCursor{instanceID: "instance-1"}},
		"continuation":        {cursor: InstanceListCursor{createdAt: now, instanceID: "instance-1"}, valid: true},
		"continuation no id":  {cursor: InstanceListCursor{createdAt: now}},
		"continuation bad id": {cursor: InstanceListCursor{createdAt: now, instanceID: " spaces "}},
	} {
		if got := test.cursor.valid(); got != test.valid {
			t.Fatalf("%s validity = %t, want %t", name, got, test.valid)
		}
	}
}

func TestInstanceListCursorOrderingSeparatesTimeAndIdentityBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC)
	cursor := InstanceListCursor{createdAt: now, instanceID: "instance-2"}
	for name, test := range map[string]struct {
		cursor     InstanceListCursor
		createdAt  time.Time
		instanceID string
		before     bool
	}{
		"initial":      {createdAt: now.Add(-time.Hour), instanceID: "instance-1", before: true},
		"later time":   {cursor: cursor, createdAt: now.Add(time.Second), instanceID: "instance-1", before: true},
		"earlier time": {cursor: cursor, createdAt: now.Add(-time.Second), instanceID: "instance-3"},
		"later id":     {cursor: cursor, createdAt: now, instanceID: "instance-3", before: true},
		"same id":      {cursor: cursor, createdAt: now, instanceID: "instance-2"},
		"earlier id":   {cursor: cursor, createdAt: now, instanceID: "instance-1"},
	} {
		if got := cursorBefore(test.cursor, test.createdAt, test.instanceID); got != test.before {
			t.Fatalf("%s ordering = %t, want %t", name, got, test.before)
		}
	}
}

func TestInstanceListQueryAcceptsTheExactPageLimit(t *testing.T) {
	t.Parallel()

	query := InstanceListQuery{selection: ListAllInstances, limit: MaxInstanceListItems}
	if !query.Valid() {
		t.Fatal("exact maximum instance page limit rejected")
	}
}
