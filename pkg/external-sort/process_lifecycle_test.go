package externalsort

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const (
	processHelperEnvironment = "EXTERNAL_SORT_PROCESS_HELPER"
	processHelperParent      = "EXTERNAL_SORT_PROCESS_PARENT"
)

func TestProcessLifecycleHelper(t *testing.T) {
	mode := os.Getenv(processHelperEnvironment)
	if mode == "" {
		return
	}
	parent := os.Getenv(processHelperParent)
	var restoreUmask func()
	if mode == "umask" {
		restoreUmask = setRestrictiveUmask()
	}
	factory, err := NewFactory(Config{
		ParentDirectory: parent,
		RecordBytes:     4,
		ChunkRecords:    1,
		MaximumRecords:  1,
	})
	if err != nil {
		os.Exit(2)
	}
	store, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	)
	if restoreUmask != nil {
		restoreUmask()
	}
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		os.Exit(3)
	}
	if mode == "umask" {
		info, statErr := os.Stat(store.directory)
		if statErr != nil || info.Mode().Perm() != 0o700 {
			os.Exit(7)
		}
		if closeErr := store.Close(); closeErr != nil {
			os.Exit(8)
		}

		os.Exit(0)
	}
	if err := store.Add(context.Background(), []byte{1, 2, 3, 4}); err != nil {
		os.Exit(4)
	}
	terminated := make(chan os.Signal, 1)
	if mode != "kill" {
		signal.Notify(terminated, syscall.SIGTERM)
	}
	if err := os.WriteFile(filepath.Join(parent, "ready"), nil, 0o600); err != nil {
		os.Exit(5)
	}
	if mode == "kill" {
		select {}
	}

	<-terminated
	signal.Stop(terminated)
	if err := store.Close(); err != nil {
		os.Exit(6)
	}
	os.Exit(0)
}

func TestCallerLifecycleHandlesPodTerminationAndGraceExpiry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM and SIGKILL lifecycle semantics require Unix")
	}

	t.Run("SIGTERM cleanup completes within grace deadline", func(t *testing.T) {
		parent := ownerOnlyTemporaryDirectory(t)
		command := startProcessHelper(t, parent, "term")
		workDirectory := waitForProcessHelper(t, parent)
		started := time.Now()
		if err := command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("Signal(SIGTERM) error = %v", err)
		}
		if err := waitForProcess(command, 2*time.Second); err != nil {
			t.Fatalf("SIGTERM helper exit error = %v", err)
		}
		if elapsed := time.Since(started); elapsed >= 2*time.Second {
			t.Fatalf("SIGTERM cleanup elapsed = %v, want less than 2s", elapsed)
		}
		if _, err := os.Stat(workDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("SIGTERM cleanup residue = %v", err)
		}
	})

	t.Run("grace expiry leaves declared residue for volume reuse janitor", func(t *testing.T) {
		parent := ownerOnlyTemporaryDirectory(t)
		command := startProcessHelper(t, parent, "kill")
		workDirectory := waitForProcessHelper(t, parent)
		if err := command.Process.Kill(); err != nil {
			t.Fatalf("Kill() error = %v", err)
		}
		if err := command.Wait(); err == nil {
			t.Fatal("SIGKILL helper exited successfully")
		}
		if _, err := os.Stat(workDirectory); err != nil {
			t.Fatalf("SIGKILL residue missing: %v", err)
		}
		if err := os.RemoveAll(workDirectory); err != nil {
			t.Fatalf("controlled test residue cleanup error = %v", err)
		}
		factory := newTestFactory(t, parent, 1, 1, 1)
		store := openTestStore(t, factory)
		if err := store.Add(context.Background(), []byte{1}); err != nil {
			t.Fatalf("Add() after volume reuse error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() after volume reuse error = %v", err)
		}
	})
}

func TestFactoryOverridesARestrictiveUmaskForUsableOwnerOnlyStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("umask semantics require Unix")
	}

	command := exec.Command(os.Args[0], "-test.run=^TestProcessLifecycleHelper$")
	command.Env = append(
		os.Environ(),
		processHelperEnvironment+"=umask",
		processHelperParent+"="+ownerOnlyTemporaryDirectory(t),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restrictive-umask helper error = %v, output = %q", err, output)
	}
}

func startProcessHelper(t *testing.T, parent string, mode string) *exec.Cmd {
	t.Helper()

	command := exec.Command(os.Args[0], "-test.run=^TestProcessLifecycleHelper$")
	command.Env = append(
		os.Environ(),
		processHelperEnvironment+"="+mode,
		processHelperParent+"="+parent,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	return command
}

func waitForProcessHelper(t *testing.T, parent string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(parent, "ready")); err == nil {
			entries, readErr := os.ReadDir(parent)
			if readErr != nil {
				t.Fatalf("ReadDir() error = %v", readErr)
			}
			for _, entry := range entries {
				if entry.IsDir() && len(entry.Name()) > len(storePrefix) &&
					entry.Name()[:len(storePrefix)] == storePrefix {
					return filepath.Join(parent, entry.Name())
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process helper did not become ready")

	return ""
}

func waitForProcess(command *exec.Cmd, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- command.Wait() }()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-result

		return errors.New("process did not exit before deadline")
	}
}
