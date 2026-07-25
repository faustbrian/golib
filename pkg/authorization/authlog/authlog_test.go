package authlog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func TestInstrumenterWritesBoundedAuditEvent(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	instrumenter, err := New(logger, slog.LevelInfo)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	next, finish := instrumenter.Start(ctx)
	if next != ctx {
		t.Error("Start() changed context")
	}
	finish(authorization.Event{
		Outcome: authorization.Allow, Reason: "acl-allow", Revision: 7,
		MatchedPolicyIDs:          []authorization.PolicyID{"entry-1"},
		MatchedPolicyIDsTruncated: true, TraceCount: 2, TraceTruncated: true,
		Duration: 1500 * time.Microsecond,
	})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output.String())
	}
	if record["msg"] != "authorization decision" || record["outcome"] != "allow" ||
		record["reason"] != "acl-allow" || record["revision"] != float64(7) ||
		record["duration_ms"] != 1.5 || record["failed"] != false {
		t.Errorf("audit record = %#v", record)
	}
}

func TestNewRejectsNilLogger(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, slog.LevelInfo); err == nil {
		t.Error("New(nil) error = nil")
	}
}
