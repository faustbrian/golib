//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIEvidenceStagingExcludesDisposableState(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	source := filepath.Join(repository, ".artifacts", "pkg", "example")
	for path, contents := range map[string]string{
		"mutation-checkpoints/root.json":             "checkpoint\n",
		"evidence/by-input/test/digest.json":         "evidence\n",
		"mutation.json":                              "aggregate\n",
		"mutation-run-dead/root.go-cache-dead/cache": "scratch\n",
		"evidence/.locks/test.lock/owner":            "lock\n",
		"coverage.out.tmp.interrupted":               "partial\n",
		"tooling/build.lock/owner":                   "lock owner\n",
		"report.tmp.interrupted/partial":             "partial directory\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(source, path), contents)
	}
	destination := filepath.Join(t.TempDir(), "staged")
	command := exec.Command(
		filepath.Join(root, "scripts", "stage-ci-evidence.sh"),
		"pkg/example",
		destination,
		"success",
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("stage CI evidence: %v\n%s", err, output)
	}

	for _, path := range []string{
		"mutation-checkpoints/root.json",
		"evidence/by-input/test/digest.json",
		"mutation.json",
	} {
		if _, err := os.Stat(filepath.Join(destination, path)); err != nil {
			t.Fatalf("durable artifact %s was not staged: %v", path, err)
		}
	}
	for _, path := range []string{
		"mutation-run-dead",
		"evidence/.locks",
		"coverage.out.tmp.interrupted",
		"tooling/build.lock",
		"report.tmp.interrupted",
	} {
		if _, err := os.Stat(filepath.Join(destination, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disposable artifact %s was staged: %v", path, err)
		}
	}
}

func TestCIEvidenceStagingRejectsSymlinks(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	source := filepath.Join(repository, ".artifacts", "pkg", "example")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	writeTestFile(t, target, "outside\n")
	if err := os.Symlink(target, filepath.Join(source, "escaped")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "staged")
	command := exec.Command(
		filepath.Join(root, "scripts", "stage-ci-evidence.sh"),
		"pkg/example",
		destination,
		"success",
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("staging accepted a symlink:\n%s", output)
	}
	if !strings.Contains(string(output), "refusing to stage symbolic link") {
		t.Fatalf("unexpected symlink rejection: %v\n%s", err, output)
	}
}

func TestCIEvidenceStagingRecoversAbandonedMutationScratch(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	source := filepath.Join(repository, ".artifacts", "pkg", "example")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "mutation.json"), "aggregate\n")
	run := filepath.Join(source, "mutation-run-abandoned")
	if err := os.MkdirAll(filepath.Join(run, "root.go-cache-abandoned"), 0o700); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	owner := "1\t" + host + "\t99999999\tMon Jan  1 00:00:00 2001\t" + filepath.Base(run) + "\n"
	writeTestFile(t, filepath.Join(run, ".mutation-owner"), owner)
	destination := filepath.Join(t.TempDir(), "staged")
	command := exec.Command(
		filepath.Join(root, "scripts", "stage-ci-evidence.sh"),
		"pkg/example",
		destination,
		"success",
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	if output, commandErr := command.CombinedOutput(); commandErr != nil {
		t.Fatalf("stage CI evidence: %v\n%s", commandErr, output)
	}
	if _, statErr := os.Stat(run); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("abandoned mutation scratch survived staging: %v", statErr)
	}
}

func TestCIEvidenceStagingRejectsDestinationWithinArtifactTree(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	source := filepath.Join(repository, ".artifacts", "pkg", "example")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "mutation.json"), "aggregate\n")
	destination := filepath.Join(source, "staged")
	command := exec.Command(
		filepath.Join(root, "scripts", "stage-ci-evidence.sh"),
		"pkg/example",
		destination,
		"success",
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("staging accepted a destination inside its source:\n%s", output)
	}
	if !strings.Contains(string(output), "must be outside its source") {
		t.Fatalf("unexpected destination rejection: %v\n%s", err, output)
	}
}

func TestCIUploadsOnlyStagedDurableEvidence(t *testing.T) {
	root := testRepositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(workflow)
	for _, required := range []string{
		"Stage attributable evidence",
		`./scripts/stage-ci-evidence.sh '${{ matrix.directory }}'`,
		`path: ${{ format('{0}/golib-evidence-{1}', runner.temp, matrix.artifact) }}`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("CI evidence upload lacks %q", required)
		}
	}
	if strings.Contains(contract, `path: ${{ matrix.directory == '.' && '.artifacts'`) {
		t.Fatal("CI uploads the mutable artifact tree directly")
	}
}
