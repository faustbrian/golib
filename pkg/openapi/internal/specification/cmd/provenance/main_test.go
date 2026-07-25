package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzProvenanceManifestDecoder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"evidence":{"license":"MIT","licenseSource":"https://example.test/LICENSE","revision":"abc","files":[]}}`,
		`[]`,
		`{`,
		`{} {}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		manifest, err := decodeManifest(bytes.NewReader(raw))
		if err != nil {
			return
		}
		_ = validateArtifactMetadata(manifest)
		_, _ = collectArtifacts(manifest)
	})
}

func TestVerifyAcceptsEveryManifestArtifact(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeArtifactManifest(t, root, "oas/spec.md", []byte("pinned\n"))
	if err := verify(root); err != nil {
		t.Fatalf("verify() error = %v", err)
	}
}

func TestVerifyAcceptsManifestAtExactByteLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	body := []byte("pinned\n")
	writeArtifactManifest(t, root, "oas/spec.md", body)
	digest := sha256.Sum256(body)
	prefix := `{"padding":"`
	suffix := `","evidence":{"license":"Apache-2.0",` +
		`"licenseSource":"https://example.test/LICENSE",` +
		`"revision":"0123456789abcdef",` +
		`"files":[{"source":"spec.md","path":"oas/spec.md",` +
		`"sha256":"` + hex.EncodeToString(digest[:]) + `"}]}}`
	padding := strings.Repeat(
		"x", maximumManifestBytes-len(prefix)-len(suffix),
	)
	manifest := prefix + padding + suffix
	if len(manifest) != maximumManifestBytes {
		t.Fatalf("manifest bytes = %d", len(manifest))
	}
	if err := os.WriteFile(
		filepath.Join(root, "specification", "manifest.json"),
		[]byte(manifest), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := verify(root); err != nil {
		t.Fatalf("exact-limit manifest error = %v", err)
	}
}

func TestVerifyRejectsManifestBeyondByteLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	body := []byte("pinned\n")
	writeArtifactManifest(t, root, "oas/spec.md", body)
	path := filepath.Join(root, "specification", "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oversized := append(raw, bytes.Repeat(
		[]byte(" "), maximumManifestBytes-len(raw)+1,
	)...)
	if len(oversized) != maximumManifestBytes+1 {
		t.Fatalf("manifest bytes = %d", len(oversized))
	}
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(root); err == nil {
		t.Fatal("verify accepted an oversized manifest")
	}
}

func TestVerifyRejectsIncompleteArtifactProvenance(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		`"license":"Apache-2.0",`,
		`"licenseSource":"https://example.test/LICENSE",`,
		`"revision":"0123456789abcdef",`,
		`"source":"spec.md",`,
	} {
		root := t.TempDir()
		writeArtifactManifest(t, root, "oas/spec.md", []byte("pinned\n"))
		path := filepath.Join(root, "specification", "manifest.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			path, []byte(strings.Replace(string(raw), fragment, "", 1)), 0o600,
		); err != nil {
			t.Fatal(err)
		}
		if err := verify(root); err == nil {
			t.Fatalf("verify accepted manifest without %s", fragment)
		}
	}
}

func TestArtifactRevisionMetadataShapes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		group map[string]any
		want  bool
	}{
		{name: "revision", group: map[string]any{"revision": "abc"}, want: true},
		{name: "empty revisions", group: map[string]any{"revisions": map[string]any{}}},
		{name: "non-string revision", group: map[string]any{
			"revisions": map[string]any{"3.2": true},
		}},
		{name: "blank revision", group: map[string]any{
			"revisions": map[string]any{"3.2": " "},
		}},
		{name: "revisions", group: map[string]any{
			"revisions": map[string]any{"3.2": "abc"},
		}, want: true},
		{name: "missing retrieval", group: map[string]any{}},
		{name: "invalid retrieval", group: map[string]any{"retrievedAt": "today"}},
		{name: "retrieval", group: map[string]any{"retrievedAt": "2026-07-22"}, want: true},
	} {
		if got := hasArtifactRevision(test.group); got != test.want {
			t.Errorf("%s revision metadata = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestValidateArtifactMetadataPropagatesNestedArrayFailure(t *testing.T) {
	t.Parallel()

	err := validateArtifactMetadata([]any{map[string]any{"files": "invalid"}})
	if err == nil {
		t.Fatal("nested invalid files value was accepted")
	}
}

func TestVerifyRejectsUntrustedOrChangedArtifacts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		path     string
		checksum string
		mutate   func(*testing.T, string)
	}{
		{
			name:     "changed bytes",
			path:     "oas/spec.md",
			checksum: strings.Repeat("0", 64),
		},
		{
			name:     "invalid checksum",
			path:     "oas/spec.md",
			checksum: "not-sha256",
		},
		{
			name:     "escaping path",
			path:     "../outside.md",
			checksum: strings.Repeat("0", 64),
		},
		{
			name:     "symlink",
			path:     "oas/spec.md",
			checksum: strings.Repeat("0", 64),
			mutate: func(t *testing.T, root string) {
				t.Helper()
				path := filepath.Join(root, "specification", "oas", "spec.md")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../target.md", path); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeArtifactManifestWithChecksum(
				t, root, test.path, []byte("pinned\n"), test.checksum,
			)
			if test.mutate != nil {
				test.mutate(t, root)
			}
			if err := verify(root); err == nil {
				t.Fatal("verify accepted an untrusted artifact")
			}
		})
	}
}

