package eventsourcing_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestSnapshotOwnsValidatedDerivedState(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"owner":"Ada"}`)
	metadata := map[string]string{"codec": "json"}
	createdAt := time.Date(
		2026,
		time.July,
		25,
		12,
		0,
		0,
		123456789,
		time.FixedZone("test", 2*60*60),
	)
	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: 7,
		SchemaVersion:    2,
		State:            state,
		Metadata:         metadata,
		CreatedAt:        createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	state[0] = '!'
	metadata["codec"] = "changed"

	if snapshot.Stream() != stream ||
		snapshot.AggregateVersion() != 7 ||
		snapshot.SchemaVersion() != 2 ||
		string(snapshot.State()) != `{"owner":"Ada"}` ||
		snapshot.Metadata()["codec"] != "json" ||
		snapshot.CreatedAt() != time.Date(
			2026,
			time.July,
			25,
			10,
			0,
			0,
			123456000,
			time.UTC,
		) ||
		snapshot.IsZero() {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	returnedState := snapshot.State()
	returnedState[0] = '!'
	returnedMetadata := snapshot.Metadata()
	returnedMetadata["codec"] = "changed"
	if string(snapshot.State()) != `{"owner":"Ada"}` ||
		snapshot.Metadata()["codec"] != "json" {
		t.Fatal("snapshot getters alias owned state")
	}

	equal, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: 7,
		SchemaVersion:    2,
		State:            []byte(`{"owner":"Ada"}`),
		Metadata:         map[string]string{"codec": "json"},
		CreatedAt:        createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Equal(equal) || !equal.Equal(snapshot) {
		t.Fatal("equal snapshots differ")
	}
}

func TestSnapshotValidationAndEquality(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	valid := eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: 1,
		SchemaVersion:    1,
		State:            []byte(`{}`),
		CreatedAt:        time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
	}
	cases := map[string]eventsourcing.SnapshotInput{
		"stream":            valid,
		"aggregate version": valid,
		"schema version":    valid,
		"empty state":       valid,
		"oversized state":   valid,
		"metadata":          valid,
		"created at":        valid,
	}
	value := cases["stream"]
	value.Stream = eventsourcing.StreamID{}
	cases["stream"] = value
	value = cases["aggregate version"]
	value.AggregateVersion = 0
	cases["aggregate version"] = value
	value = cases["schema version"]
	value.SchemaVersion = 0
	cases["schema version"] = value
	value = cases["empty state"]
	value.State = nil
	cases["empty state"] = value
	value = cases["oversized state"]
	value.State = make([]byte, eventsourcing.MaxSnapshotStateBytes+1)
	cases["oversized state"] = value
	value = cases["metadata"]
	value.Metadata = map[string]string{"es.reserved": "value"}
	cases["metadata"] = value
	value = cases["created at"]
	value.CreatedAt = time.Time{}
	cases["created at"] = value
	for name, input := range cases {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewSnapshot(input); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewSnapshot() error = %v", err)
			}
		})
	}

	base, err := eventsourcing.NewSnapshot(valid)
	if err != nil {
		t.Fatal(err)
	}
	if base.Equal(eventsourcing.Snapshot{}) ||
		(eventsourcing.Snapshot{}).Equal(base) {
		t.Fatal("zero snapshot compared equal")
	}
	variants := []eventsourcing.SnapshotInput{
		{
			Stream:           mustStream(t, "account", "account-43"),
			AggregateVersion: 1,
			SchemaVersion:    1,
			State:            []byte(`{}`),
			CreatedAt:        valid.CreatedAt,
		},
		valid,
		valid,
		valid,
		valid,
	}
	variants[1].AggregateVersion = 2
	variants[2].SchemaVersion = 2
	variants[3].State = []byte(`{"other":true}`)
	variants[4].Metadata = map[string]string{"codec": "json"}
	createdVariant := valid
	createdVariant.CreatedAt = valid.CreatedAt.Add(time.Second)
	variants = append(variants, createdVariant)
	for index, input := range variants {
		other, err := eventsourcing.NewSnapshot(input)
		if err != nil {
			t.Fatal(err)
		}
		if base.Equal(other) {
			t.Fatalf("variant %d compared equal", index)
		}
	}
}

func TestSnapshotErrorsRedactStreamIdentity(t *testing.T) {
	t.Parallel()

	stream := mustStream(t, "account", "private-account")
	stale := &eventsourcing.SnapshotVersionError{
		Stream:                   stream,
		StoredAggregateVersion:   9,
		IncomingAggregateVersion: 8,
		StoredSchemaVersion:      2,
		IncomingSchemaVersion:    1,
	}
	if !errors.Is(stale, eventsourcing.ErrSnapshotStale) ||
		strings.Contains(stale.Error(), "private-account") {
		t.Fatalf("SnapshotVersionError = %v", stale)
	}
	conflict := &eventsourcing.SnapshotConflictError{
		Stream:           stream,
		AggregateVersion: 9,
		SchemaVersion:    2,
	}
	if !errors.Is(conflict, eventsourcing.ErrSnapshotConflict) ||
		strings.Contains(conflict.Error(), "private-account") {
		t.Fatalf("SnapshotConflictError = %v", conflict)
	}
}

func mustStream(
	t *testing.T,
	aggregateType string,
	aggregateID string,
) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}
