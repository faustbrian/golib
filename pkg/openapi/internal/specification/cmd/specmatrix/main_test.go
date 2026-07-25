package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGeneratesNormativeObjectAndInitialEvidenceInventories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	specificationRoot := filepath.Join(root, "specification")
	if err := os.MkdirAll(filepath.Join(specificationRoot, "oas", "3.2"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "generatedAt": "2026-07-19",
  "openapiSpecification": {
    "repository": "https://example.test/spec",
    "license": "Apache-2.0",
    "revisions": {"3.2": "revision"},
    "files": [{
      "version": "3.2.0",
      "source": "versions/3.2.0.md",
      "path": "oas/3.2/3.2.0.md",
      "sha256": "checksum"
    }]
  },
  "publishedArtifacts": {}
}`
	prose := `# Specification

## Info Object

| Field Name | Type | Description |
| --- | --- | --- |
| title | string | REQUIRED. A title. |

The title MUST be present.
`
	if err := os.WriteFile(filepath.Join(specificationRoot, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specificationRoot, "oas", "3.2", "3.2.0.md"), []byte(prose), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run(root); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	checks := map[string]string{
		"normative.tsv":     "OAS-3.2.0-0001",
		"object-fields.tsv": "OAS-3.2.0-F0001",
		"evidence.tsv":      "unimplemented",
	}
	for name, fragment := range checks {
		raw, err := os.ReadFile(filepath.Join(specificationRoot, "conformance", name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(raw), fragment) {
			t.Errorf("%s does not contain %q", name, fragment)
		}
	}
}

func FuzzSpecmatrixManifestDecoder(f *testing.F) {
	for _, seed := range []string{
		manifestWithFiles(),
		manifestWithFiles(`{"version":"3.2.0","path":"oas/3.2/3.2.0.md"}`),
		`{}`,
		`[]`,
		`{`,
		`{} {}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = decodeManifest(bytes.NewReader(raw))
	})
}

func TestDecodeManifestEnforcesExactByteLimit(t *testing.T) {
	t.Parallel()

	prefix := manifestWithFiles()
	exact := prefix + strings.Repeat(
		" ", maximumSpecmatrixManifestBytes-len(prefix),
	)
	if _, err := decodeManifest(strings.NewReader(exact)); err != nil {
		t.Fatalf("exact-limit manifest error = %v", err)
	}
	if _, err := decodeManifest(strings.NewReader(exact + " ")); err == nil {
		t.Fatal("decodeManifest accepted oversized input")
	}
}

func TestDecodeManifestAcceptsReviewedIndependentDescriptions(t *testing.T) {
	t.Parallel()

	raw := `{
  "openapiSpecification": {"files": []},
  "independentDescriptions": {"public": {"revision": "pinned"}}
}`
	decoded, err := decodeManifest(strings.NewReader(raw))
	if err != nil || !bytes.Contains(decoded.Independent, []byte(`"pinned"`)) {
		t.Fatalf("independent descriptions = %s, %v", decoded.Independent, err)
	}
	if _, err := decodeManifest(strings.NewReader(
		`{"openapiSpecification":{"files":[]},"unknown":{}}`,
	)); err == nil {
		t.Fatal("decodeManifest accepted an unknown artifact group")
	}
}

