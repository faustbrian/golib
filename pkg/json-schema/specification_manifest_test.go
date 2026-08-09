package jsonschema_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type specificationManifest struct {
	Schema     int                   `json:"schema"`
	ReviewedAt string                `json:"reviewed_at"`
	Sources    []specificationSource `json:"sources"`
}

type specificationSource struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	URL         string                  `json:"url"`
	Version     string                  `json:"version"`
	Revision    string                  `json:"revision"`
	RetrievedAt string                  `json:"retrieved_at"`
	GeneratedAt string                  `json:"generated_at"`
	SHA256      string                  `json:"sha256"`
	DigestScope string                  `json:"digest_scope"`
	License     string                  `json:"license"`
	Use         string                  `json:"use"`
	Evidence    []specificationEvidence `json:"evidence"`
}

type specificationEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type expectedSpecificationSource struct {
	EvidencePaths []string
}

func TestSpecificationManifestPinsEveryConformanceSource(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("specification/manifest.json")
	if err != nil {
		t.Fatalf("read specification manifest: %v", err)
	}
	var manifest specificationManifest
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode specification manifest: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("decode trailing specification manifest content: %v", err)
	}
	if manifest.Schema != 1 {
		t.Fatalf("manifest schema = %d, want 1", manifest.Schema)
	}
	if _, err := time.Parse(time.DateOnly, manifest.ReviewedAt); err != nil {
		t.Fatalf("manifest review date = %q: %v", manifest.ReviewedAt, err)
	}
	wantSources := map[string]expectedSpecificationSource{
		"bowtie": {
			EvidencePaths: []string{"bowtie/reports/SHA256SUMS"},
		},
		"json-schema-test-suite": {
			EvidencePaths: []string{
				"specification/official-suite.env",
				"specification/official-suite.sha256",
				"specification/official-suite.symlinks",
				"specification/official-suite-results.tsv",
			},
		},
		"official-meta-schemas": {
			EvidencePaths: []string{
				"specification/official-meta-schemas.sources.tsv",
				"specification/official-meta-schemas.sha256",
			},
		},
	}
	seenSources := make(map[string]struct{}, len(wantSources))
	for _, source := range manifest.Sources {
		expected, known := wantSources[source.ID]
		_, duplicate := seenSources[source.ID]
		if !known || duplicate {
			t.Fatalf("unexpected or duplicate source %q", source.ID)
		}
		seenSources[source.ID] = struct{}{}
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			t.Fatalf("source %s URL = %q, want absolute HTTPS", source.ID, source.URL)
		}
		if source.Version == "" && source.Revision == "" {
			t.Fatalf("source %s has no immutable version or revision", source.ID)
		}
		if source.Name == "" || source.DigestScope == "" || source.License == "" || source.Use == "" {
			t.Fatalf("source %s has incomplete provenance metadata", source.ID)
		}
		switch source.ID {
		case "official-meta-schemas":
			assertDate(t, source.ID+" retrieval", source.RetrievedAt)
			assertSourceDigestMatchesEvidence(t, source, "specification/official-meta-schemas.sha256")
		case "bowtie":
			assertDate(t, source.ID+" generation", source.GeneratedAt)
			assertSourceDigestMatchesEvidence(t, source, "bowtie/reports/SHA256SUMS")
		case "json-schema-test-suite":
			assertSuiteSourcePin(t, source)
		}
		assertSHA256(t, source.ID, source.SHA256)
		if len(source.Evidence) == 0 {
			t.Fatalf("source %s has no local evidence", source.ID)
		}
		evidencePaths := make(map[string]struct{}, len(source.Evidence))
		for _, evidence := range source.Evidence {
			if _, duplicate := evidencePaths[evidence.Path]; duplicate {
				t.Fatalf("source %s has duplicate evidence path %q", source.ID, evidence.Path)
			}
			evidencePaths[evidence.Path] = struct{}{}
			assertEvidenceDigest(t, source.ID, evidence)
		}
		for _, path := range expected.EvidencePaths {
			if _, exists := evidencePaths[path]; !exists {
				t.Fatalf("source %s is missing evidence path %q", source.ID, path)
			}
		}
		if len(evidencePaths) != len(expected.EvidencePaths) {
			t.Fatalf("source %s has %d evidence paths, want %d", source.ID, len(evidencePaths), len(expected.EvidencePaths))
		}
	}
	for source := range wantSources {
		if _, seen := seenSources[source]; !seen {
			t.Fatalf("manifest is missing source %q", source)
		}
	}
}

func assertSuiteSourcePin(t *testing.T, source specificationSource) {
	t.Helper()

	contents, err := os.ReadFile("specification/official-suite.env")
	if err != nil {
		t.Fatalf("read official suite source pin: %v", err)
	}
	want := map[string]string{
		"SUITE_REVISION":       source.Revision,
		"SUITE_ARCHIVE_SHA256": source.SHA256,
	}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Fatalf("invalid official suite source pin line %q", line)
		}
		expected, known := want[key]
		if !known || expected != value {
			t.Fatalf("official suite source pin %s = %q, manifest has %q", key, value, expected)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("official suite source pin is missing manifest values: %v", want)
	}
}

func assertSourceDigestMatchesEvidence(t *testing.T, source specificationSource, path string) {
	t.Helper()

	for _, evidence := range source.Evidence {
		if evidence.Path == path {
			if source.SHA256 != evidence.SHA256 {
				t.Fatalf("source %s digest = %s, evidence digest = %s", source.ID, source.SHA256, evidence.SHA256)
			}
			return
		}
	}
	t.Fatalf("source %s has no checksum-manifest evidence %q", source.ID, path)
}

func assertDate(t *testing.T, field, value string) {
	t.Helper()

	if _, err := time.Parse(time.DateOnly, value); err != nil {
		t.Fatalf("%s date = %q: %v", field, value, err)
	}
}

func assertEvidenceDigest(t *testing.T, source string, evidence specificationEvidence) {
	t.Helper()

	clean := filepath.Clean(evidence.Path)
	if clean != evidence.Path || clean == "." || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatalf("source %s evidence path escapes module: %q", source, evidence.Path)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		t.Fatalf("inspect source %s evidence %s: %v", source, evidence.Path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("source %s evidence %s is not a regular file", source, evidence.Path)
	}
	contents, err := os.ReadFile(clean)
	if err != nil {
		t.Fatalf("read source %s evidence %s: %v", source, evidence.Path, err)
	}
	digest := sha256.Sum256(contents)
	got := hex.EncodeToString(digest[:])
	if got != evidence.SHA256 {
		t.Fatalf("source %s evidence %s digest = %s, want %s", source, evidence.Path, got, evidence.SHA256)
	}
}

func assertSHA256(t *testing.T, source, digest string) {
	t.Helper()

	if digest != strings.ToLower(digest) {
		t.Fatalf("source %s SHA-256 = %q, want lowercase hexadecimal", source, digest)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		t.Fatalf("source %s SHA-256 = %q, want 64 hexadecimal characters", source, digest)
	}
}
