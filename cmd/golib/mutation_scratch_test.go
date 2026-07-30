//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMutationScratchRemovesCompleteRunAfterSuccess(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	script := filepath.Join(root, "scripts", "internal", "mutation-scratch.sh")
	command := exec.Command(
		"bash",
		"-c",
		`set -euo pipefail
source "$1"
mutation_scratch_initialize "$2"
mkdir -p "${run_directory}/root.go-cache" \
    "${run_directory}/historical-inputs/revision"
printf '%s\n' scratch >"${run_directory}/root.json"
mkdir -p "${artifact}/mutation-checkpoints"
printf '%s\n' persistent >"${artifact}/mutation-checkpoints/root.json"
`,
		"mutation-scratch-test",
		script,
		artifact,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("successful mutation scratch run: %v\n%s", err, output)
	}

	assertNoMutationRunDirectories(t, artifact)
	report, err := os.ReadFile(
		filepath.Join(artifact, "mutation-checkpoints", "root.json"),
	)
	if err != nil {
		t.Fatalf("read persistent checkpoint: %v", err)
	}
	if strings.TrimSpace(string(report)) != "persistent" {
		t.Fatalf("persistent checkpoint = %q, want persistent", report)
	}
}

func TestMutationScratchRemovesCompleteRunAfterFailure(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	command := mutationScratchCommand(t, root, artifact, `
mkdir -p "${run_directory}/root.go-cache" \
    "${run_directory}/historical-inputs/revision"
printf '%s\n' scratch >"${run_directory}/root.json"
mkdir -p "${artifact}/mutation-checkpoints"
printf '%s\n' persistent >"${artifact}/mutation-checkpoints/root.json"
exit 23
`)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("failing mutation scratch run succeeded:\n%s", output)
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != 23 {
		t.Fatalf("failing mutation scratch exit = %v, want status 23\n%s", err, output)
	}

	assertNoMutationRunDirectories(t, artifact)
	if _, err := os.Stat(
		filepath.Join(artifact, "mutation-checkpoints", "root.json"),
	); err != nil {
		t.Fatalf("persistent checkpoint after failure: %v", err)
	}
}

func TestMutationScratchRemovesCompleteRunAfterInterruption(t *testing.T) {
	for _, test := range []struct {
		name   string
		signal syscall.Signal
		status int
	}{
		{name: "HUP", signal: syscall.SIGHUP, status: 129},
		{name: "INT", signal: syscall.SIGINT, status: 130},
		{name: "TERM", signal: syscall.SIGTERM, status: 143},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testRepositoryRoot(t)
			artifact := t.TempDir()
			ready := filepath.Join(artifact, "ready")
			command := mutationScratchCommand(t, root, artifact, `
mkdir -p "${run_directory}/root.go-cache" \
    "${run_directory}/historical-inputs/revision"
printf '%s\n' "${run_directory}" >"$3"
while :; do
    sleep 1
done
`, ready)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatalf("start mutation scratch process: %v", err)
			}
			waitForTestFile(t, ready)
			if err := syscall.Kill(-command.Process.Pid, test.signal); err != nil {
				t.Fatalf("interrupt mutation scratch process: %v", err)
			}
			err := command.Wait()
			exitError, ok := err.(*exec.ExitError)
			if !ok || exitError.ExitCode() != test.status {
				t.Fatalf(
					"interrupted mutation scratch exit = %v, want status %d",
					err,
					test.status,
				)
			}
			assertNoMutationRunDirectories(t, artifact)
		})
	}
}

func TestMutationScratchRecoversOnlyAbandonedOwnedRuns(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	ready := filepath.Join(artifact, "ready")
	command := mutationScratchCommand(t, root, artifact, `
printf '%s\n' "${run_directory}" >"$3"
exec sleep 300
`, ready)
	if err := command.Start(); err != nil {
		t.Fatalf("start abandoned mutation scratch process: %v", err)
	}
	runDirectory := strings.TrimSpace(string(waitForTestFile(t, ready)))
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill abandoned mutation scratch process: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed mutation scratch process exited successfully")
	}
	if _, err := os.Stat(runDirectory); err != nil {
		t.Fatalf("abandoned mutation scratch directory: %v", err)
	}

	recoverMutationScratch(t, root, artifact)
	assertNoMutationRunDirectories(t, artifact)
}

func TestMutationScratchRecoveryClaimsAbandonedRunOnce(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	ready := filepath.Join(artifact, "ready")
	owner := mutationScratchCommand(t, root, artifact, `
printf '%s\n' "${run_directory}" >"$3"
exec sleep 300
`, ready)
	if err := owner.Start(); err != nil {
		t.Fatalf("start abandoned mutation scratch process: %v", err)
	}
	waitForTestFile(t, ready)
	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("kill abandoned mutation scratch process: %v", err)
	}
	if err := owner.Wait(); err == nil {
		t.Fatal("killed mutation scratch process exited successfully")
	}

	recoveries := []*exec.Cmd{
		mutationScratchRecoveryCommand(t, root, artifact),
		mutationScratchRecoveryCommand(t, root, artifact),
	}
	outputs := make([]bytes.Buffer, len(recoveries))
	for index, recovery := range recoveries {
		recovery.Stdout = &outputs[index]
		recovery.Stderr = &outputs[index]
		if err := recovery.Start(); err != nil {
			t.Fatalf("start mutation scratch recovery %d: %v", index, err)
		}
	}
	for index, recovery := range recoveries {
		if err := recovery.Wait(); err != nil {
			t.Fatalf(
				"mutation scratch recovery %d: %v\n%s",
				index,
				err,
				outputs[index].Bytes(),
			)
		}
	}
	assertNoMutationRunDirectories(t, artifact)
}

