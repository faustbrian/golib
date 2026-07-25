package postgres

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSlogObserverRecordsOnlyBoundedFields(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	observer := NewSlogObserver(logger)
	observer.Observe(context.Background(), Observation{
		Operation: OperationTransaction,
		Outcome:   OutcomeError,
		Duration:  25 * time.Millisecond,
		ErrorKind: ErrorUniqueViolation,
		SQLState:  "23505",
	})

	entry := output.String()
	for _, expected := range []string{"transaction", "error", "unique_violation", "23505"} {
		if !strings.Contains(entry, expected) {
			t.Fatalf("log entry %q does not contain %q", entry, expected)
		}
	}
	for _, forbidden := range []string{"sql", "arguments", "dsn", "detail", "hint"} {
		if strings.Contains(entry, `"`+forbidden+`"`) {
			t.Fatalf("log entry contains forbidden field %q: %s", forbidden, entry)
		}
	}
	if strings.Contains(entry, "pool.") {
		t.Fatalf("transaction log contains absent pool snapshot: %s", entry)
	}
}

func TestSlogObserverUsesDefaultLogger(t *testing.T) {
	t.Parallel()

	if observer := NewSlogObserver(nil); observer == nil {
		t.Fatal("NewSlogObserver(nil) = nil")
	}
}

func TestSlogObserverIncludesPresentPoolSnapshot(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer := NewSlogObserver(slog.New(slog.NewJSONHandler(
		&output,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)))
	observer.Observe(context.Background(), Observation{
		Operation:    OperationAcquire,
		Outcome:      OutcomeSuccess,
		Pool:         Stats{AcquiredConns: 1, IdleConns: 2, TotalConns: 3, MaxConns: 4},
		HasPoolStats: true,
	})
	entry := output.String()
	for _, expected := range []string{`"pool.acquired":1`, `"pool.idle":2`, `"pool.total":3`, `"pool.max":4`} {
		if !strings.Contains(entry, expected) {
			t.Fatalf("log entry %q does not contain %q", entry, expected)
		}
	}
}
