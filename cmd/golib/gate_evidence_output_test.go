//go:build !windows

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateEvidenceSurvivesOutputConsumerDisconnect(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.5\n")
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
printf 'stable-input\n'
`)
	writeFile(t, filepath.Join(repository, "scripts", "check-module.sh"), `#!/bin/sh
set -eu
printf 'gate started\n'
sleep 0.1
printf 'gate completed\n'
`)
	for _, path := range []string{
		"scripts/gate-input-digest.sh",
		"scripts/check-module.sh",
	} {
		if err := os.Chmod(filepath.Join(repository, path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runGit := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	runGit("init", "--initial-branch=main")
	runGit("config", "user.email", "golib@example.test")
	runGit("config", "user.name", "golib")
	runGit("add", "go.mod", "scripts")
	runGit("commit", "-m", "initial")

	runner := filepath.Join(root, "scripts", "run-gate-with-evidence.sh")
	command := exec.Command(runner, ".", "test")
	command.Dir = repository
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read first gate output: %v", err)
	}
	if line != "gate started\n" {
		t.Fatalf("first gate output = %q", line)
	}
	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("evidence runner after output disconnect: %v", err)
	}

	evidencePath := filepath.Join(
		repository,
		".artifacts",
		"evidence",
		"test.json",
	)
	var evidence struct {
		Result   string `json:"result"`
		ExitCode int    `json:"exit_code"`
	}
	decodeJSONFile(t, evidencePath, &evidence)
	if evidence.Result != "passed" || evidence.ExitCode != 0 {
		t.Fatalf("evidence after output disconnect = %+v", evidence)
	}
	log, err := os.ReadFile(filepath.Join(repository, ".artifacts", "evidence", "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(log) != "gate started\ngate completed\n" {
		t.Fatalf("persisted gate log = %q", log)
	}
}