func TestRunRejectsInvalidInputsAndOutputLayouts(t *testing.T) {
	t.Parallel()

	if err := run(t.TempDir()); err == nil {
		t.Fatal("run accepted a missing manifest")
	}
	invalidManifest := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(invalidManifest, "specification", "manifest.json"), `{`)
	if err := run(invalidManifest); err == nil {
		t.Fatal("run accepted invalid manifest JSON")
	}
	trailingManifest := t.TempDir()
	writeSpecmatrixFile(
		t,
		filepath.Join(trailingManifest, "specification", "manifest.json"),
		manifestWithFiles()+` {}`,
	)
	if err := run(trailingManifest); err == nil {
		t.Fatal("run accepted concatenated manifest JSON")
	}
	largeManifest := t.TempDir()
	manifest := manifestWithFiles()
	manifest += strings.Repeat(" ", 1024*1024-len(manifest)+1)
	writeSpecmatrixFile(
		t,
		filepath.Join(largeManifest, "specification", "manifest.json"),
		manifest,
	)
	if err := run(largeManifest); err == nil {
		t.Fatal("run accepted an oversized manifest")
	}
	missingSource := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(missingSource, "specification", "manifest.json"), manifestWithFiles(
		`{"version":"3.2.0","path":"missing.md"}`,
	))
	if err := run(missingSource); err == nil {
		t.Fatal("run accepted a missing specification source")
	}
	oversizedSource := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(oversizedSource, "specification", "manifest.json"), manifestWithFiles(
		`{"version":"3.2.0","path":"large.md"}`,
	))
	writeSpecmatrixFile(t, filepath.Join(oversizedSource, "specification", "large.md"),
		strings.Repeat("x", 1024*1024+1))
	if err := run(oversizedSource); err == nil {
		t.Fatal("run accepted an oversized specification line")
	}
	blockedOutput := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(blockedOutput, "specification", "manifest.json"), manifestWithFiles())
	writeSpecmatrixFile(t, filepath.Join(blockedOutput, "specification", "conformance"), "not a directory")
	if err := run(blockedOutput); err == nil {
		t.Fatal("run accepted a non-directory conformance path")
	}
}

func TestRunSkipsNonVersionedMarkdownInputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"), manifestWithFiles(
		`{"version":"all","path":"all.md"}`,
		`{"version":"3.2.0","path":"schema.json"}`,
	))
	if err := run(root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "specification", "conformance", "normative.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("skipped inventory = %q", raw)
	}
}

func TestRunPropagatesInventoryDestinationFailures(t *testing.T) {
	t.Parallel()

	for _, destination := range []string{"normative.tsv", "object-fields.tsv"} {
		root := t.TempDir()
		writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"), manifestWithFiles())
		if err := os.MkdirAll(
			filepath.Join(root, "specification", "conformance", destination),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
		if err := run(root); err == nil {
			t.Fatalf("run replaced the %s directory", destination)
		}
	}
	root := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"), manifestWithFiles())
	conformance := filepath.Join(root, "specification", "conformance")
	if err := os.MkdirAll(conformance, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("evidence.tsv", filepath.Join(conformance, "evidence.tsv")); err != nil {
		t.Fatal(err)
	}
	if err := run(root); err == nil {
		t.Fatal("run accepted a cyclic evidence symlink")
	}
}

type closeFailingInput struct {
	inputFile
	err error
}

func (input closeFailingInput) Close() error {
	_ = input.inputFile.Close()
	return input.err
}

type memoryInput struct {
	*strings.Reader
	closeErr error
}

func (input memoryInput) Close() error { return input.closeErr }

func TestRunInjectsSourceLifecycleFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		secondOpen bool
		oversized  bool
		closeOpen  int
	}{
		{name: "close normative source", closeOpen: 1},
		{name: "reopen source", secondOpen: true},
		{name: "extract object fields", oversized: true},
		{name: "close object field source", closeOpen: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, source := validSpecmatrixRoot(t)
			want := errors.New(test.name)
			sourceOpens := 0
			opener := func(path string) (inputFile, error) {
				if path != source {
					return os.Open(path)
				}
				sourceOpens++
				if test.secondOpen && sourceOpens == 2 {
					return nil, want
				}
				if test.oversized && sourceOpens == 2 {
					return memoryInput{Reader: strings.NewReader(
						strings.Repeat("x", 1024*1024+1),
					)}, nil
				}
				file, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				if sourceOpens == test.closeOpen {
					return closeFailingInput{inputFile: file, err: want}, nil
				}
				return file, nil
			}
			if err := runWith(root, opener, writeAtomic); err == nil {
				t.Fatal("runWith() error = nil")
			}
		})
	}
}

func TestRunInjectsInitialEvidenceWriteFailure(t *testing.T) {
	t.Parallel()

	root, _ := validSpecmatrixRoot(t)
	want := errors.New("evidence write")
	writeOutput := func(
		destination string,
		write func(io.Writer) error,
	) error {
		if filepath.Base(destination) == "evidence.tsv" {
			return want
		}
		return writeAtomic(destination, write)
	}
	if err := runWith(root, openInput, writeOutput); !errors.Is(err, want) {
		t.Fatalf("evidence write error = %v", err)
	}
}

