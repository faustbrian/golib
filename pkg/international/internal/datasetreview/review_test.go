package datasetreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestCurrentSnapshotRoundTripsDeterministically(t *testing.T) {
	t.Parallel()

	snapshot := Current()
	if snapshot.Schema != SchemaVersion || len(snapshot.Country) != 301 ||
		len(snapshot.Subdivision) != 5653 || len(snapshot.Currency) != 307 {
		t.Fatalf("snapshot metadata = schema %d, counts %d/%d/%d",
			snapshot.Schema, len(snapshot.Country), len(snapshot.Subdivision), len(snapshot.Currency))
	}
	var first bytes.Buffer
	if err := Encode(&first, snapshot); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := Decode(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	var second bytes.Buffer
	if err := Encode(&second, decoded); err != nil {
		t.Fatalf("second Encode() error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("snapshot encoding is not deterministic")
	}
}

func TestDiffClassifiesEachDatasetIndependently(t *testing.T) {
	t.Parallel()

	before := fixtureSnapshot()
	after := fixtureSnapshot()
	after.Country[0].Status = international.StatusDeleted
	after.Subdivision[0].AliasOf = "AA-2"
	after.Currency[0].Fingerprint = strings.Repeat("b", 64)
	report, err := Diff(before, after)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(report.Country.StatusChanged) != 1 || len(report.Subdivision.AliasesChanged) != 1 ||
		len(report.Currency.MetadataChanged) != 1 {
		t.Fatalf("Diff() = %#v", report)
	}
}

func TestEmptyDiffEncodesReviewableArrays(t *testing.T) {
	t.Parallel()

	snapshot := fixtureSnapshot()
	report, err := Diff(snapshot, snapshot)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if bytes.Contains(payload, []byte("null")) {
		t.Fatalf("empty diff contains null instead of arrays: %s", payload)
	}
}

func TestSnapshotValidationRejectsMalformedOrUnboundedInput(t *testing.T) {
	t.Parallel()

	invalid := make([]Snapshot, 0, 6)
	invalid = append(invalid,
		Snapshot{},
		Snapshot{Schema: SchemaVersion},
		Snapshot{Schema: SchemaVersion, Country: []international.Record{{ID: "AA", Status: international.StatusOfficial, Fingerprint: "bad"}}},
		Snapshot{Schema: SchemaVersion, Country: []international.Record{{ID: "AA", Status: international.Status(255), Fingerprint: strings.Repeat("a", 64)}}},
		Snapshot{Schema: SchemaVersion, Country: []international.Record{
			{ID: "BB", Status: international.StatusOfficial, Fingerprint: strings.Repeat("a", 64)},
			{ID: "AA", Status: international.StatusOfficial, Fingerprint: strings.Repeat("a", 64)},
		}},
	)
	overLimit := fixtureSnapshot()
	overLimit.Country = make([]international.Record, international.MaxDatasetRecords+1)
	invalid = append(invalid, overLimit)
	for _, snapshot := range invalid {
		if err := Encode(&bytes.Buffer{}, snapshot); !errors.Is(err, international.ErrInvalidDataset) {
			t.Errorf("Encode(%#v) error = %v, want ErrInvalidDataset", snapshot, err)
		}
	}

	inputs := []string{
		`{`,
		`{"schema":2,"country":[],"subdivision":[],"currency":[]}`,
		`{"schema":1,"country":[],"subdivision":[],"currency":[],"extra":true}`,
		`{"schema":1,"country":[],"subdivision":[],"currency":[]} {}`,
		strings.Repeat("x", MaxSnapshotBytes+1),
	}
	for _, input := range inputs {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Errorf("Decode malformed input succeeded")
		}
	}
	if err := Encode(nil, fixtureSnapshot()); !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("Encode(nil) error = %v", err)
	}
	if err := Encode(errorWriter{}, fixtureSnapshot()); err == nil {
		t.Fatal("Encode(errorWriter) succeeded")
	}
	if _, err := Decode(nil); !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("Decode(nil) error = %v", err)
	}
	if _, err := Decode(errorReader{}); err == nil {
		t.Fatal("Decode(errorReader) succeeded")
	}
	if _, err := Diff(Snapshot{}, fixtureSnapshot()); !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("Diff invalid snapshot error = %v", err)
	}
	if _, err := Diff(fixtureSnapshot(), Snapshot{}); !errors.Is(err, international.ErrInvalidDataset) {
		t.Fatalf("Diff invalid after snapshot error = %v", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func fixtureSnapshot() Snapshot {
	record := international.Record{
		ID: "AA", Status: international.StatusOfficial, Fingerprint: strings.Repeat("a", 64),
	}
	return Snapshot{
		Schema:      SchemaVersion,
		Country:     []international.Record{record},
		Subdivision: []international.Record{record},
		Currency:    []international.Record{record},
	}
}
