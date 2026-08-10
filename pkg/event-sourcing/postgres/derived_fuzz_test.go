package postgres

import (
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
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
	f.Add(
		"account",
		"account-oversized-metadata",
		int64(1),
		int64(1),
		[]byte(`{"owner":"Ada"}`),
		oversizedStoredMetadataJSON(f),
		int64(1),
	)
	f.Add(
		"account",
		"account-maximum-metadata",
		int64(1),
		int64(1),
		[]byte(`{"owner":"Ada"}`),
		maximumStoredMetadataJSON(f),
		int64(1),
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
		if len(aggregateType)+len(aggregateID)+len(state)+len(metadata) >
			10<<20 {
			return
		}

		_, err := scanSnapshot(fakeRow{scan: scanValues(
			aggregateType,
			aggregateID,
			aggregateVersion,
			schemaVersion,
			state,
			metadata,
			time.Unix(createdAtSeconds, 0).UTC(),
		)})
		if len(metadata) > maximumStoredMetadataJSONBytes &&
			err != eventsourcing.ErrSnapshotCorrupt {
			t.Fatalf("oversized metadata error = %v", err)
		}
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
