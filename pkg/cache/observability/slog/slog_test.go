package slog_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	slogobserver "github.com/faustbrian/golib/pkg/cache/observability/slog"
)

func TestObserverLogsOnlyFixedRedactedAttributesByDefault(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With(
		"component", "cache",
	)
	observer, err := slogobserver.New(slogobserver.Config{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Observe(context.Background(), cache.Event{
		Operation: cache.OperationGet,
		Outcome:   cache.OutcomeHit,
		Duration:  1500 * time.Microsecond,
		Size:      42,
	}); err != nil {
		t.Fatal(err)
	}

	line := output.String()
	for _, want := range []string{`"operation":"get"`, `"outcome":"hit"`, `"duration_ms":1.5`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line lacks %s: %s", want, line)
		}
	}
	if strings.Contains(line, `"size"`) || strings.Contains(line, `"key"`) || strings.Contains(line, `"value"`) {
		t.Fatalf("default log exposed sensitive attributes: %s", line)
	}
}

func TestObserverCanIncludeSizeExplicitly(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer, err := slogobserver.New(slogobserver.Config{
		Logger:      slog.New(slog.NewJSONHandler(&output, nil)),
		IncludeSize: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Observe(context.Background(), cache.Event{Size: 42}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"size_bytes":42`) {
		t.Fatalf("explicit size missing: %s", output.String())
	}
}

func TestNewRejectsNilLogger(t *testing.T) {
	t.Parallel()

	if _, err := slogobserver.New(slogobserver.Config{}); err == nil {
		t.Fatal("expected nil logger error")
	}
}
