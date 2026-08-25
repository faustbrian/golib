package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestStandaloneSafetyProgramMatchesRepositoryPolicy(t *testing.T) {
	directory := t.TempDir()
	program := filepath.Join(directory, "safety.go")
	if err := os.WriteFile(program, []byte(standaloneSafetyProgram), 0o644); err != nil {
		t.Fatal(err)
	}

	safe := filepath.Join(directory, "safe")
	if err := os.MkdirAll(safe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(safe, "main.go"),
		[]byte("package safe\nimport \"os\"\nfunc Exit() { os.Exit(1) }\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", program, safe)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("safe package rejected: %v\n%s", err, output)
	}

	unsafePackage := filepath.Join(directory, "unsafe")
	if err := os.MkdirAll(unsafePackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(unsafePackage, "unsafe.go"),
		[]byte("package unsafepackage\nimport \"unsafe\"\nvar _ unsafe.Pointer\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("go", "run", program, unsafePackage)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("unsafe package was accepted")
	}
	if !strings.Contains(string(output), `forbidden import "unsafe"`) {
		t.Fatalf("unsafe failure did not identify the import:\n%s", output)
	}
}
