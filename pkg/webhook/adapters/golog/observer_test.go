package golog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	baselog "github.com/faustbrian/golib/pkg/log"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

func TestObserverWritesOnlyFixedSecretSafeAttributesThroughGoLog(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := baselog.JSON(&output, &slog.HandlerOptions{Level: slog.LevelDebug})
	observer, err := New(logger, slog.LevelInfo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer.Observe(context.Background(), webhook.Observation{
		Operation: webhook.OperationDeliveryAttempt, Outcome: webhook.OutcomeFailure,
		Reason: webhook.ReasonTransport, Duration: 1500 * time.Millisecond,
		Algorithm: webhook.SHA256, StatusCode: 503, Attempt: 2,
		Classification: webhook.FailureExhausted,
	})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v, output = %q", err, output.String())
	}
	if record["webhook.operation"] != "delivery_attempt" || record["webhook.outcome"] != "failure" ||
		record["http.response.status_class"] != "5xx" || record["webhook.duration_ms"] != float64(1500) {
		t.Fatalf("record = %#v", record)
	}
	for _, forbidden := range []string{"payload", "signature", "endpoint", "event_id", "key_id", "secret"} {
		if _, exists := record[forbidden]; exists {
			t.Fatalf("record contains forbidden field %q: %#v", forbidden, record)
		}
	}
}

func TestNewAndStatusClassValidation(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, slog.LevelInfo); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(nil) error = %v", err)
	}
	for status, want := range map[int]string{0: "none", 99: "none", 200: "2xx", 429: "4xx", 599: "5xx", 600: "none"} {
		if got := statusClass(status); got != want {
			t.Fatalf("statusClass(%d) = %q, want %q", status, got, want)
		}
	}
}
