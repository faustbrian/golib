package opensearch_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBenchmarkEvidenceValidatorRequiresEverySuccessfulSample(t *testing.T) {
	const samples = 2
	transcript := benchmarkTranscript(samples)
	path := t.TempDir() + "/benchmark.txt"
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("./scripts/check-benchmark-evidence.sh", path, "2").CombinedOutput(); err != nil {
		t.Fatalf("valid transcript rejected: %v: %s", err, output)
	}

	failed := transcript + "--- FAIL: BenchmarkSharedSearchSemantics/fake/indexing-16\n"
	if err := os.WriteFile(path, []byte(failed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("./scripts/check-benchmark-evidence.sh", path, "2").Run(); err == nil {
		t.Fatal("failed benchmark transcript was accepted")
	}

	missing := strings.Replace(transcript,
		"BenchmarkSharedSearchSemantics/fake/indexing-16 20 1 ns/op\n", "", 1)
	if err := os.WriteFile(path, []byte(missing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("./scripts/check-benchmark-evidence.sh", path, "2").Run(); err == nil {
		t.Fatal("incomplete benchmark transcript was accepted")
	}
}

func benchmarkTranscript(samples int) string {
	names := []string{
		"fake/indexing", "fake/query", "fake/bulk_indexing", "fake/pagination",
		"opensearch-adapter/indexing", "opensearch-adapter/query", "opensearch-adapter/bulk_indexing",
		"opensearch-adapter/pagination", "opensearch-adapter/cursor_pagination",
		"direct-official-client/indexing", "direct-official-client/query", "direct-official-client/bulk_indexing",
		"direct-official-client/pagination", "direct-official-client/cursor_pagination",
	}
	var transcript strings.Builder
	for _, name := range names {
		for range samples {
			transcript.WriteString("BenchmarkSharedSearchSemantics/")
			transcript.WriteString(name)
			transcript.WriteString("-16 20 1 ns/op\n")
		}
	}
	transcript.WriteString("PASS\n")
	return transcript.String()
}
