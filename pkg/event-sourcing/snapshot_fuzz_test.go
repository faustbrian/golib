package eventsourcing_test

import (
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzSnapshotValidation(fuzz *testing.F) {
	fuzz.Add(
		"account",
		"account-42",
		uint64(7),
		uint32(2),
		[]byte(`{"owner":"Ada"}`),
		"codec",
		"json",
		int64(1),
	)
	fuzz.Add("", "", uint64(0), uint32(0), []byte{}, "", "", int64(0))

	fuzz.Fuzz(func(
		t *testing.T,
		aggregateType string,
		aggregateID string,
		aggregateVersion uint64,
		schemaVersion uint32,
		state []byte,
		metadataKey string,
		metadataValue string,
		createdAtMicro int64,
	) {
		stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
		if err != nil {
			return
		}
		metadata := map[string]string{}
		if metadataKey != "" || metadataValue != "" {
			metadata[metadataKey] = metadataValue
		}
		snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
			Stream:           stream,
			AggregateVersion: aggregateVersion,
			SchemaVersion:    eventsourcing.SchemaVersion(schemaVersion),
			State:            state,
			Metadata:         metadata,
			CreatedAt:        time.UnixMicro(createdAtMicro),
		})
		if err != nil {
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewSnapshot() error = %v", err)
			}

			return
		}
		if snapshot.IsZero() ||
			snapshot.Stream() != stream ||
			snapshot.AggregateVersion() != aggregateVersion ||
			snapshot.SchemaVersion() != eventsourcing.SchemaVersion(schemaVersion) ||
			!snapshot.CreatedAt().Equal(time.UnixMicro(createdAtMicro).UTC()) {
			t.Fatalf("snapshot identity changed")
		}
		if len(state) != 0 {
			state[0] ^= 0xff
			if snapshot.State()[0] == state[0] {
				t.Fatal("snapshot aliases fuzz input state")
			}
		}
		if len(metadata) != 0 {
			metadata[metadataKey] = "changed"
			if snapshot.Metadata()[metadataKey] == "changed" {
				t.Fatal("snapshot aliases fuzz input metadata")
			}
		}
	})
}
