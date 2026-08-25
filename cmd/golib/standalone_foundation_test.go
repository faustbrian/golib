package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallStandaloneLintConfiguration(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	configuration := filepath.Join(source, "pkg", "fixture", ".golangci.yml")
	if err := os.MkdirAll(filepath.Dir(configuration), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configuration, []byte("version: \"2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	preserve, err := installStandaloneLintConfiguration(
		source,
		destination,
		standaloneRepository{Family: "fixture", Name: "go-fixture"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !preserve {
		t.Fatal("package-owned lint configuration was not preserved")
	}
	contents, err := os.ReadFile(filepath.Join(destination, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "version: \"2\"\n" {
		t.Fatalf("copied lint configuration = %q", contents)
	}

	preserve, err = installStandaloneLintConfiguration(
		source,
		destination,
		standaloneRepository{Family: "missing", Name: "go-missing"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preserve {
		t.Fatal("missing package lint configuration was preserved")
	}
}