func TestVerifyRejectsInvalidOrAmbiguousManifests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specification"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "specification", "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"files":[]} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(root); err == nil ||
		!strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing manifest error = %v", err)
	}

	writeArtifactManifest(t, root, "oas/spec.md", []byte("pinned\n"))
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("pinned\n"))
	duplicate := strings.Replace(
		string(raw), `"files":[`, `"duplicates":[{"path":"oas/spec.md","sha256":"`+
			hex.EncodeToString(digest[:])+`"}],"files":[`, 1,
	)
	if err := os.WriteFile(manifest, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(root); err == nil {
		t.Fatal("verify accepted a duplicate artifact path")
	}
}

func TestVerifyRejectsMissingMalformedEmptyAndPartialManifests(t *testing.T) {
	t.Parallel()

	if err := verify(t.TempDir()); err == nil {
		t.Fatal("verify accepted a missing manifest")
	}
	for _, manifest := range []string{
		`{`,
		`{}`,
		`{"files":[{"path":"oas/spec.md"}]}`,
		`{"files":[{"sha256":"` + strings.Repeat("0", 64) + `"}]}`,
		`{"outer":[{"path":"oas/spec.md"}]}`,
	} {
		root := t.TempDir()
		specificationRoot := filepath.Join(root, "specification")
		if err := os.MkdirAll(specificationRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(specificationRoot, "manifest.json"),
			[]byte(manifest),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if err := verify(root); err == nil {
			t.Fatalf("verify accepted manifest %s", manifest)
		}
	}
}

func TestSecureArtifactPathRejectsEveryUnsafeFilesystemShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, path := range []string{"", `oas\spec.md`, "missing/spec.md"} {
		if _, err := secureArtifactPath(root, path); err == nil {
			t.Fatalf("secureArtifactPath accepted %q", path)
		}
	}
	directory := filepath.Join(root, "oas")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := secureArtifactPath(root, "oas"); err == nil {
		t.Fatal("secureArtifactPath accepted a directory")
	}
}

func TestVerifyPropagatesArtifactReadFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeArtifactManifest(t, root, "oas/spec.md", []byte("pinned\n"))
	path := filepath.Join(root, "specification", "oas", "spec.md")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := verify(root); err == nil {
		t.Fatal("verify accepted an unreadable artifact")
	}
}

func TestEqualDigestRejectsDifferentLengths(t *testing.T) {
	t.Parallel()

	if equalDigest([]byte{1}, []byte{1, 2}) {
		t.Fatal("equalDigest accepted different lengths")
	}
}

func TestExecuteReportsFlagVerificationAndSuccessOutcomes(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if code := execute([]string{"-unknown"}, &stderr); code != 2 {
		t.Fatalf("flag failure exit = %d", code)
	}
	if code := execute([]string{"-root", t.TempDir()}, &stderr); code != 1 {
		t.Fatalf("verification failure exit = %d", code)
	}
	root := t.TempDir()
	writeArtifactManifest(t, root, "oas/spec.md", []byte("pinned\n"))
	if code := execute([]string{"-root", root}, &stderr); code != 0 {
		t.Fatalf("success exit = %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("execute did not report its failures")
	}
}

func TestMainDelegatesToProcessExit(t *testing.T) {
	originalArgs := os.Args
	originalExit := exitProcess
	t.Cleanup(func() {
		os.Args = originalArgs
		exitProcess = originalExit
	})
	os.Args = []string{"provenance", "-root", t.TempDir()}
	got := -1
	exitProcess = func(code int) { got = code }
	main()
	if got != 1 {
		t.Fatalf("main exit = %d", got)
	}
}

func TestCollectArtifactsReturnsNestedTraversalFailures(t *testing.T) {
	t.Parallel()

	_, err := collectArtifacts(map[string]any{
		"outer": []any{map[string]any{"path": "only"}},
	})
	if err == nil {
		t.Fatal("collectArtifacts accepted a partial nested artifact")
	}
}

func writeArtifactManifest(t *testing.T, root, path string, body []byte) {
	t.Helper()
	digest := sha256.Sum256(body)
	writeArtifactManifestWithChecksum(
		t, root, path, body, hex.EncodeToString(digest[:]),
	)
}

func writeArtifactManifestWithChecksum(
	t *testing.T,
	root string,
	path string,
	body []byte,
	checksum string,
) {
	t.Helper()
	specificationRoot := filepath.Join(root, "specification")
	if err := os.MkdirAll(specificationRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(specificationRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, body, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"evidence":{"license":"Apache-2.0",` +
		`"licenseSource":"https://example.test/LICENSE",` +
		`"revision":"0123456789abcdef",` +
		`"files":[{"source":"spec.md","path":"` + path +
		`","sha256":"` + checksum + `"}]}}`
	if err := os.WriteFile(
		filepath.Join(specificationRoot, "manifest.json"),
		[]byte(manifest),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