func TestMutationScratchRecoveryPreservesConcurrentActiveRuns(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	type activeRun struct {
		command *exec.Cmd
		path    string
	}
	runs := make([]activeRun, 0, 2)
	for index := range 2 {
		ready := filepath.Join(artifact, fmt.Sprintf("ready-%d", index))
		command := mutationScratchCommand(t, root, artifact, `
printf '%s\n' "${run_directory}" >"$3"
while :; do
    sleep 1
done
`, ready)
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			t.Fatalf("start active mutation scratch process %d: %v", index, err)
		}
		runs = append(runs, activeRun{
			command: command,
			path:    strings.TrimSpace(string(waitForTestFile(t, ready))),
		})
	}
	defer func() {
		for _, run := range runs {
			if run.command.ProcessState == nil {
				_ = syscall.Kill(-run.command.Process.Pid, syscall.SIGTERM)
				_ = run.command.Wait()
			}
		}
	}()

	recoverMutationScratch(t, root, artifact)
	if runs[0].path == runs[1].path {
		t.Fatalf("concurrent mutation runs share %s", runs[0].path)
	}
	for _, run := range runs {
		if _, err := os.Stat(run.path); err != nil {
			t.Fatalf("active mutation scratch directory was removed: %v", err)
		}
	}
	for _, run := range runs {
		if err := syscall.Kill(-run.command.Process.Pid, syscall.SIGTERM); err != nil {
			t.Fatalf("stop active mutation scratch process: %v", err)
		}
		if err := run.command.Wait(); err == nil {
			t.Fatal("interrupted active mutation scratch process exited successfully")
		}
	}
	assertNoMutationRunDirectories(t, artifact)
}

func TestMutationScratchCreatesIsolatedPackageCaches(t *testing.T) {
	root := testRepositoryRoot(t)
	artifact := t.TempDir()
	cacheRecord := filepath.Join(artifact, "package-caches")
	command := mutationScratchCommand(t, root, artifact, `
mutation_scratch_package_cache root
root_cache="${active_build_cache}"
mutation_scratch_package_cache adapters-goqueue
adapter_cache="${active_build_cache}"
test "${root_cache}" != "${adapter_cache}"
test -d "${root_cache}"
test -d "${adapter_cache}"
printf '%s\n%s\n' "${root_cache}" "${adapter_cache}" >"$3"
`, cacheRecord)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create isolated package caches: %v\n%s", err, output)
	}
	cachePaths := strings.Fields(string(waitForTestFile(t, cacheRecord)))
	if len(cachePaths) != 2 {
		t.Fatalf("package cache paths = %q, want two", cachePaths)
	}
	for _, cachePath := range cachePaths {
		if !strings.HasPrefix(cachePath, artifact+string(filepath.Separator)) {
			t.Fatalf("package cache escaped mutation artifact: %s", cachePath)
		}
		if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
			t.Fatalf("package cache survived run cleanup: %s: %v", cachePath, err)
		}
	}
	assertNoMutationRunDirectories(t, artifact)
}

func mutationScratchCommand(
	t *testing.T,
	root string,
	artifact string,
	body string,
	arguments ...string,
) *exec.Cmd {
	t.Helper()
	script := filepath.Join(root, "scripts", "internal", "mutation-scratch.sh")
	harness := `set -euo pipefail
source "$1"
mutation_scratch_initialize "$2"
` + body
	commandArguments := []string{
		"-c",
		harness,
		"mutation-scratch-test",
		script,
		artifact,
	}
	commandArguments = append(commandArguments, arguments...)
	return exec.Command("bash", commandArguments...)
}

func recoverMutationScratch(t *testing.T, root string, artifact string) {
	t.Helper()
	command := mutationScratchRecoveryCommand(t, root, artifact)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("recover abandoned mutation scratch: %v\n%s", err, output)
	}
}

func mutationScratchRecoveryCommand(
	t *testing.T,
	root string,
	artifact string,
) *exec.Cmd {
	t.Helper()
	script := filepath.Join(root, "scripts", "internal", "mutation-scratch.sh")
	return exec.Command(
		"bash",
		"-c",
		`set -euo pipefail
source "$1"
mutation_scratch_recover_abandoned "$2"
`,
		"mutation-scratch-recovery-test",
		script,
		artifact,
	)
}

func waitForTestFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil && len(contents) > 0 {
			return contents
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read process readiness file: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}

func assertNoMutationRunDirectories(t *testing.T, artifact string) {
	t.Helper()
	entries, err := os.ReadDir(artifact)
	if err != nil {
		t.Fatalf("read mutation artifact directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "mutation-run-") {
			t.Fatalf("mutation scratch directory remains: %s", entry.Name())
		}
	}
}
