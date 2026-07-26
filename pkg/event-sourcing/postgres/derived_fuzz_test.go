package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func FuzzScanSnapshot(f *testing.F) {
	f.Add(
		"account",
		"account-1",
		int64(1),
		int64(1),
		[]byte(`{"owner":"Ada"}`),
		[]byte(`{"codec":"json"}`),
		int64(1),
	)
	f.Add(
		"",
		"",
		int64(0),
		int64(0),
		[]byte{},
		[]byte(`[]`),
		int64(0),
	)

	f.Fuzz(func(
		t *testing.T,
		aggregateType string,
		aggregateID string,
		aggregateVersion int64,
		schemaVersion int64,
		state []byte,
		metadata []byte,
		createdAtSeconds int64,
	) {
		t.Helper()

		_, _ = scanSnapshot(fakeRow{scan: scanValues(
			aggregateType,
			aggregateID,
			aggregateVersion,
			schemaVersion,
			state,
			metadata,
			time.Unix(createdAtSeconds, 0).UTC(),
		)})
	})
}

func FuzzProjectionStatus(f *testing.F) {
	f.Add(int16(projectionStateRunning), int64(1), true)
	f.Add(int16(0), int64(0), false)
	f.Add(int16(projectionStatePaused), int64(-1), true)

	f.Fuzz(func(
		t *testing.T,
		state int16,
		checkpoint int64,
		valid bool,
	) {
		t.Helper()

		_, _ = newProjectionStatus(
			state,
			pgtype.Int8{Int64: checkpoint, Valid: valid},
		)
	})
}
