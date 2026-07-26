package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExecutableBoundaryMapsOutputStatusAndTermination(t *testing.T) {
	t.Parallel()

	binary := filepath.Join(t.TempDir(), "process-fixture")
	// #nosec G204 -- the command and output path are fixed test inputs.
	build := exec.CommandContext(
		t.Context(), "go", "build", "-trimpath", "-o", binary, "./cmd/process-fixture",
	)
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build process fixture: %v\n%s", err, output)
	}

	t.Run("success", func(t *testing.T) {
		stdout, stderr, status := runProcessFixture(t, binary)
		if status != 0 || stderr != "" {
			t.Fatalf("status = %d, stderr = %q", status, stderr)
		}
		assertProcessEnvelope(t, stdout, true, "")
	})

	t.Run("usage failure", func(t *testing.T) {
		stdout, stderr, status := runProcessFixture(t, binary, "--unknown")
		if status != 2 || stderr != "" {
			t.Fatalf("status = %d, stderr = %q", status, stderr)
		}
		assertProcessEnvelope(t, stdout, false, "unknown_option")
	})

	t.Run("SIGTERM", func(t *testing.T) {
		// #nosec G204 -- binary is built into this test's private temporary directory.
		command := exec.CommandContext(t.Context(), binary, "--await-signal")
		var stdout bytes.Buffer
		command.Stdout = &stdout
		stderr, err := command.StderrPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		ready := make(chan string, 1)
		go func() {
			line, _ := bufio.NewReader(stderr).ReadString('\n')
			ready <- line
		}()
		select {
		case line := <-ready:
			if line != "ready\n" {
				t.Fatalf("readiness = %q", line)
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			t.Fatal("process fixture did not become ready")
		}
		if err := command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		err = command.Wait()
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 130 {
			t.Fatalf("SIGTERM wait error = %v", err)
		}
		assertProcessEnvelope(t, stdout.String(), false, "canceled")
	})
}

func runProcessFixture(t *testing.T, binary string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- binary is built into the caller's private temporary directory.
	command := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("run fixture: %v", err)
	}
	return stdout.String(), stderr.String(), exitError.ExitCode()
}

func assertProcessEnvelope(t *testing.T, raw string, ok bool, kind string) {
	t.Helper()
	var envelope struct {
		Schema string `json:"schema"`
		OK     bool   `json:"ok"`
		Error  struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode process output %q: %v", raw, err)
	}
	if envelope.Schema != "go-cli/v1" || envelope.OK != ok || envelope.Error.Kind != kind {
		t.Fatalf("process envelope = %#v", envelope)
	}
}
