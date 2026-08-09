package postgres

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzScanHistoryEvent(f *testing.F) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	f.Add(int64(1), int16(workflow.EventInstancePaused), int64(0), "", "", "", "", []byte(nil))
	f.Add(int64(-1), int16(0), int64(-1), "orders", "1", "bad", "step", []byte("data"))
	f.Fuzz(func(t *testing.T, sequence int64, kind int16, attempt int64, name, version, fingerprint, step string, data []byte) {
		if len(name) > 300 || len(version) > 300 || len(fingerprint) > 100 || len(step) > 300 || len(data) > workflow.MaxPayloadBytes+1 {
			t.Skip()
		}
		row := &fakeRow{values: []any{
			sequence, kind, now, name, version, fingerprint, "", step, attempt,
			"", (*time.Time)(nil), "", false, data,
		}}
		event, err := scanHistoryEvent(row, "instance-1")
		if err == nil {
			if event.Sequence() == 0 || event.InstanceID() != "instance-1" || event.OccurredAt().IsZero() {
				t.Fatalf("accepted corrupt event = %#v", event)
			}
		}
	})
}
