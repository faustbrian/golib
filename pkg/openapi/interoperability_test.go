package openapi_test

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestInteroperabilityMatrixCoversEveryFixtureAndTool(t *testing.T) {
	t.Parallel()

	fixtures, err := os.ReadDir("interoperability/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	wantFixtures := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		if fixture.Type().IsRegular() {
			wantFixtures[fixture.Name()] = true
		}
	}
	wantFixtures["openapi.yaml"] = true
	wantFixtures["api.github.com.2022-11-28.json"] = true
	wantTools := map[string]string{
		"golib-openapi":      "workspace",
		"getkin/kin-openapi": "v0.143.0",
		"pb33f/libopenapi":   "v0.38.7",
		"go-openapi/loads":   "v0.25.0",
	}

	file, err := os.Open(filepath.Join("interoperability", "expected.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 7
	header, err := reader.Read()
	if err != nil || header[0] != "fixture" || header[6] != "roundtrip" {
		t.Fatalf("matrix header = %#v, %v", header, err)
	}
	seen := make(map[string]map[string]bool, len(wantFixtures))
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !wantFixtures[row[0]] {
			t.Fatalf("unexpected fixture %q", row[0])
		}
		version, exists := wantTools[row[1]]
		if !exists || row[2] != version {
			t.Fatalf("unexpected tool version %q %q", row[1], row[2])
		}
		if seen[row[0]] == nil {
			seen[row[0]] = make(map[string]bool)
		}
		if seen[row[0]][row[1]] {
			t.Fatalf("duplicate matrix row for %q and %q", row[0], row[1])
		}
		seen[row[0]][row[1]] = true
		for _, status := range row[3:] {
			if status != "accept" && status != "reject" && status != "na" {
				t.Fatalf("invalid status %q", status)
			}
		}
	}
	for fixture := range wantFixtures {
		for tool := range wantTools {
			if !seen[fixture][tool] {
				t.Errorf("missing matrix row for %q and %q", fixture, tool)
			}
		}
	}
}
