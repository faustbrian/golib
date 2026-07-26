package specification

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedEvidenceMatchesNormativeInventory(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	normative := readTSV(t, filepath.Join(root, "specification", "conformance", "normative.tsv"))
	evidence := readTSV(t, filepath.Join(root, "specification", "conformance", "evidence.tsv"))
	if len(evidence) != len(normative) {
		t.Fatalf("evidence rows = %d, normative rows = %d", len(evidence), len(normative))
	}
	for index := 1; index < len(normative); index++ {
		if evidence[index][0] != normative[index][0] {
			t.Fatalf(
				"evidence row %d ID = %q, want %q",
				index, evidence[index][0], normative[index][0],
			)
		}
		status := evidence[index][1]
		switch status {
		case "unimplemented":
			continue
		case "partial", "implemented":
		default:
			t.Fatalf("evidence row %s has invalid status %q", evidence[index][0], status)
		}
		for column := 2; column < 6; column++ {
			if evidence[index][column] == "" {
				t.Fatalf("evidence row %s has empty reviewed column %d", evidence[index][0], column)
			}
		}
		for _, column := range []int{2, 3, 4} {
			path := strings.SplitN(evidence[index][column], "#", 2)[0]
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				t.Fatalf(
					"evidence row %s path %q: %v",
					evidence[index][0], path, err,
				)
			}
		}
	}
}

func readTSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 0
	rows, err := reader.ReadAll()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return rows
}
