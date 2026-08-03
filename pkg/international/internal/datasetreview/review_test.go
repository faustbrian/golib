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
	for _, snapshot := range invalid {
		if err := Encode(&bytes.Buffer{}, snapshot); !errors.Is(err, international.ErrInvalidDataset) {
			t.Errorf("Encode(%#v) error = %v, want ErrInvalidDataset", snapshot, err)
		}
	}
	overLimit := fixtureSnapshot()
	overLimit.Country = make([]international.Record, international.MaxDatasetRecords+1)
	if err := Encode(&bytes.Buffer{}, overLimit); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Encode(over limit) error = %v, want ErrResourceLimit", err)
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

func TestSnapshotAndRecordSizeBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := Decode(strings.NewReader(strings.Repeat("x", MaxSnapshotBytes+1))); !errors.Is(
		err,
		international.ErrResourceLimit,
	) {
		t.Fatalf("Decode(over limit) error = %v, want ErrResourceLimit", err)
	}
	if _, err := Decode(strings.NewReader(strings.Repeat("x", MaxSnapshotBytes))); !errors.Is(err, international.ErrInvalidDataset) || errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Decode(at limit) error = %v, want ErrInvalidDataset", err)
	}

	atLimit := make([]international.Record, international.MaxDatasetRecords)
	atLimit[0] = international.Record{
		ID: "AA", Status: international.StatusOfficial, Fingerprint: strings.Repeat("a", 64),
	}
	if err := validateRecords(atLimit); !errors.Is(err, international.ErrInvalidDataset) ||
		errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("validateRecords(at limit) error = %v, want ErrInvalidDataset", err)
	}
}

func TestRecordValidationChecksEachFieldIndependently(t *testing.T) {
	t.Parallel()

	fingerprint := strings.Repeat("a", 64)
	if err := validateRecords([]international.Record{{
		ID: "AA", Status: international.StatusHistoric, Fingerprint: fingerprint,
	}}); err != nil {
		t.Fatalf("validateRecords(valid historic record) error = %v", err)
	}

	for name, records := range map[string][]international.Record{
		"empty ID": {{Status: international.StatusOfficial, Fingerprint: fingerprint}},
		"duplicate ID": {
			{ID: "AA", Status: international.StatusOfficial, Fingerprint: fingerprint},
			{ID: "AA", Status: international.StatusOfficial, Fingerprint: fingerprint},
		},
		"unknown status":        {{ID: "AA", Status: international.Status(255), Fingerprint: fingerprint}},
		"short fingerprint":     {{ID: "AA", Status: international.StatusOfficial, Fingerprint: strings.Repeat("a", 62)}},
		"malformed fingerprint": {{ID: "AA", Status: international.StatusOfficial, Fingerprint: strings.Repeat("g", 64)}},
	} {
		if err := validateRecords(records); !errors.Is(err, international.ErrInvalidDataset) {
			t.Errorf("validateRecords(%s) error = %v, want ErrInvalidDataset", name, err)
		}
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
