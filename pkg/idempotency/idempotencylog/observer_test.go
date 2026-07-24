package idempotencylog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencylog"
)

func TestObserverWritesOnlyBoundedFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	observer, err := idempotencylog.New(logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer.Observe(context.Background(), idempotency.Observation{
		Transition:  idempotency.TransitionAcquire,
		Outcome:     idempotency.OutcomeConflict,
		Reason:      idempotency.ReasonUnavailable,
		Durable:     true,
		Correlation: "restricted-correlation",
	})

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for name, want := range map[string]any{
		"msg":         "idempotency transition",
		"transition":  "acquire",
		"outcome":     "conflict",
		"reason":      "unavailable",
		"durable":     true,
		"correlation": "restricted-correlation",
	} {
		if got := record[name]; got != want {
			t.Errorf("record[%q] = %#v, want %#v", name, got, want)
		}
	}
	if len(record) != 8 { // time, level, message, and five bounded fields.
		t.Fatalf("record fields = %#v, want only slog metadata and bounded fields", record)
	}
}

func TestNewRejectsNilLogger(t *testing.T) {
	observer, err := idempotencylog.New(nil)
	if observer != nil || !errors.Is(err, idempotencylog.ErrNilLogger) {
		t.Fatalf("New(nil) = %#v, %v", observer, err)
	}
}
