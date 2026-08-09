package specification_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"
)

type provenanceManifest struct {
	Schema    int                `json:"schema"`
	Standards []provenanceSource `json:"standards"`
	Artifacts []provenanceSource `json:"artifacts"`
}

type provenanceSource struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Source  string `json:"source"`
	Release string `json:"release"`
	SHA256  string `json:"sha256"`
	Status  string `json:"status"`
	License string `json:"license"`
	Use     string `json:"use"`
}

func TestManifestProvenanceIsWellFormed(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest provenanceManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.Schema != 1 || len(manifest.Standards) == 0 || len(manifest.Artifacts) == 0 {
		t.Fatalf("manifest inventory is incomplete: %+v", manifest)
	}

	seenStandards := make(map[string]bool, len(manifest.Standards))
	for _, source := range manifest.Standards {
		if source.ID == "" || seenStandards[source.ID] {
			t.Fatalf("standard has empty or duplicate ID %q", source.ID)
		}
		seenStandards[source.ID] = true
		assertPinnedHTTPS(t, source)
		if source.Status == "" {
			t.Fatalf("standard %q has no status", source.ID)
		}
	}

	seenArtifacts := make(map[string]bool, len(manifest.Artifacts))
	for _, source := range manifest.Artifacts {
		if source.Path == "" || seenArtifacts[source.Path] {
			t.Fatalf("artifact has empty or duplicate path %q", source.Path)
		}
		seenArtifacts[source.Path] = true
		assertPinnedHTTPS(t, source)
		if source.License == "" || source.Use == "" {
			t.Fatalf("artifact %q has incomplete licensing or use metadata", source.Path)
		}
		digest, decodeErr := hex.DecodeString(source.SHA256)
		if decodeErr != nil || len(digest) != sha256.Size {
			t.Fatalf("artifact %q has invalid SHA-256 %q", source.Path, source.SHA256)
		}
	}
}

func assertPinnedHTTPS(t *testing.T, source provenanceSource) {
	t.Helper()

	parsed, err := url.Parse(source.Source)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		t.Fatalf("source %q has invalid authoritative URL %q", source.ID+source.Path, source.Source)
	}
	if source.Release == "" {
		t.Fatalf("source %q has no release pin", source.ID+source.Path)
	}
}
