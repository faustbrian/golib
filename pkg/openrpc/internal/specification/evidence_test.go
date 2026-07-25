package specification

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConformanceEvidenceReferencesExecutableTests(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("../..")
	normative := readTSV(t, filepath.Join(root, "specification/conformance/normative.tsv"))
	known := make(map[string]bool, len(normative))
	for _, row := range normative[1:] {
		known[row[0]] = true
	}
	evidence := readTSV(t, filepath.Join(root, "specification/conformance/evidence.tsv"))
	seen := make(map[string]bool, len(evidence))
	for rowIndex, row := range evidence[1:] {
		if len(row) != 5 {
			t.Fatalf("evidence row %d has %d columns", rowIndex+2, len(row))
		}
		if !known[row[0]] || seen[row[0]] {
			t.Fatalf("invalid or duplicate evidence id %q", row[0])
		}
		seen[row[0]] = true
		if row[3] != "complete" && row[3] != "not-applicable" {
			t.Fatalf("invalid status %q for %s", row[3], row[0])
		}
		if row[3] == "not-applicable" {
			if _, documented := nonBehavioralRequirements[row[0]]; !documented {
				t.Fatalf("undocumented non-behavioral requirement %s", row[0])
			}
		}
		assertEvidenceTarget(t, root, row[1], false)
		assertEvidenceTarget(t, root, row[2], true)
	}
}

func TestConformanceEvidenceCoversEveryNormativeStatement(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("../..")
	normative := readTSV(t, filepath.Join(root, "specification/conformance/normative.tsv"))
	evidence := readTSV(t, filepath.Join(root, "specification/conformance/evidence.tsv"))
	covered := make(map[string]string, len(evidence)-1)
	for _, row := range evidence[1:] {
		covered[row[0]] = row[3]
	}
	for _, row := range normative[1:] {
		if _, found := covered[row[0]]; !found {
			t.Errorf("normative requirement %s has no reviewed disposition", row[0])
		}
	}
	for requirement := range nonBehavioralRequirements {
		if covered[requirement] != "not-applicable" {
			t.Errorf("non-behavioral requirement %s has status %q", requirement, covered[requirement])
		}
	}
}

func TestObjectFieldMatrixReferencesExecutableEvidence(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("../..")
	rows := readTSV(t, filepath.Join(root, "specification/conformance/object-fields.tsv"))
	for rowIndex, row := range rows[1:] {
		if len(row) != 12 {
			t.Fatalf("object-field row %d has %d columns", rowIndex+2, len(row))
		}
		if row[11] != "complete" {
			t.Fatalf("object-field row %d has status %q", rowIndex+2, row[11])
		}
		assertEvidenceTarget(t, root, row[8], false)
		assertEvidenceTarget(t, root, row[9], false)
		assertEvidenceTarget(t, root, row[10], true)
	}
}

func readTSV(t *testing.T, path string) [][]string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	rows := make([][]string, len(lines))
	for index, line := range lines {
		rows[index] = strings.Split(line, "\t")
	}
	return rows
}

func assertEvidenceTarget(t *testing.T, root string, target string, executable bool) {
	t.Helper()
	fileName, symbol, found := strings.Cut(target, ":")
	if !found || fileName == "" || symbol == "" {
		t.Fatalf("invalid evidence target %q", target)
	}
	contents, err := os.ReadFile(filepath.Join(root, fileName))
	if err != nil {
		t.Fatalf("evidence target %q: %v", target, err)
	}
	needle := symbol
	if executable {
		needle = "func " + symbol + "("
	} else if separator := strings.LastIndexByte(needle, '.'); separator >= 0 {
		needle = needle[separator+1:]
	}
	if !strings.Contains(string(contents), needle) {
		t.Fatalf("evidence target %q does not contain %q", target, needle)
	}
}
