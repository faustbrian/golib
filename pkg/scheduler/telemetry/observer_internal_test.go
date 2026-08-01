package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestObserverContextAndLogLevelContracts(t *testing.T) {
	t.Parallel()

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("key"), "value")
	if eventContext(ctx) != ctx {
		t.Fatal("eventContext() replaced a supplied context")
	}
	if eventContext(nil) == nil {
		t.Fatal("eventContext(nil) returned nil")
	}

	if got := logLevel(scheduler.Event{
		Err: errors.New("failed"), Result: scheduler.ResultFailed,
	}); got != slog.LevelError {
		t.Fatalf("logLevel(failure) = %v", got)
	}
	for name, event := range map[string]scheduler.Event{
		"error without failed result": {
			Err: errors.New("skipped"), Result: scheduler.ResultSkipped,
		},
		"failed result without error": {Result: scheduler.ResultFailed},
	} {
		if got := logLevel(event); got != slog.LevelInfo {
			t.Fatalf("logLevel(%s) = %v", name, got)
		}
	}
}
