package knapsack_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const benchmarkEvidenceDate = "2026-07-24"
const benchmarkEvidenceDocument = "docs/benchmarks/2026-07-24-darwin-arm64.md"
const nativeBenchmarkRaw = "docs/benchmarks/raw/2026-07-24-darwin-arm64.txt"
const rssBenchmarkRaw = "docs/benchmarks/raw/2026-07-24-darwin-arm64-rss.tsv"

func TestNativeBenchmarkEvidenceIsCurrent(t *testing.T) {
	t.Parallel()

	metadata := readBenchmarkMetadata(t, nativeBenchmarkRaw)
	want := map[string]string{
		"go_version":              runtime.Version(),
		"environment":             runtime.GOOS + "/" + runtime.GOARCH,
		"date":                    benchmarkEvidenceDate,
		"duration":                "100ms",
		"samples":                 "10",
		"seed":                    "0",
		"solver_parallelism":      "1",
		"gate_status":             "pass",
		"raw_format":              "go_test_benchmark",
		"benchmark_source_sha256": fileSHA256(t, "solver/benchmark_test.go"),
		"generator_sha256":        fileSHA256(t, "scripts/benchmark-compare.sh"),
		"threshold_sha256":        fileSHA256(t, "specification/benchmark-thresholds.tsv"),
		"input_sha256":            benchmarkInputSHA256(t, "native"),
	}
	for field, value := range want {
		if metadata[field] != value {
			t.Fatalf("native benchmark evidence %s = %q, want %q", field, metadata[field], value)
		}
	}
	for _, field := range []string{"processor", "command", "semantic_normalization"} {
		if metadata[field] == "" {
			t.Fatalf("native benchmark evidence omits %s", field)
		}
	}
}

func TestRSSBenchmarkEvidenceIsCurrent(t *testing.T) {
	t.Parallel()

	metadata := readBenchmarkMetadata(t, rssBenchmarkRaw)
	want := map[string]string{
		"go_version":              runtime.Version(),
		"environment":             runtime.GOOS + "/" + runtime.GOARCH,
		"date":                    benchmarkEvidenceDate,
		"duration":                "100ms",
		"samples":                 "5",
		"seed":                    "0",
		"solver_parallelism":      "1",
		"gate_status":             "pass",
		"benchmark_source_sha256": fileSHA256(t, "solver/benchmark_test.go"),
		"generator_sha256":        fileSHA256(t, "scripts/benchmark-rss.sh"),
		"threshold_sha256":        fileSHA256(t, "specification/benchmark-rss-thresholds.tsv"),
		"input_sha256":            benchmarkInputSHA256(t, "rss"),
	}
	for field, value := range want {
		if metadata[field] != value {
			t.Fatalf("RSS benchmark evidence %s = %q, want %q", field, metadata[field], value)
		}
	}
	for _, field := range []string{"processor", "command", "semantic_normalization", "time_format"} {
		if metadata[field] == "" {
			t.Fatalf("RSS benchmark evidence omits %s", field)
		}
	}
}

func readBenchmarkMetadata(t *testing.T, path string) map[string]string {
	t.Helper()
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	metadata := map[string]string{}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if scanner.Text() == "go_test_output" {
			break
		}
		key, value, found := strings.Cut(scanner.Text(), "\t")
		if found {
			metadata[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return metadata
}

func benchmarkInputSHA256(t *testing.T, profile string) string {
	t.Helper()
	output, err := exec.Command("./scripts/benchmark-input-digest.sh", profile).Output()
	if err != nil {
		t.Fatalf("benchmark input digest: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