type failingSpecmatrixWriter struct{ err error }

func (writer failingSpecmatrixWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func TestRunPropagatesEveryInventoryEncoderFailure(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"normative.tsv", "object-fields.tsv", "evidence.tsv",
	} {
		t.Run(target, func(t *testing.T) {
			root, _ := validSpecmatrixRoot(t)
			want := errors.New(target)
			writeOutput := func(
				destination string,
				write func(io.Writer) error,
			) error {
				if filepath.Base(destination) == target {
					return write(failingSpecmatrixWriter{err: want})
				}
				return nil
			}
			if err := runWith(
				root, openInput, writeOutput,
			); !errors.Is(err, want) {
				t.Fatalf("%s encoder error = %v", target, err)
			}
		})
	}
}

type closeFailingOutput struct {
	bytes.Buffer
	name string
	err  error
}

func (output *closeFailingOutput) Close() error { return output.err }
func (output *closeFailingOutput) Name() string { return output.name }

func TestWriteAtomicInjectsCloseFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("close output")
	output := &closeFailingOutput{
		name: filepath.Join(t.TempDir(), "temporary"), err: want,
	}
	err := writeAtomicWith(
		filepath.Join(t.TempDir(), "destination"),
		func(writer io.Writer) error {
			_, writeErr := io.WriteString(writer, "content")
			return writeErr
		},
		func(string, string) (temporaryOutput, error) { return output, nil },
	)
	if !errors.Is(err, want) {
		t.Fatalf("close output error = %v", err)
	}
}

func TestWriteAtomicPropagatesWriterAndFilesystemFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("write failed")
	root := t.TempDir()
	if err := writeAtomic(filepath.Join(root, "failed.tsv"), func(io.Writer) error {
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("writer error = %v", err)
	}
	if err := writeAtomic(filepath.Join(root, "missing", "failed.tsv"), func(io.Writer) error {
		return nil
	}); err == nil {
		t.Fatal("writeAtomic accepted a missing parent")
	}
	destination := filepath.Join(root, "directory")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(destination, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "content")
		return err
	}); err == nil {
		t.Fatal("writeAtomic replaced a directory")
	}
}

func TestExecuteReportsStatusWithoutExitingProcess(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	if status := execute(t.TempDir(), &stderr); status != 1 || stderr.Len() == 0 {
		t.Fatalf("failed execute status = %d, stderr = %q", status, stderr.String())
	}
	root := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"), manifestWithFiles())
	stderr.Reset()
	if status := execute(root, &stderr); status != 0 || stderr.Len() != 0 {
		t.Fatalf("successful execute status = %d, stderr = %q", status, stderr.String())
	}
}

func TestMainDelegatesToExecute(t *testing.T) {
	root := t.TempDir()
	writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"), manifestWithFiles())
	priorRoot := *rootFlag
	priorExit := exitProcess
	t.Cleanup(func() {
		*rootFlag = priorRoot
		exitProcess = priorExit
	})
	*rootFlag = root
	status := -1
	exitProcess = func(code int) { status = code }
	main()
	if status != 0 {
		t.Fatalf("main exit status = %d", status)
	}
}

func manifestWithFiles(files ...string) string {
	return fmt.Sprintf(`{"openapiSpecification":{"files":[%s]}}`, strings.Join(files, ","))
}

func writeSpecmatrixFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func validSpecmatrixRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "specification", "oas", "3.2", "3.2.0.md")
	writeSpecmatrixFile(t, filepath.Join(root, "specification", "manifest.json"),
		manifestWithFiles(`{"version":"3.2.0","path":"oas/3.2/3.2.0.md"}`))
	writeSpecmatrixFile(t, source, `# Specification

## Info Object

| Field Name | Type | Description |
| --- | --- | --- |
| title | string | REQUIRED. A title. |

The title MUST be present.
`)
	return root, source
}
