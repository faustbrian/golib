package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var errManifestBoundary = errors.New("manifest boundary failed")

type manifestTestFile struct {
	writes    int
	failWrite int
	closeErr  error
}

func (file *manifestTestFile) Write(data []byte) (int, error) {
	file.writes++
	if file.writes == file.failWrite {
		return 0, errManifestBoundary
	}
	return len(data), nil
}

func (*manifestTestFile) Name() string { return "temporary-manifest" }

func (file *manifestTestFile) Close() error { return file.closeErr }

func TestGenerateReproducesCommittedManifest(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", ".."))
	output := filepath.Join(t.TempDir(), "manifest.tsv")
	if err := generate(output); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- output is inside this test's temporary directory.
	generated, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(
		filepath.Join("specification", "official-suite-results.tsv"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != string(committed) {
		t.Fatal("generated manifest differs from committed evidence")
	}
}

func TestFixturePathsReturnsSortedJSONFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := fixturePaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{
		filepath.Join(root, "a.json"),
		filepath.Join(root, "b.json"),
	}) {
		t.Fatalf("unexpected paths %#v", paths)
	}
	if _, err := fixturePaths(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected missing fixture root error")
	}
}

func TestSuiteRevisionRequiresDeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suite.env")
	if err := os.WriteFile(path, []byte("OTHER=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := suiteRevision(path); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("got %v, want missing declaration", err)
	}
	if _, err := suiteRevision(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing file error")
	}
	if err := os.WriteFile(path, []byte("SUITE_REVISION=abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision, err := suiteRevision(path)
	if err != nil || revision != "abc123" {
		t.Fatalf("got %q, %v", revision, err)
	}
}

func TestManifestGeneratorPropagatesEveryIOFailure(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		configure func(*manifestGenerator, *manifestTestFile)
	}{
		{name: "revision", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.revision = func(string) (string, error) { return "", errManifestBoundary }
		}},
		{name: "create temporary", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.createTemp = func(string, string) (manifestFile, error) { return nil, errManifestBoundary }
		}},
		{name: "header write", configure: func(_ *manifestGenerator, file *manifestTestFile) {
			file.failWrite = 1
		}},
		{name: "fixture paths", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.fixturePaths = func(string) ([]string, error) { return nil, errManifestBoundary }
		}},
		{name: "fixture read", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.fixturePaths = oneFixture
			generator.readFile = func(string) ([]byte, error) { return nil, errManifestBoundary }
		}},
		{name: "fixture parse", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.fixturePaths = oneFixture
			generator.readFile = func(string) ([]byte, error) { return []byte(`{`), nil }
		}},
		{name: "relative path", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.fixturePaths = oneFixture
			generator.relativePath = func(string, string) (string, error) { return "", errManifestBoundary }
		}},
		{name: "row write", configure: func(generator *manifestGenerator, file *manifestTestFile) {
			generator.fixturePaths = oneFixture
			file.failWrite = 2
		}},
		{name: "close", configure: func(_ *manifestGenerator, file *manifestTestFile) {
			file.closeErr = errManifestBoundary
		}},
		{name: "rename", configure: func(generator *manifestGenerator, _ *manifestTestFile) {
			generator.rename = func(string, string) error { return errManifestBoundary }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := &manifestTestFile{}
			generator := testManifestGenerator(file)
			test.configure(&generator, file)
			err := generator.generate("manifest.tsv")
			if err == nil || test.name != "fixture parse" && !errors.Is(err, errManifestBoundary) {
				t.Fatalf("got %v, want boundary failure", err)
			}
		})
	}
}

func TestManifestCommandReportsParsingAndGenerationFailures(t *testing.T) {
	if code := run([]string{"-unknown"}, io.Discard); code != 2 {
		t.Fatalf("got parse exit %d, want 2", code)
	}

	previousArgs := commandArgs
	previousErrorOutput := commandErrorOutput
	previousGenerate := generateManifest
	previousExit := exitProcess
	t.Cleanup(func() {
		commandArgs = previousArgs
		commandErrorOutput = previousErrorOutput
		generateManifest = previousGenerate
		exitProcess = previousExit
	})

	commandArgs = []string{"-output", "manifest.tsv"}
	var stderr bytes.Buffer
	commandErrorOutput = &stderr
	generateManifest = func(string) error { return errManifestBoundary }
	exitCode := 0
	exitProcess = func(code int) { exitCode = code }
	main()
	if exitCode != 1 || !strings.Contains(stderr.String(), errManifestBoundary.Error()) {
		t.Fatalf("got exit=%d stderr=%q", exitCode, stderr.String())
	}

	generateManifest = func(string) error { return nil }
	if code := run(nil, io.Discard); code != 0 {
		t.Fatalf("got success exit %d, want 0", code)
	}
}

func testManifestGenerator(file manifestFile) manifestGenerator {
	return manifestGenerator{
		revision: func(string) (string, error) { return "revision", nil },
		createTemp: func(string, string) (manifestFile, error) {
			return file, nil
		},
		fixturePaths: func(string) ([]string, error) { return nil, nil },
		readFile: func(string) ([]byte, error) {
			return []byte(`[{"tests":[true]}]`), nil
		},
		relativePath: func(string, string) (string, error) {
			return "fixture.json", nil
		},
		rename: func(string, string) error { return nil },
		remove: func(string) error { return nil },
	}
}

func oneFixture(string) ([]string, error) {
	return []string{"fixture.json"}, nil
}
