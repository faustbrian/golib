//go:build !windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopServicesBoundsHungDockerCleanup(t *testing.T) {
	root := testRepositoryRoot(t)
	bin := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "docker.log")
	stateFile := filepath.Join(t.TempDir(), "services")
	docker := filepath.Join(bin, "docker")
	writeTestFile(t, docker, `#!/bin/sh
if [ "$3" = "hung" ]; then
    exec sleep 30
fi
printf '%s\n' "$3" >>"$FAKE_DOCKER_LOG"
`)
	if err := os.Chmod(docker, 0o700); err != nil {
		t.Fatalf("make fake Docker executable: %v", err)
	}
	writeTestFile(t, stateFile, "hung\nhealthy\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(root, "scripts", "stop-services.sh"),
		stateFile,
	)
	command.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(command.Env, "FAKE_DOCKER_LOG", logFile)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_DOCKER_CLEANUP_TIMEOUT_SECONDS",
		"1",
	)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stop services: %v\n%s", err, result)
	}
	if !strings.Contains(string(result), "timed out removing Docker container hung") {
		t.Fatalf("stop services output = %q", result)
	}
	removed, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read Docker removal log: %v", err)
	}
	if string(removed) != "healthy\n" {
		t.Fatalf("Docker removal log = %q, want healthy", removed)
	}
}

func TestRabbitStreamServiceLifecycleExportsFixturesAndCleansOnlyOwnedResources(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	bin := filepath.Join(repository, "bin")
	fixture := filepath.Join(repository, "pkg", "rabbitstream", "rabbitmq", "integration")
	for _, directory := range []string{
		bin,
		filepath.Join(repository, ".golib"),
		filepath.Join(repository, "scripts"),
		fixture,
		filepath.Join(repository, "pkg", "example"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "required_services": ["rabbitstream"]
  }, {
    "directory": "pkg/standalone",
    "required_services": ["rabbitstream-standalone"]
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, ".golib", "versions.env"), "")
	for _, script := range []string{"start-services.sh", "stop-services.sh"} {
		writeTestFile(
			t,
			filepath.Join(repository, "scripts", script),
			mustReadFile(t, filepath.Join(root, "scripts", script)),
		)
		if err := os.Chmod(filepath.Join(repository, "scripts", script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []string{"setup.sh", "standalone-setup.sh", "tls-setup.sh"} {
		writeTestFile(
			t,
			filepath.Join(fixture, script),
			mustReadFile(t, filepath.Join(root, "pkg", "rabbitstream", "rabbitmq", "integration", script)),
		)
		if err := os.Chmod(filepath.Join(fixture, script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dockerLog := filepath.Join(repository, "docker.log")
	writeTestFile(t, filepath.Join(bin, "docker"), `#!/bin/sh
set -eu
case " $* " in
    *" cp certgen:/certs/"*)
        for destination do :; done
        printf '%s\n' fixture >"$destination"
        printf '%s\n' cp >>"$FAKE_DOCKER_LOG"
        ;;
    *" down "*) printf '%s\n' down >>"$FAKE_DOCKER_LOG" ;;
    *" up "*) printf '%s\n' up >>"$FAKE_DOCKER_LOG" ;;
    *) printf '%s\n' other >>"$FAKE_DOCKER_LOG" ;;
esac
`)
	writeTestFile(t, filepath.Join(bin, "curl"), "#!/bin/sh\nprintf '%s' 201\n")
	for _, executable := range []string{"docker", "curl"} {
		if err := os.Chmod(filepath.Join(bin, executable), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}

	environmentFile := filepath.Join(repository, "environment")
	stateFile := filepath.Join(repository, "state")
	temporaryRoot := filepath.Join(repository, "tmp")
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	start := exec.Command(
		filepath.Join(repository, "scripts", "start-services.sh"),
		"pkg/example", environmentFile, stateFile,
	)
	start.Dir = repository
	start.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	start.Env = environmentWithValues(start.Env, "FAKE_DOCKER_LOG", dockerLog)
	start.Env = environmentWithValues(start.Env, "TMPDIR", temporaryRoot)
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start RabbitStream fixtures: %v\n%s", err, output)
	}
	environment := mustReadFile(t, environmentFile)
	for _, name := range []string{
		"RABBITSTREAM_TEST_HOST",
		"RABBITSTREAM_TEST_PORT",
		"RABBITSTREAM_TEST_USER",
		"RABBITSTREAM_TEST_PASSWORD",
		"RABBITSTREAM_TEST_RESTART_CONTAINER",
		"RABBITSTREAM_TEST_PROXY_API",
		"RABBITSTREAM_TEST_PROXY_NAME",
		"RABBITSTREAM_CLUSTER_PORTS",
		"RABBITSTREAM_CLUSTER_CONTAINERS",
		"RABBITSTREAM_TLS_HOST",
		"RABBITSTREAM_TLS_PORT",
		"RABBITSTREAM_TLS_USER",
		"RABBITSTREAM_TLS_PASSWORD",
		"RABBITSTREAM_TLS_RUNTIME",
		"RABBITSTREAM_RESTRICTED_USER",
		"RABBITSTREAM_RESTRICTED_PASSWORD",
	} {
		if !strings.Contains(environment, name+"=") {
			t.Errorf("service environment lacks %s", name)
		}
	}
	state := mustReadFile(t, stateFile)
	if strings.Contains(state, "codex-lb") {
		t.Fatal("service state includes pre-existing codex-lb")
	}
	if strings.Count(state, "compose\t") != 3 || !strings.Contains(state, "directory\t") ||
		!strings.Contains(state, "lock\t") {
		t.Fatalf("RabbitStream service state = %q", state)
	}
	for _, line := range strings.Split(state, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 4 && fields[0] == "compose" &&
			!strings.HasPrefix(fields[3], "codex-rabbitstream-") {
			t.Errorf("Compose project is not recognizably task-owned: %q", fields[3])
		}
	}
	logBeforeContender := mustReadFile(t, dockerLog)
	contenderContext, cancelContender := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancelContender()
	contender := exec.CommandContext(
		contenderContext,
		filepath.Join(repository, "scripts", "start-services.sh"),
		"pkg/standalone",
		filepath.Join(repository, "contender-environment"),
		filepath.Join(repository, "contender-state"),
	)
	contender.Dir = repository
	contender.Env = start.Env
	if output, err := contender.CombinedOutput(); err == nil ||
		!errors.Is(contenderContext.Err(), context.DeadlineExceeded) {
		t.Fatalf("concurrent RabbitStream fixture start = %v, want lock wait deadline\n%s", err, output)
	}
	if logAfterContender := mustReadFile(t, dockerLog); logAfterContender != logBeforeContender {
		t.Fatalf("concurrent fixture started Docker resources: %q", logAfterContender)
	}

	stop := exec.Command(filepath.Join(repository, "scripts", "stop-services.sh"), stateFile)
	stop.Dir = repository
	stop.Env = environmentWithValues(start.Env, "GOLIB_DOCKER_CLEANUP_TIMEOUT_SECONDS", "2")
	if output, err := stop.CombinedOutput(); err != nil {
		t.Fatalf("stop RabbitStream fixtures: %v\n%s", err, output)
	}
	log := mustReadFile(t, dockerLog)
	if strings.Count(log, "up\n") != 3 || strings.Count(log, "down\n") != 3 {
		t.Fatalf("Docker lifecycle log = %q", log)
	}
	for _, line := range strings.Split(state, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 2 && (fields[0] == "directory" || fields[0] == "lock") {
			if _, err := os.Stat(fields[1]); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("owned %s remains after cleanup: %v", fields[0], err)
			}
		}
	}

	standaloneEnvironmentFile := filepath.Join(repository, "standalone-environment")
	standaloneStateFile := filepath.Join(repository, "standalone-state")
	standaloneStart := exec.Command(
		filepath.Join(repository, "scripts", "start-services.sh"),
		"pkg/standalone", standaloneEnvironmentFile, standaloneStateFile,
	)
	standaloneStart.Dir = repository
	standaloneStart.Env = start.Env
	if output, err := standaloneStart.CombinedOutput(); err != nil {
		t.Fatalf("start standalone RabbitStream fixture: %v\n%s", err, output)
	}
	standaloneEnvironment := mustReadFile(t, standaloneEnvironmentFile)
	if strings.Contains(standaloneEnvironment, "RABBITSTREAM_TLS_HOST=") {
		t.Fatal("standalone fixture unexpectedly exported TLS state")
	}
	if state := mustReadFile(t, standaloneStateFile); strings.Count(state, "compose\t") != 1 {
		t.Fatalf("standalone RabbitStream service state = %q", state)
	}
	standaloneStop := exec.Command(
		filepath.Join(repository, "scripts", "stop-services.sh"), standaloneStateFile,
	)
	standaloneStop.Dir = repository
	standaloneStop.Env = stop.Env
	if output, err := standaloneStop.CombinedOutput(); err != nil {
		t.Fatalf("stop standalone RabbitStream fixture: %v\n%s", err, output)
	}
}

func TestGateInputDigestDoesNotInspectLiveDockerForServiceModule(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	repository := t.TempDir()
	bin := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "docker.log")
	for _, directory := range []string{".golib", "pkg/example", "scripts"} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/example",
    "owned_dependencies": [],
    "required_services": ["postgresql"]
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, "packages.json"), "{\"packages\":[]}\n")
	writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "POSTGRES_IMAGE=postgres:18.4-alpine\n")
	writeTestFile(t, filepath.Join(repository, "pkg/example/example.go"), "package example\n")
	writeTestFile(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf 'called\\n' >>\"$FAKE_DOCKER_LOG\"\nprintf '29.6.2\\n'\n")
	writeTestFile(t, filepath.Join(bin, "go"), `#!/bin/sh
case "$2" in
    GOVERSION) printf '%s\n' go1.26.6 ;;
    GOOS) printf '%s\n' linux ;;
    GOARCH) printf '%s\n' amd64 ;;
    CGO_ENABLED) printf '%s\n' 0 ;;
    *) exit 1 ;;
esac
`)
	writeTestFile(t, filepath.Join(bin, "node"), "#!/bin/sh\nprintf '%s\n' v24.0.0\n")
	for _, executable := range []string{"docker", "go", "node"} {
		if err := os.Chmod(filepath.Join(bin, executable), 0o700); err != nil {
			t.Fatalf("make fake %s executable: %v", executable, err)
		}
	}
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
		"format-check",
		"pkg/example",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(command.Env, "GOLIB_ROOT", repository)
	command.Env = environmentWithValues(command.Env, "FAKE_DOCKER_LOG", logFile)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gate input digest: %v\n%s", err, result)
	}
	if len(strings.TrimSpace(string(result))) != sha256.Size*2 {
		t.Fatalf("gate input digest = %q", result)
	}
	if _, err := os.Stat(logFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("service gate digest inspected live Docker: %v", err)
	}

	legacy := exec.Command(
		filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
		"format-check",
		"pkg/example",
	)
	legacy.Dir = repository
	legacy.Env = environmentWithValues(
		command.Env,
		"GOLIB_GATE_INPUT_POLICY",
		"legacy-api-baseline",
	)
	legacyResult, err := legacy.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy gate input digest: %v\n%s", err, legacyResult)
	}
	if len(strings.TrimSpace(string(legacyResult))) != sha256.Size*2 {
		t.Fatalf("legacy gate input digest = %q", legacyResult)
	}
	called, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read legacy Docker invocation log: %v", err)
	}
	if string(called) != "called\n" {
		t.Fatalf("legacy Docker invocation log = %q, want called", called)
	}
}

func TestGateInputDigestDoesNotInspectDockerForServiceFreeModule(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	repository := t.TempDir()
	bin := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "docker.log")
	if err := os.MkdirAll(filepath.Join(repository, "pkg/example"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/example",
    "owned_dependencies": [],
    "required_services": []
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, "packages.json"), "{\"packages\":[]}\n")
	writeTestFile(t, filepath.Join(repository, "pkg/example/example.go"), "package example\n")
	writeTestFile(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf 'called\\n' >>\"$FAKE_DOCKER_LOG\"\nprintf '29.0.0\\n'\n")
	writeTestFile(t, filepath.Join(bin, "go"), `#!/bin/sh
case "$2" in
    GOVERSION) printf '%s\n' go1.26.6 ;;
    GOOS) printf '%s\n' linux ;;
    GOARCH) printf '%s\n' amd64 ;;
    CGO_ENABLED) printf '%s\n' 0 ;;
    *) exit 1 ;;
esac
`)
	writeTestFile(t, filepath.Join(bin, "node"), "#!/bin/sh\nprintf '%s\n' v24.0.0\n")
	for _, executable := range []string{"docker", "go", "node"} {
		if err := os.Chmod(filepath.Join(bin, executable), 0o700); err != nil {
			t.Fatalf("make fake %s executable: %v", executable, err)
		}
	}
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}

	command := exec.Command(
		filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
		"format-check",
		"pkg/example",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(command.Env, "GOLIB_ROOT", repository)
	command.Env = environmentWithValues(command.Env, "FAKE_DOCKER_LOG", logFile)
	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gate input digest: %v\n%s", err, result)
	}
	if len(strings.TrimSpace(string(result))) != sha256.Size*2 {
		t.Fatalf("gate input digest = %q", result)
	}
	if _, err := os.Stat(logFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("service-free digest inspected Docker: %v", err)
	}
}

func TestVerificationSnapshotDisablesInheritedFileSystemMonitor(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "go.mod"), "module example.test/snapshot\n\ngo 1.26.6\n")
	for _, arguments := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "golib@example.test"},
		{"config", "user.name", "golib"},
		{"config", "commit.gpgSign", "false"},
		{"add", "go.mod"},
		{"commit", "-m", "initial"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("prepare snapshot fixture: %v\n%s", err, output)
		}
	}
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	writeTestFile(t, globalConfig, "[core]\n\tfsmonitor = true\n")
	snapshot := filepath.Join(t.TempDir(), "repository")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(root, "scripts", "create-verification-snapshot.sh"),
		repository,
		snapshot,
	)
	command.Env = environmentWithValues(
		os.Environ(),
		"GIT_CONFIG_GLOBAL",
		globalConfig,
	)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create verification snapshot: %v\n%s", err, result)
	}

	config := exec.Command(
		"git",
		"-C",
		snapshot,
		"config",
		"--local",
		"--get",
		"core.fsmonitor",
	)
	value, err := config.Output()
	if err != nil {
		t.Fatalf("read snapshot fsmonitor policy: %v", err)
	}
	if strings.TrimSpace(string(value)) != "false" {
		t.Fatalf("snapshot core.fsmonitor = %q, want false", value)
	}
}

func TestGateInputDigestExcludesIndependentlyVersionedNestedModules(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [
    {
      "directory": ".",
      "module_path": "example.test/repository",
      "owned_dependencies": []
    },
    {
      "directory": "pkg/parent",
      "module_path": "example.test/parent",
      "owned_dependencies": []
    },
    {
      "directory": "pkg/parent/adapter",
      "module_path": "example.test/parent/adapter",
      "owned_dependencies": []
    },
    {
      "directory": "pkg/owner",
      "module_path": "example.test/owner",
      "owned_dependencies": ["example.test/parent/adapter"]
    }
  ]
}
`)
	writeTestFile(t, filepath.Join(root, "packages.json"), "{\"packages\":[]}\n")
	repositoryFile := filepath.Join(root, "tool.go")
	parentFile := filepath.Join(root, "pkg", "parent", "parent.go")
	adapterFile := filepath.Join(root, "pkg", "parent", "adapter", "adapter.go")
	ownerFile := filepath.Join(root, "pkg", "owner", "owner.go")
	for _, directory := range []string{filepath.Dir(adapterFile), filepath.Dir(ownerFile)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, repositoryFile, "package repository\n")
	writeTestFile(t, parentFile, "package parent\n")
	writeTestFile(t, adapterFile, "package adapter\nconst Version = 1\n")
	writeTestFile(t, ownerFile, "package owner\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}

	digest := func(module string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			"format-check",
			module,
		)
		command.Dir = root
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s: %v\n%s", module, err, result)
		}

		return strings.TrimSpace(string(result))
	}

	repositoryBefore := digest(".")
	parentBefore := digest("pkg/parent")
	adapterBefore := digest("pkg/parent/adapter")
	ownerBefore := digest("pkg/owner")
	writeTestFile(t, adapterFile, "package adapter\nconst Version = 2\n")
	repositoryAfterAdapterChange := digest(".")
	parentAfterAdapterChange := digest("pkg/parent")
	adapterAfter := digest("pkg/parent/adapter")
	ownerAfter := digest("pkg/owner")
	if repositoryAfterAdapterChange == repositoryBefore {
		t.Fatal("repository tooling evidence ignored a nested module change")
	}
	if parentAfterAdapterChange != parentBefore {
		t.Fatal("nested adapter change invalidated parent module evidence")
	}
	if adapterAfter == adapterBefore {
		t.Fatal("nested adapter change did not invalidate its own evidence")
	}
	if ownerAfter == ownerBefore {
		t.Fatal("owned nested adapter change did not invalidate owner evidence")
	}

	writeTestFile(t, parentFile, "package parent\nconst Version = 2\n")
	if digest("pkg/parent") == parentAfterAdapterChange {
		t.Fatal("parent module change did not invalidate parent evidence")
	}
	repositoryAfterParentChange := digest(".")
	if repositoryAfterParentChange == repositoryAfterAdapterChange {
		t.Fatal("repository tooling evidence ignored a nested parent change")
	}

	writeTestFile(t, repositoryFile, "package repository\nconst Version = 2\n")
	if digest(".") == repositoryAfterParentChange {
		t.Fatal("repository module change did not invalidate repository evidence")
	}
}

func TestGateInputDigestExcludesOwnedDependencyTests(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "pkg", "dependency"),
		filepath.Join(root, "pkg", "owner"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [
    {
      "directory": "pkg/dependency",
      "module_path": "example.test/dependency",
      "owned_dependencies": [],
      "required_services": [],
      "packages": []
    },
    {
      "directory": "pkg/owner",
      "module_path": "example.test/owner",
      "owned_dependencies": ["example.test/dependency"],
      "required_services": [],
      "packages": []
    }
  ]
}
`)
	writeTestFile(t, filepath.Join(root, "packages.json"), `{"packages":[]}`)
	dependencySource := filepath.Join(root, "pkg", "dependency", "dependency.go")
	dependencyTest := filepath.Join(root, "pkg", "dependency", "dependency_test.go")
	writeTestFile(t, dependencySource, "package dependency\n\nconst Value = 1\n")
	writeTestFile(t, dependencyTest, "package dependency\n\nconst expected = 1\n")
	writeTestFile(t, filepath.Join(root, "pkg", "owner", "owner.go"), "package owner\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = root
	if result, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, result)
	}

	digest := func(module string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			"format-check",
			module,
		)
		command.Dir = root
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s: %v\n%s", module, err, result)
		}

		return strings.TrimSpace(string(result))
	}

	dependencyBefore := digest("pkg/dependency")
	ownerBefore := digest("pkg/owner")
	writeTestFile(t, dependencyTest, "package dependency\n\nconst expected = 2\n")
	if current := digest("pkg/dependency"); current == dependencyBefore {
		t.Fatal("dependency test did not change its own gate inputs")
	}
	if current := digest("pkg/owner"); current != ownerBefore {
		t.Fatalf("dependency test changed owner gate inputs: %s != %s", current, ownerBefore)
	}
	writeTestFile(t, dependencySource, "package dependency\n\nconst Value = 2\n")
	if current := digest("pkg/owner"); current == ownerBefore {
		t.Fatal("dependency source did not change owner gate inputs")
	}
}

func TestGateInputDigestExcludesNonExecutableRepositoryMetadata(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	moduleDirectory := filepath.Join(root, "pkg", "example")
	if err := os.MkdirAll(moduleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	writeManifest := func(familyLabel string, reverseDependencies, testTags []string) {
		t.Helper()
		manifest := map[string]any{
			"modules": []map[string]any{
				{
					"directory":          ".",
					"module_path":        "example.test/repository",
					"owned_dependencies": []string{},
					"required_services":  []string{},
					"releasable":         false,
					"test_tags":          []string{},
					"gates":              map[string]bool{"tests": true},
					"packages":           []any{},
				},
				{
					"directory":                  "pkg/example",
					"module_path":                "example.test/example",
					"owned_dependencies":         []string{},
					"required_services":          []string{},
					"releasable":                 true,
					"family":                     "foundations",
					"family_label":               familyLabel,
					"family_description":         "Catalog navigation metadata.",
					"family_order":               1,
					"reverse_owned_dependencies": reverseDependencies,
					"goal_status":                "implementation-evidence-inventoried",
					"test_tags":                  testTags,
					"gates":                      map[string]bool{"tests": true},
					"packages":                   []any{},
				},
			},
		}
		contents, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(root, "modules.json"), string(contents))
	}

	writeManifest("Foundations", []string{"example.test/consumer"}, []string{})
	writeTestFile(t, filepath.Join(root, "packages.json"), `{"packages":[]}`)
	makefile := filepath.Join(root, "Makefile")
	writeTestFile(t, makefile, "inventory:\n\t@true\n")
	writeTestFile(t, filepath.Join(moduleDirectory, "example.go"), "package example\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = root
	if result, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, result)
	}

	digest := func(module string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			"test",
			module,
		)
		command.Dir = root
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest test inputs: %v\n%s", err, result)
		}

		return strings.TrimSpace(string(result))
	}

	before := digest("pkg/example")
	writeManifest("Core Foundations", []string{"example.test/consumer"}, []string{})
	if current := digest("pkg/example"); current != before {
		t.Fatalf("catalog presentation metadata changed gate inputs: %s != %s", current, before)
	}
	writeManifest("Core Foundations", []string{"example.test/replacement"}, []string{})
	if current := digest("pkg/example"); current != before {
		t.Fatalf("reverse dependency metadata changed gate inputs: %s != %s", current, before)
	}
	rootBefore := digest(".")
	writeTestFile(t, makefile, "cohesion:\n\t@true\n")
	if current := digest("pkg/example"); current != before {
		t.Fatalf("root Makefile changed nested-module gate inputs: %s != %s", current, before)
	}
	if current := digest("."); current == rootBefore {
		t.Fatal("root Makefile did not change root-module gate inputs")
	}
	writeManifest("Core Foundations", []string{"example.test/replacement"}, []string{"integration"})
	if current := digest("pkg/example"); current == before {
		t.Fatal("test tags did not change gate inputs")
	}
}

func TestLocalProxyBuildsSelectedDependencyClosureDeterministically(t *testing.T) {
	sourceRoot := testRepositoryRoot(t)
	root := cleanRepositorySnapshot(t, sourceRoot)
	script := filepath.Join(sourceRoot, "scripts", "build-local-proxy.sh")
	writeTestFile(
		t,
		filepath.Join(root, "pkg", "authentication", "authotel", "proxy_untracked_test.go"),
		"package authotel\n",
	)
	mutationBootstrap := filepath.Join(
		root,
		"pkg",
		"authentication",
		"authotel",
		".golib",
		"mutation-bootstrap",
	)
	if err := os.MkdirAll(mutationBootstrap, 0o755); err != nil {
		t.Fatalf("create mutation bootstrap fixture: %v", err)
	}
	writeTestFile(
		t,
		filepath.Join(mutationBootstrap, "root.zip"),
		"ci evidence\n",
	)
	first := t.TempDir()
	second := t.TempDir()

	for _, output := range []string{first, second} {
		command := exec.Command(
			script,
			output,
			"v0.0.0",
			"pkg/authentication/authotel",
		)
		command.Dir = root
		if result, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build local proxy: %v\n%s", err, result)
		}
	}

	expected := []string{
		"github.com/faustbrian/golib/pkg/authentication",
		"github.com/faustbrian/golib/pkg/authentication/authotel",
		"github.com/faustbrian/golib/pkg/clock",
	}
	for _, modulePath := range expected {
		relative := filepath.FromSlash(modulePath + "/@v/v0.0.0.zip")
		firstArchive, err := os.ReadFile(filepath.Join(first, relative))
		if err != nil {
			t.Fatalf("read first %s archive: %v", modulePath, err)
		}
		secondArchive, err := os.ReadFile(filepath.Join(second, relative))
		if err != nil {
			t.Fatalf("read second %s archive: %v", modulePath, err)
		}
		if string(firstArchive) != string(secondArchive) {
			t.Fatalf("local proxy archive for %s is not deterministic", modulePath)
		}
		if modulePath == "github.com/faustbrian/golib/pkg/authentication/authotel" {
			reader, err := zip.NewReader(bytes.NewReader(firstArchive), int64(len(firstArchive)))
			if err != nil {
				t.Fatalf("open authotel local proxy archive: %v", err)
			}
			for _, file := range reader.File {
				if strings.Contains(file.Name, "/.golib/") {
					t.Fatalf("local proxy archive includes repository tooling: %s", file.Name)
				}
				if strings.HasSuffix(file.Name, "/proxy_untracked_test.go") {
					t.Fatalf("local proxy archive includes untracked output: %s", file.Name)
				}
			}
		}
	}
	authotelManifest, err := os.ReadFile(filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/authotel/@v/v0.0.0.mod",
		),
	))
	if err != nil {
		t.Fatalf("read authotel local manifest: %v", err)
	}
	if strings.Contains(string(authotelManifest), "v0.0.0-") {
		t.Fatalf("local proxy manifest retained remote pseudo-version:\n%s", authotelManifest)
	}
	if !strings.Contains(
		string(authotelManifest),
		"github.com/faustbrian/golib/pkg/authentication v0.0.0",
	) {
		t.Fatalf("local proxy manifest does not use local v0.0.0:\n%s", authotelManifest)
	}
	unselected := filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/jwt/@v/v0.0.0.mod",
		),
	)
	if _, err := os.Stat(unselected); !os.IsNotExist(err) {
		t.Fatalf("local proxy unexpectedly included unselected module: %v", err)
	}
	authotelArchive, err := zip.OpenReader(filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/authotel/@v/v0.0.0.zip",
		),
	))
	if err != nil {
		t.Fatalf("open authotel local archive: %v", err)
	}
	defer func() {
		if err := authotelArchive.Close(); err != nil {
			t.Errorf("close authotel local archive: %v", err)
		}
	}()
	for _, file := range authotelArchive.File {
		if strings.HasSuffix(file.Name, "/proxy_untracked_test.go") {
			t.Fatal("local proxy included untracked module source")
		}
	}

	parentSelection := t.TempDir()
	command := exec.Command(
		script,
		parentSelection,
		"v0.0.0",
		"pkg/authentication",
	)
	command.Dir = root
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build parent module proxy: %v\n%s", err, result)
	}
	for _, modulePath := range []string{
		"github.com/faustbrian/golib/pkg/authentication/authotel",
		"github.com/faustbrian/golib/pkg/authentication/jwt",
		"github.com/faustbrian/golib/pkg/authentication/oidc",
	} {
		moduleFile := filepath.Join(
			parentSelection,
			filepath.FromSlash(modulePath+"/@v/v0.0.0.mod"),
		)
		if _, err := os.Stat(moduleFile); err != nil {
			t.Fatalf("parent proxy omitted nested module %s: %v", modulePath, err)
		}
	}

	nonReleasableSelection := t.TempDir()
	command = exec.Command(
		script,
		nonReleasableSelection,
		"v0.0.0",
		"pkg/analysis/testdata/coverage",
	)
	command.Dir = root
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build empty proxy for cataloged fixture module: %v\n%s", err, result)
	}
	entries, err := os.ReadDir(nonReleasableSelection)
	if err != nil {
		t.Fatalf("read fixture module proxy: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("fixture module proxy contains %d entries, want none", len(entries))
	}

	manifestPath := filepath.Join(root, "modules.json")
	var rootSelectionCatalog catalog
	if err := json.Unmarshal([]byte(mustReadFile(t, manifestPath)), &rootSelectionCatalog); err != nil {
		t.Fatalf("decode root selection catalog: %v", err)
	}
	rootSelectionCatalog.Modules = slices.DeleteFunc(
		rootSelectionCatalog.Modules,
		func(item module) bool {
			return item.Directory != "pkg/authentication" &&
				item.Directory != "pkg/knapsack/objective/gomoney"
		},
	)
	rootSelectionManifest, err := json.MarshalIndent(rootSelectionCatalog, "", "  ")
	if err != nil {
		t.Fatalf("encode root selection catalog: %v", err)
	}
	writeTestFile(t, manifestPath, string(rootSelectionManifest)+"\n")

	rootSelection := t.TempDir()
	command = exec.Command(script, rootSelection, "v0.0.0", ".")
	command.Dir = root
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build root module proxy: %v\n%s", err, result)
	}
	for _, modulePath := range []string{
		"github.com/faustbrian/golib/pkg/authentication",
		"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney",
	} {
		moduleFile := filepath.Join(
			rootSelection,
			filepath.FromSlash(modulePath+"/@v/v0.0.0.mod"),
		)
		if _, err := os.Stat(moduleFile); err != nil {
			t.Fatalf("root proxy omitted releasable module %s: %v", modulePath, err)
		}
	}

	command = exec.Command(
		script,
		t.TempDir(),
		"v0.0.0",
		"pkg/not-cataloged",
	)
	command.Dir = root
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("empty proxy selection succeeded:\n%s", result)
	}

	parentArchive := filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/@v/v0.0.0.zip",
		),
	)
	archive, err := zip.OpenReader(parentArchive)
	if err != nil {
		t.Fatalf("open parent module archive: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Errorf("close parent module archive: %v", err)
		}
	}()
	for _, file := range archive.File {
		if strings.Contains(file.Name, "/authotel/") {
			t.Fatalf("parent archive contains nested module file %s", file.Name)
		}
	}
}

func TestLocalProxyExcludesNestedModulesFromRootArchive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.test/root\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(root, "root.go"), "package root\n")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("create nested module directory: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "nested", "go.mod"), "module example.test/root/nested\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(root, "nested", "nested.go"), "package nested\n")
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [
    {
      "directory": ".",
      "module_path": "example.test/root",
      "releasable": true,
      "owned_dependencies": []
    },
    {
      "directory": "nested",
      "module_path": "example.test/root/nested",
      "releasable": true,
      "owned_dependencies": []
    }
  ]
}
`)

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	add := exec.Command("git", "add", "go.mod", "root.go", "nested/go.mod", "nested/nested.go", "modules.json")
	add.Dir = root
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, output)
	}
	if err := copyStandaloneFoundationFileAs(
		testRepositoryRoot(t),
		root,
		"scripts/build-local-proxy.sh",
		filepath.Join(".golib", "scripts", "build-local-proxy.sh"),
		standaloneRepository{Name: "go-example"},
		map[string]string{},
	); err != nil {
		t.Fatalf("write standalone local proxy builder: %v", err)
	}

	output := t.TempDir()
	command := exec.Command(
		filepath.Join(root, ".golib", "scripts", "build-local-proxy.sh"),
		output,
		"v1.0.0",
		".",
	)
	command.Dir = root
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build root local proxy: %v\n%s", err, result)
	}

	archive, err := zip.OpenReader(filepath.Join(
		output,
		filepath.FromSlash("example.test/root/@v/v1.0.0.zip"),
	))
	if err != nil {
		t.Fatalf("open root module archive: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Errorf("close root module archive: %v", err)
		}
	}()
	for _, file := range archive.File {
		if strings.Contains(file.Name, "/nested/") {
			t.Fatalf("root archive contains nested module file %s", file.Name)
		}
	}
}

func TestLocalProxyUsesPlannedStableVersionForOwnedDependencies(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	output := t.TempDir()
	command := exec.Command(
		filepath.Join(root, "scripts", "build-local-proxy.sh"),
		output,
		"v1.0.0",
		"pkg/authentication/authotel",
	)
	command.Dir = root
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build stable local proxy: %v\n%s", err, result)
	}

	manifest, err := os.ReadFile(filepath.Join(
		output,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/authotel/@v/v1.0.0.mod",
		),
	))
	if err != nil {
		t.Fatalf("read stable authotel manifest: %v", err)
	}
	if !strings.Contains(
		string(manifest),
		"github.com/faustbrian/golib/pkg/authentication v1.0.0",
	) {
		t.Fatalf("stable proxy manifest does not use planned dependency version:\n%s", manifest)
	}
}

func TestReleasePlanDefaultsUnreleasedModulesToV1(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"--plan",
		"pkg/retry",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("plan initial release: %v\n%s", err, output)
	}

	var plan struct {
		Module                 string   `json:"module"`
		CurrentVersion         string   `json:"current_version"`
		ProposedVersion        string   `json:"proposed_version"`
		Tag                    string   `json:"tag"`
		DependencyReleaseOrder []string `json:"dependency_release_order"`
	}
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatalf("decode release plan: %v\n%s", err, output)
	}
	if plan.Module != "pkg/retry" || plan.CurrentVersion != "unreleased" ||
		plan.ProposedVersion != "v1.0.0" || plan.Tag != "pkg/retry/v1.0.0" {
		t.Fatalf("unexpected initial release plan: %+v", plan)
	}
	wantOrder := []string{"pkg/resilience", "pkg/retry"}
	if !slices.Equal(plan.DependencyReleaseOrder, wantOrder) {
		t.Fatalf("dependency release order = %v, want %v", plan.DependencyReleaseOrder, wantOrder)
	}
}

func TestReleasePlanReportsOperationalAssuranceVerdict(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"--plan",
		"pkg/retry",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("plan release with assurance: %v\n%s", err, output)
	}

	var plan struct {
		OperationalAssurance struct {
			Verdict string `json:"verdict"`
		} `json:"operational_assurance"`
	}
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatalf("decode release plan: %v\n%s", err, output)
	}
	if plan.OperationalAssurance.Verdict != "not ready" {
		t.Fatalf(
			"operational assurance verdict = %q, want not ready",
			plan.OperationalAssurance.Verdict,
		)
	}
}

func TestReleaseCreationRequiresOperationalAssurance(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"pkg/retry",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("release creation accepted a not-ready assurance verdict:\n%s", output)
	}
	if !strings.Contains(string(output), "operational assurance verdict is not ready") {
		t.Fatalf("release creation returned the wrong assurance failure:\n%s", output)
	}
}

func TestReleasePlanDefaultsVerkleTreeInitialReleaseToV1(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"--plan",
		"pkg/verkle-tree",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("plan verkle-tree initial release: %v\n%s", err, output)
	}

	var plan struct {
		Module          string `json:"module"`
		CurrentVersion  string `json:"current_version"`
		ProposedVersion string `json:"proposed_version"`
		Tag             string `json:"tag"`
	}
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatalf("decode verkle-tree release plan: %v\n%s", err, output)
	}
	if plan.Module != "pkg/verkle-tree" || plan.CurrentVersion != "unreleased" ||
		plan.ProposedVersion != "v1.0.0" ||
		plan.Tag != "pkg/verkle-tree/v1.0.0" {
		t.Fatalf("unexpected verkle-tree initial release plan: %+v", plan)
	}
}

func TestReleasePlanRejectsFormerVerkleTreePreV1Version(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"--plan",
		"--version",
		"v0.1.0",
		"pkg/verkle-tree",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("release plan accepted the former pre-v1 initial version:\n%s", output)
	}
	if !strings.Contains(string(output), "initial release must be v1.0.0") {
		t.Fatalf("release plan returned the wrong initial-version failure:\n%s", output)
	}
}

func TestReleasePlanRejectsPreV1InitialVersion(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts", "release.sh"),
		"--plan",
		"--version",
		"v0.9.0",
		"pkg/retry",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("release plan accepted pre-v1 initial version:\n%s", output)
	}
	if !strings.Contains(string(output), "initial release must be v1.0.0") {
		t.Fatalf("release plan returned the wrong initial-version failure:\n%s", output)
	}
}

func TestIsolatedGoUsesTemporarySumsForOwnedModules(t *testing.T) {
	root := testRepositoryRoot(t)
	module := t.TempDir()
	writeTestFile(t, filepath.Join(module, "go.mod"), `module example.test/consumer

go 1.26.6

require github.com/faustbrian/golib/pkg/dependency v0.0.0
`)
	sourceSum := filepath.Join(module, "go.sum")
	const staleSum = "github.com/faustbrian/golib/pkg/dependency v0.0.0 h1:stale=\n"
	writeTestFile(t, sourceSum, staleSum)

	fakeGo := filepath.Join(t.TempDir(), "go")
	output := filepath.Join(t.TempDir(), "go-flags")
	writeTestFile(t, fakeGo, `#!/bin/sh
set -eu
modfile=
for flag in ${GOFLAGS:-}; do
	case "$flag" in
		-modfile=*) modfile=${flag#-modfile=} ;;
	esac
done
for argument in "$@"; do
	case "$argument" in
		-modfile=*) modfile=${argument#-modfile=} ;;
	esac
done
if { [ "${1:-}" = run ] && [ "${2#*@}" != "${2:-}" ]; } ||
	[ "${1:-}" = doc ]; then
	{
		printf 'environment=%s\n' "$GOFLAGS"
		printf 'arguments=%s\n' "$*"
	} >"$GOLIB_FAKE_GO_OUTPUT"
	exit 0
fi
test -n "$modfile"
test -f "$modfile"
sum=${modfile%.mod}.sum
if grep -q '^github.com/faustbrian/golib/' "$sum"; then
	echo "temporary sum retained an owned checksum" >&2
	exit 1
fi
if [ "${1:-}" = mod ] && [ "${2:-}" = download ]; then
	exit 0
fi
if [ "${1:-}" = mod ] && [ "${2:-}" = tidy ]; then
	printf '%s\n' \
		'github.com/faustbrian/golib/pkg/dependency v0.0.0 h1:current=' \
		>>"$sum"
	exit 0
fi
{
	printf 'environment=%s\n' "$GOFLAGS"
	printf 'arguments=%s\n' "$*"
	printf 'isolated-modfile=%s\n' "${GOLIB_ISOLATED_MODFILE:-}"
} >"$GOLIB_FAKE_GO_OUTPUT"
`)
	if err := os.Chmod(fakeGo, 0o700); err != nil {
		t.Fatal(err)
	}

	environment := environmentWithValues(
		environmentWithValues(
			environmentWithValues(
				os.Environ(),
				"GOLIB_REAL_GO",
				fakeGo,
			),
			"GOLIB_ISOLATED_MODFILES_DIRECTORY",
			t.TempDir(),
		),
		"GOLIB_FAKE_GO_OUTPUT",
		output,
	)
	script := filepath.Join(root, "scripts", "internal", "isolated-go.sh")
	command := exec.Command(script, "test", "./...")
	command.Dir = module
	command.Env = environment
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run isolated Go command: %v\n%s", err, result)
	}

	invocation, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"-modfile=", "-mod=readonly"} {
		if !strings.Contains(string(invocation), required) {
			t.Fatalf("isolated Go invocation lacks %q: %s", required, invocation)
		}
	}
	environmentLine := strings.SplitN(string(invocation), "\n", 2)[0]
	if strings.Contains(environmentLine, "-modfile=") ||
		strings.Contains(environmentLine, "-mod=readonly") {
		t.Fatalf("isolated Go flags leaked into child environment: %s", invocation)
	}
	if !strings.Contains(string(invocation), "isolated-modfile=") ||
		strings.Contains(string(invocation), "isolated-modfile=\n") {
		t.Fatalf("isolated Go invocation omitted its opt-in modfile: %s", invocation)
	}
	versionedTool := exec.Command(script, "run", "example.test/tool@v1.0.0")
	versionedTool.Dir = module
	versionedTool.Env = environment
	if result, runErr := versionedTool.CombinedOutput(); runErr != nil {
		t.Fatalf("run versioned tool: %v\n%s", runErr, result)
	}
	invocation, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(invocation), "-modfile=") ||
		strings.Contains(string(invocation), "-mod=readonly") {
		t.Fatalf("versioned tool inherited module isolation: %s", invocation)
	}
	get := exec.Command(script, "get", "example.test/dependency@v1.1.0")
	get.Dir = module
	get.Env = environment
	if result, getErr := get.CombinedOutput(); getErr != nil {
		t.Fatalf("run isolated dependency update: %v\n%s", getErr, result)
	}
	invocation, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"-modfile=", "-mod=mod"} {
		if !strings.Contains(string(invocation), required) {
			t.Fatalf("isolated dependency update lacks %q: %s", required, invocation)
		}
	}
	for _, line := range strings.Split(string(invocation), "\n") {
		if strings.HasPrefix(line, "arguments=") &&
			strings.Contains(line, "-modfile=") {
			t.Fatalf("isolated dependency update passed modfile as an argument: %s", invocation)
		}
	}
	fakeTool := filepath.Join(t.TempDir(), "fake-tool")
	writeTestFile(t, fakeTool, `#!/bin/sh
set -eu
{
	printf 'environment=%s\n' "$GOFLAGS"
	printf 'arguments=%s\n' "$*"
	printf 'go=%s\n' "$(command -v go)"
} >"$GOLIB_FAKE_GO_OUTPUT"
`)
	if err := os.Chmod(fakeTool, 0o700); err != nil {
		t.Fatal(err)
	}
	isolatedTool := exec.Command(script, "exec-tool", fakeTool, "./...")
	isolatedTool.Dir = module
	isolatedTool.Env = environment
	if result, toolErr := isolatedTool.CombinedOutput(); toolErr != nil {
		t.Fatalf("run tool against isolated module: %v\n%s", toolErr, result)
	}
	invocation, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"-modfile=", "-mod=readonly"} {
		if !strings.Contains(string(invocation), required) {
			t.Fatalf("isolated tool invocation lacks %q: %s", required, invocation)
		}
	}
	if !strings.Contains(string(invocation), "arguments=./...") {
		t.Fatalf("isolated tool arguments were not preserved: %s", invocation)
	}
	if !strings.Contains(string(invocation), "go="+fakeGo) {
		t.Fatalf("isolated tool did not resolve the real Go binary: %s", invocation)
	}
	documentation := exec.Command(script, "doc", "./...")
	documentation.Dir = module
	documentation.Env = environment
	if result, docErr := documentation.CombinedOutput(); docErr != nil {
		t.Fatalf("run documentation command: %v\n%s", docErr, result)
	}
	invocation, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	documentationLines := strings.SplitN(string(invocation), "\n", 2)
	for _, required := range []string{"-modfile=", "-mod=readonly"} {
		if !strings.Contains(documentationLines[0], required) {
			t.Fatalf(
				"documentation environment lacks %q: %s",
				required,
				invocation,
			)
		}
		if strings.Contains(documentationLines[1], required) {
			t.Fatalf(
				"documentation arguments contain %q: %s",
				required,
				invocation,
			)
		}
	}
	tidy := exec.Command(script, "mod", "tidy", "-diff")
	tidy.Dir = module
	tidy.Env = environment
	if result, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("check isolated module tidiness: %v\n%s", err, result)
	}
	currentSum, err := os.ReadFile(sourceSum)
	if err != nil {
		t.Fatal(err)
	}
	if string(currentSum) != staleSum {
		t.Fatalf("isolated Go command changed source go.sum:\n%s", currentSum)
	}
}

func TestIsolatedGatesUseLocalProxyWithoutWeakeningPublicProof(t *testing.T) {
	root := testRepositoryRoot(t)
	moduleScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	moduleContract := string(moduleScript)
	runnerScript, err := os.ReadFile(filepath.Join(root, "scripts", "run-modules.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runnerContract := string(runnerScript)
	for _, required := range []string{
		`build-local-proxy.sh`,
		`isolated-go.sh`,
		`GOLIB_ISOLATED_MODFILES_DIRECTORY`,
		`run_go_tool`,
		`exec-tool`,
		`GOPROXY="file://${GOLIB_LOCAL_PROXY},${upstream}"`,
		`GOMODCACHE="${GOLIB_LOCAL_MODCACHE}"`,
		`golib-modcache.`,
		`GOLANGCI_LINT_CACHE="${GOLIB_ISOLATED_MODFILES_DIRECTORY}/golangci-lint-cache"`,
		`mkdir -p "${GOLANGCI_LINT_CACHE}"`,
		`run --allow-parallel-runners --timeout=10m ./...`,
		`chmod -R u+w "${GOLIB_LOCAL_MODCACHE}"`,
		`awk '$1 !~ /^github\.com\/faustbrian\/golib\// { print }'`,
		`test -s "${root}/LICENSE"`,
		`--ignore "github.com/faustbrian/golib"`,
		`--config "${root}/.gitleaks.toml"`,
		`check-api-baseline.sh`,
		`update-api-baseline.sh`,
		`export GOFLAGS="${upstream_flags}"`,
		`GOWORK=off "$@"`,
	} {
		if !strings.Contains(moduleContract, required) {
			t.Fatalf("isolated module contract lacks %q", required)
		}
	}
	if strings.Contains(
		moduleContract,
		`export GOFLAGS="${upstream_flags:+${upstream_flags} }-mod=readonly"`,
	) {
		t.Fatal("module isolation leaked read-only module flags into child tools")
	}
	if strings.Contains(moduleContract, `/cache/download`) {
		t.Fatal("module isolation uses a potentially incomplete module cache as a proxy")
	}
	if strings.Contains(runnerContract, `/cache/download`) {
		t.Fatal("module orchestrator uses a potentially incomplete module cache as a proxy")
	}
	if !strings.Contains(
		moduleContract,
		"run_go_tool \\\n"+
			"                \"github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@${CYCLONEDX_VERSION}\" \\\n"+
			"                cyclonedx-gomod \\",
	) {
		t.Fatal("CycloneDX SBOM generation does not use the isolated tool runner")
	}

	releaseScript, err := os.ReadFile(filepath.Join(root, "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	releaseContract := string(releaseScript)
	for _, required := range []string{
		`env -u GOLIB_LOCAL_PROXY`,
		`GOPROXY="${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}"`,
		`public release verification passed`,
	} {
		if !strings.Contains(releaseContract, required) {
			t.Fatalf("public release contract lacks %q", required)
		}
	}
}

func TestAPIBaselineRejectsIncompatibleChanges(t *testing.T) {
	root := testRepositoryRoot(t)
	module := t.TempDir()
	writeTestFile(t, filepath.Join(module, "go.mod"), `module example.com/api

go 1.26
`)
	source := filepath.Join(module, "api.go")
	writeTestFile(t, source, `package api

func Stable(value int) string { return "" }
`)

	update := exec.Command(
		filepath.Join(root, "scripts", "update-api-baseline.sh"),
		module,
	)
	update.Dir = root
	if output, err := update.CombinedOutput(); err != nil {
		t.Fatalf("update API baseline: %v\n%s", err, output)
	}

	check := exec.Command(
		filepath.Join(root, "scripts", "check-api-baseline.sh"),
		module,
	)
	check.Dir = root
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("check unchanged API: %v\n%s", err, output)
	}

	writeTestFile(t, source, `package api

func Stable(value string) string { return "" }
`)
	check = exec.Command(
		filepath.Join(root, "scripts", "check-api-baseline.sh"),
		module,
	)
	check.Dir = root
	output, err := check.CombinedOutput()
	if err == nil {
		t.Fatal("API check accepted an incompatible signature change")
	}
	if !strings.Contains(string(output), "incompatible exported API changes") {
		t.Fatalf("API check returned the wrong failure:\n%s", output)
	}
}

func TestOpenRPCIntegrationTargetReferencesExecutableScript(t *testing.T) {
	root := testRepositoryRoot(t)
	module := filepath.Join(root, "pkg", "openrpc")
	makefile, err := os.ReadFile(filepath.Join(module, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}

	const command = "./scripts/check-go-jsonrpc-integration.sh"
	if !strings.Contains(string(makefile), "\nintegration:\n\t"+command+"\n") {
		t.Fatalf("OpenRPC integration target does not invoke %s", command)
	}

	info, err := os.Stat(filepath.Join(module, filepath.FromSlash(command)))
	if err != nil {
		t.Fatalf("stat OpenRPC integration script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("OpenRPC integration script is not executable")
	}
}

func TestQualityScriptsDoNotRequireRipgrep(t *testing.T) {
	root := testRepositoryRoot(t)
	module := standaloneFuzzModule(t)
	path := restrictedToolPath(t)

	for _, test := range []struct {
		name   string
		script string
	}{
		{name: "safety", script: "check-go-safety.sh"},
		{name: "fuzz", script: "check-fuzz.sh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(filepath.Join(root, "scripts", test.script), module)
			command.Dir = root
			command.Env = environmentWithPath(path)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%s without ripgrep: %v\n%s", test.script, err, output)
			}
			if strings.Contains(string(output), "command not found") {
				t.Fatalf("%s silently missed a tool: %s", test.script, output)
			}
		})
	}
}

func TestFuzzSmokeUsesDeterministicExecutionBudget(t *testing.T) {
	root := testRepositoryRoot(t)
	contents, err := os.ReadFile(filepath.Join(root, "scripts", "check-fuzz.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(contents)
	for _, required := range []string{
		`GOLIB_FUZZ_SMOKE_BUDGET:-10000x`,
		`-fuzztime="${fuzz_budget}"`,
		`no fuzz targets were executed`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("fuzz smoke contract lacks %q", required)
		}
	}
	moduleScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moduleScript), "find_make_target fuzz fuzz-smoke") {
		t.Fatal("module fuzz gate does not honor fuzz-smoke targets")
	}
}

func TestFuzzSmokeRejectsModulesWithoutTargets(t *testing.T) {
	root := testRepositoryRoot(t)
	module := standaloneModule(t, "package fixture\n")
	command := exec.Command(filepath.Join(root, "scripts", "check-fuzz.sh"), module)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("fuzz smoke accepted a module without fuzz targets")
	}
	if !strings.Contains(string(output), "no fuzz targets were executed") {
		t.Fatalf("fuzz smoke returned the wrong failure:\n%s", output)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSafetyScriptRejectsForbiddenProductionFeatures(t *testing.T) {
	root := testRepositoryRoot(t)
	tests := map[string]string{
		"unsafe import":      "package violation\n\nimport \"unsafe\"\n\nvar _ unsafe.Pointer\n",
		"cgo import":         "package violation\n\nimport \"C\"\n",
		"linkname directive": "package violation\n\n//go:linkname local target\nfunc local()\n",
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			module := standaloneModule(t, source)
			command := exec.Command(filepath.Join(root, "scripts", "check-go-safety.sh"), module)
			command.Dir = root
			command.Env = environmentWithPath(restrictedToolPath(t))
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("safety check accepted %s", name)
			}
			if !strings.Contains(string(output), "GO-SAFETY-1 violation") {
				t.Fatalf("safety check returned the wrong failure: %s", output)
			}
		})
	}
}

func TestReleaseRejectsUnknownAndNonReleasableModules(t *testing.T) {
	root := testRepositoryRoot(t)
	tests := map[string]string{
		"unknown":        "pkg/not-a-module",
		"non-releasable": "pkg/json-schema/benchmarks/comparison",
	}
	for name, module := range tests {
		t.Run(name, func(t *testing.T) {
			command := exec.Command("bash", filepath.Join(root, "scripts", "release.sh"), "--dry-run", module)
			command.Dir = root
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("release accepted %s module", name)
			}
			if name == "unknown" && !strings.Contains(string(output), "unknown module") {
				t.Fatalf("release returned the wrong unknown-module failure: %s", output)
			}
			if name == "non-releasable" && !strings.Contains(string(output), "module is not releasable") {
				t.Fatalf("release returned the wrong lifecycle failure: %s", output)
			}
		})
	}
}

func TestReleaseSelectionContainsOnlyReleasableModules(t *testing.T) {
	root := testRepositoryRoot(t)
	command := exec.Command(
		filepath.Join(root, "scripts", "filter-releasable-modules.sh"),
	)
	command.Dir = root
	command.Stdin = strings.NewReader(strings.Join([]string{
		"pkg/authentication",
		"pkg/json-schema/benchmarks/comparison",
		".",
		"pkg/wsdl",
	}, "\n") + "\n")

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("filter release selection: %v\n%s", err, output)
	}
	if got, want := string(output), "pkg/authentication\npkg/wsdl\n"; got != want {
		t.Fatalf("release selection = %q, want %q", got, want)
	}
}

func TestCIUsesCompleteModuleProxiesAndCollisionFreeOutputs(t *testing.T) {
	root := testRepositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(workflow)
	for _, required := range []string{
		`actions: read`,
		`path: ${{ format('{0}/golib-evidence-{1}', runner.temp, matrix.artifact) }}`,
		`include-hidden-files: true`,
		`workspace="${GITHUB_WORKSPACE}/go.work"`,
		`output="${RUNNER_TEMP}/codeql-build/${slug}"`,
		`.build_tags[]?`,
		`select(.build_required == true)`,
		`go list -f '{{if or .GoFiles .CgoFiles}}{{.ImportPath}}{{end}}'`,
		`sed '/^$/d'`,
		`LC_ALL=C sort -u`,
		`if [[ "${resolved}" != "${expected}" ]]`,
		`while IFS= read -r package; do`,
		`while IFS= read -r tag; do`,
		`target="${output}/package-${package_index}-variant-${variant_index}"`,
		`GOWORK="${workspace}" go build -tags="${tag}" -o "${target}" "${package}"`,
		`target="${output}/package-${package_index}-default"`,
		`GOWORK="${workspace}" go build -o "${target}" "${package}"`,
		`matrix.directory == 'pkg/cli'`,
		`ZSH_DEB_SHA256: bd5cc8dd3a01a6db38c0a815d75202c356a9c7f378674ba7bed9bc86dcba8af0`,
		`zsh_5.9-6ubuntu2_amd64.deb`,
		`printf '%s  %s\n' "${ZSH_DEB_SHA256}" "${archive}" | sha256sum --check -`,
		`dpkg-deb --extract "${archive}" "${root}"`,
		`echo "${root}/bin" >> "${GITHUB_PATH}"`,
		`"${root}/bin/zsh" --version | grep -Eq '^zsh 5\.9 '`,
		`package-manager-cache: false`,
		`denoland/setup-deno@22d081ff2d3a40755e97629de92e3bcbfa7cf2ed`,
		`deno-version: '2.9.4'`,
		`restore-ci-mutation-evidence.sh '${{ matrix.directory }}'`,
		`name: ${{ inputs.release_dry_run == true && 'release-evidence' || 'evidence' }}-${{ matrix.artifact }}`,
		`GITHUB_REPOSITORY_ID: ${{ github.repository_id }}`,
		`release_dry_run:`,
		`RELEASE_DRY_RUN: ${{ inputs.release_dry_run }}`,
		`.releasable == true`,
		`Run release dry-run`,
		`id: strict_contract`,
		`id: release_dry_run`,
		`CONTRACT_OUTCOME: ${{ inputs.release_dry_run == true && steps.release_dry_run.outcome || steps.strict_contract.outcome }}`,
		`"${CONTRACT_OUTCOME}"`,
		`GOLIB_VERIFICATION_SNAPSHOT: '1'`,
		`./scripts/run-modules.sh release-dry-run`,
		`--modules '${{ matrix.directory }}'`,
		`release-dry-run.log`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("CI workflow lacks %q", required)
		}
	}
	if strings.Contains(contract, "apt-get") {
		t.Fatal("CI workflow installs runner packages instead of verifying available pinned runtimes")
	}
	if strings.Contains(contract, "actions/setup-node@") &&
		strings.Contains(contract, "node-version: '24.4.1'\n          cache: false") {
		t.Fatal("setup-node receives unsupported cache=false package-manager input")
	}
	restore := strings.Index(contract, "Restore content-addressed mutation evidence")
	strictContract := strings.Index(contract, "Run strict module contract")
	if restore < 0 || strictContract < 0 || restore > strictContract {
		t.Fatal("CI does not restore mutation checkpoints before module verification")
	}
}

func TestCIStagesAttributableOutcomeWithoutGateArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	destination := filepath.Join(t.TempDir(), "evidence")
	command := exec.Command(
		filepath.Join(testRepositoryRoot(t), "scripts", "stage-ci-evidence.sh"),
		"testdata/coverage",
		destination,
		"success",
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
	for key, value := range map[string]string{
		"GITHUB_REPOSITORY":  "faustbrian/go-analysis",
		"GITHUB_RUN_ID":      "1234",
		"GITHUB_RUN_ATTEMPT": "2",
		"GITHUB_SHA":         "0123456789abcdef",
	} {
		command.Env = environmentWithValues(command.Env, key, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stage CI outcome without gate artifacts: %v\n%s", err, output)
	}

	result := struct {
		Module     string `json:"module"`
		Outcome    string `json:"outcome"`
		Repository string `json:"repository"`
		RunID      string `json:"run_id"`
		RunAttempt string `json:"run_attempt"`
		Revision   string `json:"revision"`
	}{}
	contents, err := os.ReadFile(filepath.Join(destination, "ci-result.json"))
	if err != nil {
		t.Fatalf("read attributable CI result: %v", err)
	}
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode attributable CI result: %v", err)
	}
	if result.Module != "testdata/coverage" || result.Outcome != "success" ||
		result.Repository != "faustbrian/go-analysis" || result.RunID != "1234" ||
		result.RunAttempt != "2" || result.Revision != "0123456789abcdef" {
		t.Fatalf("attributable CI result = %+v", result)
	}
}

func TestStandaloneTidyRepeatsUntilManifestChecksumsConverge(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		testRepositoryRoot(t),
		"scripts",
		"tidy-standalone-modules.sh",
	))
	if err != nil {
		t.Fatalf("read standalone tidy orchestrator: %v", err)
	}
	contract := string(contents)
	for _, required := range []string{
		`standalone_manifest_digest()`,
		`maximum_passes=`,
		`for ((pass = 1; pass <= maximum_passes; pass++))`,
		`find "${GOMODCACHE}" -mindepth 1 -depth -delete`,
		`before="$(standalone_manifest_digest)"`,
		`after="$(standalone_manifest_digest)"`,
		`if [[ "${before}" == "${after}" ]]`,
		`standalone module checksums did not converge`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("standalone tidy orchestrator lacks %q", required)
		}
	}
	loop := strings.Index(contract, `for ((pass = 1; pass <= maximum_passes; pass++))`)
	clean := strings.Index(contract, `standalone-clean-sums`)
	if loop < 0 || clean < loop {
		t.Fatal("standalone checksum cleanup is not repeated inside the convergence loop")
	}
}

func TestCIRestoresOnlyCatalogedMutationCheckpoints(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, "pkg/example"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "packages": [
      {"directory": ".", "coverage_required": true},
      {"directory": "nested/package", "coverage_required": true},
      {"directory": "docs", "coverage_required": false}
    ]
  }]
}
`)
	archive := filepath.Join(t.TempDir(), "evidence.zip")
	writeMutationEvidenceArchive(t, archive, map[string]string{
		"mutation-checkpoints/root.json":           legacyCheckpointFixture("pkg/example", "."),
		"mutation-checkpoints/nested-package.json": checkpointFixture("pkg/example", "nested/package"),
		"mutation-checkpoints/docs.json":           checkpointFixture("pkg/example", "docs"),
		"evidence/coverage.json":                   "{}\n",
	})

	command := exec.Command(
		filepath.Join(root, "scripts", "restore-ci-mutation-evidence.sh"),
		"pkg/example",
		archive,
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restore mutation evidence: %v\n%s", err, output)
	}
	for _, name := range []string{"root.json", "nested-package.json"} {
		path := filepath.Join(repository, ".artifacts/pkg/example/mutation-checkpoints", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("restored checkpoint %s: %v", name, err)
		}
	}
	for _, path := range []string{
		filepath.Join(repository, ".artifacts/pkg/example/mutation-checkpoints/docs.json"),
		filepath.Join(repository, ".artifacts/pkg/example/evidence/coverage.json"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("non-cataloged artifact restored at %s: %v", path, err)
		}
	}
}

func TestCIMutationRestoreRejectsCheckpointIdentityMismatch(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, "pkg/example"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "packages": [{"directory": ".", "coverage_required": true}]
  }]
}
`)
	archive := filepath.Join(t.TempDir(), "evidence.zip")
	writeMutationEvidenceArchive(t, archive, map[string]string{
		"mutation-checkpoints/root.json": checkpointFixture("pkg/other", "."),
	})

	command := exec.Command(
		filepath.Join(root, "scripts", "restore-ci-mutation-evidence.sh"),
		"pkg/example",
		archive,
	)
	command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("reject mismatched mutation evidence: %v\n%s", err, output)
	}
	checkpoint := filepath.Join(
		repository,
		".artifacts/pkg/example/mutation-checkpoints/root.json",
	)
	if _, err := os.Stat(checkpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched checkpoint restored: %v", err)
	}
}

func TestCIMutationRestoreCombinesNewestValidCheckpointsAcrossPriorArtifacts(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{".golib", "pkg/example"} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "packages": [
      {"directory": ".", "coverage_required": true},
      {"directory": "nested/package", "coverage_required": true}
    ]
  }]
}
`)
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "gate-input-digest.sh"),
		"#!/bin/sh\nset -eu\nprintf '%s\\n' '"+strings.Repeat("a", 64)+"'\n",
	)
	writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "GREMLINS_VERSION=v0.6.0\n")
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "mutation-verifier-identity.sh"),
		"#!/bin/sh\nset -eu\nprintf '%s\\n' '"+strings.Repeat("b", 64)+"'\n",
	)
	for _, executable := range []string{
		"scripts/gate-input-digest.sh",
		"scripts/mutation-verifier-identity.sh",
	} {
		if err := os.Chmod(filepath.Join(repository, executable), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	newest := filepath.Join(t.TempDir(), "release-evidence.zip")
	writeMutationEvidenceArchive(t, newest, map[string]string{
		"release-dry-run.log": "release rehearsal\n",
	})
	partial := filepath.Join(t.TempDir(), "partial-evidence.zip")
	writeMutationEvidenceArchive(t, partial, map[string]string{
		"mutation-checkpoints/nested-package.json": strings.Replace(
			checkpointFixture("pkg/example", "nested/package"),
			strings.Repeat("a", 64),
			strings.Repeat("d", 64),
			1,
		),
	})
	older := filepath.Join(t.TempDir(), "verification-evidence.zip")
	writeMutationEvidenceArchive(t, older, map[string]string{
		"mutation-checkpoints/root.json":           checkpointFixture("pkg/example", "."),
		"mutation-checkpoints/nested-package.json": checkpointFixture("pkg/example", "nested/package"),
	})

	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	writeTestFile(t, gh, `#!/bin/sh
set -eu
case "$*" in
  *"actions/artifacts -f name=evidence-pkg-example"*)
    cat <<'JSON'
{"artifacts":[
  {"id":33,"expired":false,"created_at":"2026-08-24T08:00:00Z","workflow_run":{"id":333,"head_repository_id":123,"head_branch":"main"}},
  {"id":22,"expired":false,"created_at":"2026-08-24T07:00:00Z","workflow_run":{"id":222,"head_repository_id":123,"head_branch":"main"}},
  {"id":11,"expired":false,"created_at":"2026-08-24T06:00:00Z","workflow_run":{"id":111,"head_repository_id":123,"head_branch":"main"}}
]}
JSON
    ;;
  *"actions/artifacts/33/zip"*) cat "$FAKE_NEWEST_ARCHIVE" ;;
  *"actions/artifacts/22/zip"*) cat "$FAKE_PARTIAL_ARCHIVE" ;;
  *"actions/artifacts/11/zip"*) cat "$FAKE_OLDER_ARCHIVE" ;;
  *) printf 'unexpected gh arguments: %s\n' "$*" >&2; exit 2 ;;
esac
`)
	if err := os.Chmod(gh, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(
		filepath.Join(root, "scripts", "restore-ci-mutation-evidence.sh"),
		"pkg/example",
	)
	command.Env = os.Environ()
	for name, value := range map[string]string{
		"GOLIB_ROOT":           repository,
		"GH_TOKEN":             "test-token",
		"GITHUB_REPOSITORY":    "faustbrian/golib",
		"GITHUB_REPOSITORY_ID": "123",
		"GITHUB_RUN_ID":        "999",
		"GITHUB_SHA":           strings.Repeat("c", 40),
		"FAKE_NEWEST_ARCHIVE":  newest,
		"FAKE_PARTIAL_ARCHIVE": partial,
		"FAKE_OLDER_ARCHIVE":   older,
		"PATH":                 bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	} {
		command.Env = environmentWithValues(command.Env, name, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restore prior mutation evidence: %v\n%s", err, output)
	}
	for _, name := range []string{"root.json", "nested-package.json"} {
		checkpoint := filepath.Join(
			repository,
			".artifacts/pkg/example/mutation-checkpoints",
			name,
		)
		if _, err := os.Stat(checkpoint); err != nil {
			t.Fatalf("restore valid checkpoint %s: %v\n%s", name, err, output)
		}
	}
	if !strings.Contains(string(output), "restored 2 prior content-addressed mutation checkpoints") {
		t.Fatalf("restore output = %q", output)
	}
}

func TestCIMutationRestoreUsesOlderApprovedIdentityMigration(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		".golib",
		"pkg/example",
		"scripts/internal",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "packages": [{"directory": ".", "coverage_required": true}]
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "GREMLINS_VERSION=v0.6.0\n")
	writeTestFile(t, filepath.Join(repository, ".golib/mutation-history-migrations.json"), "{}\n")
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "gate-input-digest.sh"),
		"#!/bin/sh\nset -eu\nprintf '%s\\n' '"+strings.Repeat("a", 64)+"'\n",
	)
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "mutation-verifier-identity.sh"),
		"#!/bin/sh\nset -eu\nprintf '%s\\n' '"+strings.Repeat("b", 64)+"'\n",
	)
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "internal", "reuse-approved-mutation-checkpoint.sh"),
		`#!/bin/sh
set -eu
checkpoint="$2"
current_input="$5"
validated_revision="$8"
output="$9"
approved_input="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
[ "$(jq -r '.gate_input_digest' "$checkpoint")" = "$approved_input" ] || exit 1
jq --arg input "$current_input" --arg revision "$validated_revision" '
  .gate_input_digest = $input
  | .validated_revision = $revision
  | .verifier_identity_source = "approved-semantic-migration"
' "$checkpoint" >"$output"
`,
	)
	for _, executable := range []string{
		"scripts/gate-input-digest.sh",
		"scripts/mutation-verifier-identity.sh",
		"scripts/internal/reuse-approved-mutation-checkpoint.sh",
	} {
		if err := os.Chmod(filepath.Join(repository, executable), 0o700); err != nil {
			t.Fatalf("make %s executable: %v", executable, err)
		}
	}

	newest := filepath.Join(t.TempDir(), "unapproved-evidence.zip")
	writeMutationEvidenceArchive(t, newest, map[string]string{
		"mutation-checkpoints/root.json": strings.Replace(
			checkpointFixture("pkg/example", "."),
			strings.Repeat("a", 64),
			strings.Repeat("e", 64),
			1,
		),
	})
	older := filepath.Join(t.TempDir(), "approved-evidence.zip")
	writeMutationEvidenceArchive(t, older, map[string]string{
		"mutation-checkpoints/root.json": strings.Replace(
			checkpointFixture("pkg/example", "."),
			strings.Repeat("a", 64),
			strings.Repeat("d", 64),
			1,
		),
	})

	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	writeTestFile(t, gh, `#!/bin/sh
set -eu
case "$*" in
  *"actions/artifacts -f name=evidence-pkg-example"*)
    cat <<'JSON'
{"artifacts":[
  {"id":22,"expired":false,"created_at":"2026-08-24T08:00:00Z","workflow_run":{"id":222,"head_repository_id":123,"head_branch":"main"}},
  {"id":11,"expired":false,"created_at":"2026-08-24T07:00:00Z","workflow_run":{"id":111,"head_repository_id":123,"head_branch":"main"}}
]}
JSON
    ;;
  *"actions/artifacts/22/zip"*) cat "$FAKE_NEWEST_ARCHIVE" ;;
  *"actions/artifacts/11/zip"*) cat "$FAKE_OLDER_ARCHIVE" ;;
  *) printf 'unexpected gh arguments: %s\n' "$*" >&2; exit 2 ;;
esac
`)
	if err := os.Chmod(gh, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(
		filepath.Join(root, "scripts", "restore-ci-mutation-evidence.sh"),
		"pkg/example",
	)
	command.Env = os.Environ()
	for name, value := range map[string]string{
		"GOLIB_ROOT":           repository,
		"GH_TOKEN":             "test-token",
		"GITHUB_REPOSITORY":    "faustbrian/golib",
		"GITHUB_REPOSITORY_ID": "123",
		"GITHUB_RUN_ID":        "999",
		"GITHUB_SHA":           strings.Repeat("c", 40),
		"FAKE_NEWEST_ARCHIVE":  newest,
		"FAKE_OLDER_ARCHIVE":   older,
		"PATH":                 bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	} {
		command.Env = environmentWithValues(command.Env, name, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("restore approved mutation evidence: %v\n%s", err, output)
	}
	checkpoint := filepath.Join(
		repository,
		".artifacts/pkg/example/mutation-checkpoints/root.json",
	)
	contents, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatalf("read restored checkpoint: %v\n%s", err, output)
	}
	if !strings.Contains(string(contents), strings.Repeat("a", 64)) ||
		!strings.Contains(string(contents), `"verifier_identity_source": "approved-semantic-migration"`) {
		t.Fatalf("restored checkpoint = %s", contents)
	}
}

func writeMutationEvidenceArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, contents := range files {
		entry, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(contents)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func checkpointFixture(module, packageDirectory string) string {
	return fmt.Sprintf(`{
  "schema_version": 3,
  "module": %q,
  "package": %q,
  "execution_revision": "cccccccccccccccccccccccccccccccccccccccc",
  "gate_input_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "gremlins_version": "v0.6.0",
  "gremlins_verifier_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "gremlins_binary_sha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
  "verifier_identity_source": "executed",
  "report": {"files": [{"mutations": [{"status": "KILLED"}]}]}
}
`, module, packageDirectory)
}

func legacyCheckpointFixture(module, packageDirectory string) string {
	return strings.Replace(
		checkpointFixture(module, packageDirectory),
		`"gremlins_verifier_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`,
		`"gremlins_verifier_sha256": null`,
		1,
	)
}

func TestCanonicalMutationGateCannotDelegateToWeakerModuleTargets(t *testing.T) {
	root := testRepositoryRoot(t)
	moduleScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(moduleScript), "run_make_target mutation") {
		t.Fatal("module gate delegates mutation policy to package-local targets")
	}

	mutationScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-mutation.sh"))
	if err != nil {
		t.Fatal(err)
	}
	mutationRunner, err := os.ReadFile(filepath.Join(root, "scripts", "internal", "run-mutation.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mutationScript), `run-mutation.sh" enforce`) {
		t.Fatal("canonical mutation gate does not force enforcement mode")
	}
	mutationCommand, err := os.ReadFile(
		filepath.Join(root, "scripts", "internal", "mutation-command.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	mutationScratch, err := os.ReadFile(
		filepath.Join(root, "scripts", "internal", "mutation-scratch.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	mutationDigest, err := os.ReadFile(
		filepath.Join(root, "scripts", "gate-input-digest.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	contract := string(mutationRunner) + string(mutationCommand) +
		string(mutationScratch) + string(mutationDigest)
	for _, required := range []string{
		`build-golib-gremlins.sh`,
		`configure-mutation-workers.sh`,
		`mutation-coverage.sh`,
		`reuse-mutation-coverage.sh`,
		`coverage-profile.json`,
		`GOLIB_GREMLINS_COVERAGE_PROFILE`,
		`GOLIB_GREMLINS_COVERAGE_ELAPSED`,
		`PATH="$(dirname "${GOLIB_REAL_GO}"):${PATH}"`,
		`GOFLAGS="-modfile=${modfile} -mod=mod"`,
		`go mod edit -modfile="${modfile}"`,
		`.module_path, .directory`,
		`.coverage_required == true`,
		`.test_tags | map(select(. != "interoperability")) | join(",")`,
		`--exclude-files '^.+/'`,
		`--threshold-efficacy 100`,
		`--threshold-mcover 100`,
		`GOCACHE="${active_build_cache}"`,
		`mutation_environment+=( -u TEST262_ROOT)`,
		`append_value mutation-test-environment "TEST262_ROOT=unset"`,
		`mutation_scratch_initialize "${artifact}"`,
		`mutation_scratch_package_cache "${slug}"`,
		`trap 'mutation_scratch_on_signal 129' HUP`,
		`trap 'mutation_scratch_on_signal 130' INT`,
		`trap 'mutation_scratch_on_signal 143' TERM`,
		`find "${run_directory}" -depth -delete`,
		`.status != "KILLED"`,
		`mutation report unexpectedly contains no reviewed mutants`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("canonical mutation gate lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"continue-on-error",
		"mapfile",
		"MUTATION_DISCOVER_ONLY",
		`status == "TIMED OUT" then "KILLED"`,
	} {
		if strings.Contains(contract, forbidden) {
			t.Fatalf("canonical mutation gate contains forbidden bypass %q", forbidden)
		}
	}
}

func TestMutationEvidenceUsesContentAddressedCheckpoints(t *testing.T) {
	root := testRepositoryRoot(t)
	runner, err := os.ReadFile(filepath.Join(root, "scripts", "internal", "run-mutation.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(runner)
	for _, required := range []string{
		`gate-input-digest.sh`,
		`mutation-checkpoints`,
		`execution_revision=`,
		`execution_revisions`,
		`gate_input_digests`,
		`mutation-verifier-identity.sh`,
		`gremlins_verifier_sha256`,
		`gremlins_verifier_sha256s`,
		`gremlins_binary_sha256`,
		`gremlins_binary_sha256s`,
		`verifier_identity_source`,
		`approved-semantic-migration`,
		`select(. != null)`,
		`mutation-legacy`,
		`optional-mutation-digest.sh`,
		`observer-v1`,
		`legacy-stable`,
		`caller`,
		`migrated dependency-test-isolated mutation identity`,
		`migrated module-wide mutation identity`,
		`identity_lineage`,
		`migrated caller-dependent mutation identity`,
		`reuse-approved-mutation-checkpoint.sh`,
		`reused approved content-identical mutation evidence`,
		`write_aggregate`,
		`mv "${checkpoint_tmp}" "${checkpoint}"`,
		`.complete == true`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("mutation evidence contract lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		`historical_package_digest`,
		`checkpoint revision is unavailable`,
		`git archive`,
		`git cat-file`,
	} {
		if strings.Contains(contract, forbidden) {
			t.Fatalf("mutation evidence contract depends on repository history through %q", forbidden)
		}
	}
	if strings.Index(contract, `execution_revision="$(git -C "${root}" rev-parse HEAD)"`) >
		strings.Index(contract, `for package_directory in "${packages[@]}"`) {
		t.Fatal("mutation runner captures execution revision after package execution")
	}
	removeAggregate := strings.Index(contract, `rm -f "${report}"`)
	packageLoop := strings.Index(contract, `for package_directory in "${packages[@]}"`)
	if removeAggregate < 0 || packageLoop < 0 || removeAggregate > packageLoop {
		t.Fatal("mutation runner does not invalidate the previous aggregate before execution")
	}
	if _, err := os.Stat(filepath.Join(root, "scripts", "gate-input-digest.sh")); err != nil {
		t.Fatalf("mutation evidence fingerprint tool: %v", err)
	}
}

func TestMutationVerifierIdentityTracksBehavioralInputs(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		filepath.Join(repository, ".golib"),
		filepath.Join(repository, "scripts", "internal"),
		filepath.Join(repository, "scripts", "patches"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, ".golib", "versions.env"), `GREMLINS_VERSION=v0.6.0
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
`)
	for _, file := range []string{
		"scripts/internal/mutation-command.sh",
		"scripts/internal/mutation-coverage.sh",
		"scripts/patches/gremlins-run-all-mutants.patch",
		"scripts/patches/gremlins-shared-coverage.patch",
		"scripts/patches/gremlins-module-relative-diff.patch",
	} {
		writeFile(t, filepath.Join(repository, file), file+"\n")
	}
	unrelated := filepath.Join(repository, "scripts", "internal", "run-mutation.sh")
	writeFile(t, unrelated, "orchestration one\n")

	digest := func() string {
		t.Helper()
		command := exec.Command(filepath.Join(root, "scripts", "mutation-verifier-identity.sh"))
		command.Dir = repository
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("calculate mutation verifier identity: %v\n%s", err, output)
		}

		return strings.TrimSpace(string(output))
	}

	initial := digest()
	if len(initial) != 64 {
		t.Fatalf("mutation verifier identity length = %d", len(initial))
	}
	writeFile(t, unrelated, "orchestration two\n")
	if current := digest(); current != initial {
		t.Fatalf("orchestration changed verifier identity: %s != %s", current, initial)
	}
	patch := filepath.Join(repository, "scripts", "patches", "gremlins-run-all-mutants.patch")
	writeFile(t, patch, "changed mutation semantics\n")
	if current := digest(); current == initial {
		t.Fatal("mutation semantics did not change verifier identity")
	}
}

func TestGremlinsBuildCacheUsesCompleteVerifierIdentity(t *testing.T) {
	root := testRepositoryRoot(t)
	contents, err := os.ReadFile(filepath.Join(root, "scripts", "build-golib-gremlins.sh"))
	if err != nil {
		t.Fatal(err)
	}
	build := string(contents)
	identity := `verifier_identity="$("${root}/scripts/mutation-verifier-identity.sh")"`
	platform := `platform_identity="$(go env GOOS GOARCH | paste -sd- -)"`
	artifact := `artifact="${root}/.artifacts/tooling/gremlins-${verifier_identity}-${platform_identity}"`
	if !strings.Contains(build, identity) ||
		!strings.Contains(build, platform) ||
		!strings.Contains(build, artifact) {
		t.Fatal("Gremlins build cache is not bound to the complete verifier identity")
	}
	if strings.Index(build, identity) > strings.Index(build, `if [[ -x "${binary}" ]]`) {
		t.Fatal("Gremlins verifier identity is resolved after the build-cache early return")
	}
}

func TestOptionalMutationDigestTreatsUnavailableHistoryAsNoMatch(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(filepath.Join(repository, "scripts", "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile(filepath.Join(
		root,
		"scripts",
		"internal",
		"optional-mutation-digest.sh",
	))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "internal", "optional-mutation-digest.sh"),
		string(helper),
	)
	writeTestFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
if [ "${GOLIB_MUTATION_DIGEST_RESOLUTION:-}" = unavailable ]; then
	printf 'historical dependency cannot be resolved\n' >&2
	exit 23
fi
printf 'available-digest\n'
`)
	for _, path := range []string{
		"scripts/gate-input-digest.sh",
		"scripts/internal/optional-mutation-digest.sh",
	} {
		if err := os.Chmod(filepath.Join(repository, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}

	command := exec.Command(
		filepath.Join(repository, "scripts", "internal", "optional-mutation-digest.sh"),
		"unavailable",
		"pkg/example",
		".",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("unavailable historical digest was fatal: %v\n%s", err, output)
	} else if len(output) != 0 {
		t.Fatalf("unavailable historical digest output = %q, want empty", output)
	}

	command = exec.Command(
		filepath.Join(repository, "scripts", "internal", "optional-mutation-digest.sh"),
		"observer-v1",
		"pkg/example",
		".",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("available historical digest: %v\n%s", err, output)
	} else if string(output) != "available-digest\n" {
		t.Fatalf("available historical digest output = %q", output)
	}
}

func TestApprovedMutationCheckpointMigrationUsesExactInputIdentity(t *testing.T) {
	root := testRepositoryRoot(t)
	directory := t.TempDir()
	checkpoint := filepath.Join(directory, "checkpoint.json")
	ledger := filepath.Join(directory, "ledger.json")
	output := filepath.Join(directory, "migrated.json")
	writeFile(t, checkpoint, `{
  "schema_version": 3,
  "module": "pkg/example",
  "package": ".",
  "execution_revision": "original-execution",
  "validated_revision": "old-validation",
  "gate_input_digest": "old-input",
  "gremlins_version": "v0.6.0",
  "gremlins_binary_sha256": "historical-binary",
  "environment": {"GOOS": "linux"},
  "report": {
    "files": [{
      "file_name": "example.go",
      "mutations": [{"status": "KILLED", "type": "INVERT_LOGICAL"}]
    }]
  }
}`)

	reportDigestCommand := exec.Command("sh", "-c", `jq -S -c '.report' "$1" | shasum -a 256 | awk '{print $1}'`, "digest", checkpoint)
	reportDigestOutput, err := reportDigestCommand.Output()
	if err != nil {
		t.Fatalf("calculate fixture report digest: %v", err)
	}
	reportDigest := strings.TrimSpace(string(reportDigestOutput))
	writeFile(t, ledger, fmt.Sprintf(`{
  "schema_version": 3,
  "verifier_migrations": [{
    "module": "pkg/example",
    "package": ".",
    "execution_revision": "original-execution",
    "gate_input_digest": "old-input",
    "gremlins_version": "v0.6.0",
    "gremlins_verifier_sha256": "tool-identity",
    "report_sha256": %q
  }],
  "entries": [{
    "module": "pkg/example",
    "package": ".",
    "execution_revision": "original-execution",
    "gate_input_digest": "old-input",
    "replacement_gate_input_digest": "current-input",
    "gremlins_version": "v0.6.0",
    "report_sha256": %q
  }]
}`, reportDigest, reportDigest))

	script := filepath.Join(root, "scripts", "internal", "reuse-approved-mutation-checkpoint.sh")
	command := exec.Command(
		script,
		ledger,
		checkpoint,
		"pkg/example",
		".",
		"current-input",
		"v0.6.0",
		"tool-identity",
		"current-validation",
		output,
	)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reuse approved checkpoint: %v\n%s", err, result)
	}

	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var migrated struct {
		ExecutionRevision      string   `json:"execution_revision"`
		ValidatedRevision      string   `json:"validated_revision"`
		GateInputDigest        string   `json:"gate_input_digest"`
		GremlinsVerifierSHA256 string   `json:"gremlins_verifier_sha256"`
		VerifierIdentitySource string   `json:"verifier_identity_source"`
		IdentityLineage        []string `json:"identity_lineage"`
		IdentityMigration      struct {
			Reason                  string `json:"reason"`
			PreviousGateInputDigest string `json:"previous_gate_input_digest"`
		} `json:"identity_migration"`
	}
	if err := json.Unmarshal(contents, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.ExecutionRevision != "original-execution" {
		t.Fatalf("execution revision = %q", migrated.ExecutionRevision)
	}
	if migrated.ValidatedRevision != "current-validation" {
		t.Fatalf("validated revision = %q", migrated.ValidatedRevision)
	}
	if migrated.GateInputDigest != "current-input" {
		t.Fatalf("gate input digest = %q", migrated.GateInputDigest)
	}
	if migrated.GremlinsVerifierSHA256 != "tool-identity" {
		t.Fatalf("Gremlins verifier identity = %q", migrated.GremlinsVerifierSHA256)
	}
	if migrated.VerifierIdentitySource != "approved-semantic-migration" {
		t.Fatalf("verifier identity source = %q", migrated.VerifierIdentitySource)
	}
	if !slices.Contains(migrated.IdentityLineage, "old-input") {
		t.Fatalf("identity lineage = %v", migrated.IdentityLineage)
	}
	if migrated.IdentityMigration.Reason != "approved-input-identity-migration" ||
		migrated.IdentityMigration.PreviousGateInputDigest != "old-input" {
		t.Fatalf("identity migration = %+v", migrated.IdentityMigration)
	}

	directCheckpoint := filepath.Join(directory, "direct-checkpoint.json")
	writeFile(t, directCheckpoint, `{
  "schema_version": 3,
  "module": "pkg/example",
  "package": ".",
  "execution_revision": "original-execution",
  "validated_revision": "old-validation",
  "gate_input_digest": "current-input",
  "identity_lineage": ["old-input"],
  "gremlins_version": "v0.6.0",
  "gremlins_binary_sha256": "historical-binary",
  "environment": {"GOOS": "linux"},
  "report": {
    "files": [{
      "file_name": "example.go",
      "mutations": [{"status": "KILLED", "type": "INVERT_LOGICAL"}]
    }]
  }
}`)
	direct := exec.Command(
		script,
		ledger,
		directCheckpoint,
		"pkg/example",
		".",
		"current-input",
		"v0.6.0",
		"tool-identity",
		"current-validation",
		output,
	)
	if result, err := direct.CombinedOutput(); err != nil {
		t.Fatalf("bind verifier identity to current checkpoint: %v\n%s", err, result)
	}
	decodeJSONFile(t, output, &migrated)
	if migrated.GremlinsVerifierSHA256 != "tool-identity" ||
		migrated.GateInputDigest != "current-input" ||
		migrated.VerifierIdentitySource != "approved-semantic-migration" {
		t.Fatalf("direct verifier migration = %+v", migrated)
	}

	forgedCheckpoint := filepath.Join(directory, "forged-checkpoint.json")
	writeFile(t, forgedCheckpoint, `{
  "schema_version": 3,
  "module": "pkg/example",
  "package": ".",
  "execution_revision": "original-execution",
  "validated_revision": "old-validation",
  "gate_input_digest": "current-input",
  "gremlins_version": "v0.6.0",
  "gremlins_binary_sha256": "historical-binary",
  "environment": {"GOOS": "linux"},
  "report": {
    "files": [{
      "file_name": "forged.go",
      "mutations": [{"status": "KILLED", "type": "CONDITIONALS_NEGATION"}]
    }]
  }
}`)
	forged := exec.Command(
		script,
		ledger,
		forgedCheckpoint,
		"pkg/example",
		".",
		"current-input",
		"v0.6.0",
		"tool-identity",
		"current-validation",
		output,
	)
	if err := forged.Run(); err == nil {
		t.Fatal("migration accepted an unreviewed report from an approved execution revision")
	}

	rejected := exec.Command(
		script,
		ledger,
		checkpoint,
		"pkg/example",
		".",
		"different-input",
		"v0.6.0",
		"tool-identity",
		"current-validation",
		output,
	)
	if err := rejected.Run(); err == nil {
		t.Fatal("migration accepted an unapproved replacement input")
	}

	rejected = exec.Command(
		script,
		ledger,
		checkpoint,
		"pkg/example",
		".",
		"current-input",
		"v0.6.0",
		"different-tool-identity",
		"current-validation",
		output,
	)
	if err := rejected.Run(); err == nil {
		t.Fatal("migration accepted evidence from a different mutation binary")
	}
}

func TestMutationVerifierMigrationLedgerUsesExactCheckpointIdentities(t *testing.T) {
	root := testRepositoryRoot(t)
	var ledger struct {
		VerifierMigrationReview struct {
			GremlinsVerifierSHA256 string `json:"gremlins_verifier_sha256"`
			Reason                 string `json:"reason"`
			ReviewedAt             string `json:"reviewed_at"`
		} `json:"verifier_migration_review"`
		VerifierMigrations []struct {
			Module                 string `json:"module"`
			Package                string `json:"package"`
			ExecutionRevision      string `json:"execution_revision"`
			GateInputDigest        string `json:"gate_input_digest"`
			GremlinsVersion        string `json:"gremlins_version"`
			GremlinsVerifierSHA256 string `json:"gremlins_verifier_sha256"`
			ReportSHA256           string `json:"report_sha256"`
		} `json:"verifier_migrations"`
	}
	decodeJSONFile(
		t,
		filepath.Join(root, ".golib", "mutation-history-migrations.json"),
		&ledger,
	)
	if len(ledger.VerifierMigrations) != 557 {
		t.Fatalf("exact verifier migrations = %d, want 557", len(ledger.VerifierMigrations))
	}
	if ledger.VerifierMigrationReview.Reason == "" ||
		ledger.VerifierMigrationReview.ReviewedAt == "" {
		t.Fatal("verifier migration review metadata is incomplete")
	}
	seen := make(map[string]struct{}, len(ledger.VerifierMigrations))
	for _, migration := range ledger.VerifierMigrations {
		fields := []string{
			migration.Module,
			migration.Package,
			migration.ExecutionRevision,
			migration.GateInputDigest,
			migration.GremlinsVersion,
			migration.GremlinsVerifierSHA256,
			migration.ReportSHA256,
		}
		if slices.Contains(fields, "") {
			t.Fatalf("incomplete verifier migration: %+v", migration)
		}
		if migration.GremlinsVerifierSHA256 !=
			ledger.VerifierMigrationReview.GremlinsVerifierSHA256 {
			t.Fatalf("migration verifier identity differs from review: %+v", migration)
		}
		key := strings.Join(fields, "\x00")
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate verifier migration: %+v", migration)
		}
		seen[key] = struct{}{}
	}
}

func TestMutationDigestIgnoresCallerWorkspaceIsolation(t *testing.T) {
	root := testRepositoryRoot(t)
	script := filepath.Join(root, "scripts", "gate-input-digest.sh")
	run := func(environment []string) string {
		t.Helper()
		command := exec.Command(
			script,
			"mutation",
			"pkg/http-client",
			".",
		)
		command.Dir = root
		command.Env = environment
		output, err := command.Output()
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				t.Fatalf(
					"calculate mutation digest: %v\n%s",
					err,
					exitError.Stderr,
				)
			}
			t.Fatalf("calculate mutation digest: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}

	direct := directGoEnvironment(t)
	workspace := environmentWithValues(
		direct,
		"GOWORK",
		filepath.Join(root, "go.work"),
	)
	isolated := environmentWithValues(direct, "GOWORK", "off")
	isolated = environmentWithValues(
		isolated,
		"GOFLAGS",
		"-modfile=/unusable/foreign.mod -mod=readonly",
	)
	if workspaceDigest, isolatedDigest := run(workspace), run(isolated); workspaceDigest != isolatedDigest {
		t.Fatalf(
			"caller isolation changed mutation digest: %s != %s",
			workspaceDigest,
			isolatedDigest,
		)
	}
}

func TestMutationDigestTracksIntegrationInputsInsteadOfDocumentation(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		".golib",
		"pkg/dependency",
		"pkg/dependency/testdata",
		"pkg/example",
		"pkg/example/consumer",
		"pkg/unrelated",
		"scripts/internal",
		"scripts/patches",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/root\n\ngo 1.26.6\n")
	writeFile(t, filepath.Join(repository, "go.work"), `go 1.26.6

use (
	./pkg/example
	./pkg/unrelated
)
`)
	writeFile(t, filepath.Join(repository, "pkg", "unrelated", "go.mod"), `module example.test/unrelated

go 1.26.6

require example.invalid/unpublished v0.0.0-20990101000000-deadbeefdead
`)
	writeFile(t, filepath.Join(repository, "pkg", "unrelated", "unrelated.go"), "package unrelated\n")
	writeFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/example",
    "owned_dependencies": [],
    "test_tags": [],
    "required_services": ["postgresql"],
    "go_version": "1.26.6",
    "gates": {"mutation": true},
    "packages": [
      {"directory": ".", "coverage_required": true},
      {"directory": "consumer", "coverage_required": true},
      {"directory": "sibling", "coverage_required": true}
    ]
  }, {
    "directory": "pkg/dependency",
    "module_path": "example.test/dependency",
    "owned_dependencies": [],
    "test_tags": [],
    "required_services": [],
    "go_version": "1.26.6",
    "gates": {"mutation": true},
    "packages": []
  }]
}`)
	writeFile(t, filepath.Join(repository, "packages.json"), `{"packages":[]}`)
	versionsFile := filepath.Join(repository, ".golib", "versions.env")
	writeFile(t, versionsFile, `GREMLINS_VERSION=v0.6.0
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:first
`)
	writeFile(t, filepath.Join(repository, ".golib", "mutation-zero-inventory.json"), `{"packages":[]}`)
	for _, path := range []string{
		"scripts/build-local-proxy.sh",
		"scripts/build-golib-gremlins.sh",
		"scripts/check-mutation.sh",
		"scripts/internal/run-mutation.sh",
		"scripts/internal/isolated-go.sh",
		"scripts/internal/mutation-command.sh",
		"scripts/internal/mutation-coverage.sh",
		"scripts/package-source-digest.sh",
		"scripts/patches/gremlins-run-all-mutants.patch",
		"scripts/patches/gremlins-shared-coverage.patch",
		"scripts/patches/gremlins-module-relative-diff.patch",
		"scripts/start-services.sh",
	} {
		writeFile(t, filepath.Join(repository, path), path+"\n")
	}
	digestScript, err := os.ReadFile(filepath.Join(root, "scripts", "gate-input-digest.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), string(digestScript))
	if err := os.Chmod(filepath.Join(repository, "scripts", "gate-input-digest.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "pkg", "dependency", "go.mod"), "module example.test/dependency\n\ngo 1.26.6\n")
	dependencySum := filepath.Join(repository, "pkg", "dependency", "go.sum")
	writeFile(t, dependencySum, "example.test/archive v0.1.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	dependencySource := filepath.Join(repository, "pkg", "dependency", "dependency.go")
	dependencyTest := filepath.Join(repository, "pkg", "dependency", "dependency_test.go")
	dependencyFixture := filepath.Join(repository, "pkg", "dependency", "testdata", "value.txt")
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 1 }\n")
	writeFile(t, dependencyTest, "package dependency\n\n// Dependency tests are not observers of another module's mutants.\n")
	writeFile(t, dependencyFixture, "one\n")
	moduleManifest := filepath.Join(repository, "pkg", "example", "go.mod")
	writeFile(t, moduleManifest, `module example.test/example

go 1.26.6

require example.test/dependency v0.0.0

replace example.test/dependency => ../dependency
`)
	moduleSum := filepath.Join(repository, "pkg", "example", "go.sum")
	writeFile(t, moduleSum, "example.test/dependency v0.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	source := filepath.Join(repository, "pkg", "example", "example.go")
	writeFile(t, source, `package example

import "example.test/dependency"

func Value() int { return dependency.Value() }
`)
	if err := os.MkdirAll(filepath.Join(repository, "pkg", "example", "sibling"), 0o700); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(repository, "pkg", "example", "sibling", "sibling.go")
	writeFile(t, sibling, "package sibling\n\nfunc Value() int { return 1 }\n")
	consumerSource := filepath.Join(repository, "pkg", "example", "consumer", "consumer.go")
	consumerTest := filepath.Join(repository, "pkg", "example", "consumer", "consumer_test.go")
	writeFile(t, consumerSource, `package consumer

import example "example.test/example"

func Value() int { return example.Value() }
`)
	writeFile(t, consumerTest, `package consumer

import "testing"

func TestValue(t *testing.T) {
	if Value() != 1 {
		t.Fatal("wrong value")
	}
}
`)
	if err := os.MkdirAll(filepath.Join(repository, "pkg", "example", "testdata"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(repository, "pkg", "example", "testdata", "value.txt")
	writeFile(t, fixture, "one\n")
	readme := filepath.Join(repository, "pkg", "example", "README.md")
	writeFile(t, readme, "# Example\n")
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}

	digestWithResolution := func(resolution string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repository, "scripts", "gate-input-digest.sh"),
			"mutation",
			"pkg/example",
			".",
		)
		command.Dir = repository
		command.Env = directGoEnvironment(t)
		if resolution != "" {
			command.Env = environmentWithValues(
				command.Env,
				"GOLIB_MUTATION_DIGEST_RESOLUTION",
				resolution,
			)
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("calculate mutation digest: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	digest := func() string {
		t.Helper()

		return digestWithResolution("")
	}
	digestPackageWithResolution := func(packageDirectory string, resolution string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repository, "scripts", "gate-input-digest.sh"),
			"mutation",
			"pkg/example",
			packageDirectory,
		)
		command.Dir = repository
		command.Env = directGoEnvironment(t)
		if resolution != "" {
			command.Env = environmentWithValues(
				command.Env,
				"GOLIB_MUTATION_DIGEST_RESOLUTION",
				resolution,
			)
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("calculate %s mutation digest: %v\n%s", packageDirectory, err, output)
		}

		return strings.TrimSpace(string(output))
	}
	digestPackage := func(packageDirectory string) string {
		t.Helper()

		return digestPackageWithResolution(packageDirectory, "")
	}

	initial := digest()
	legacyInitial := digestWithResolution("legacy-stable")
	writeFile(t, moduleManifest, `module example.test/example

go 1.26.6

require example.test/dependency v0.0.0-20260728110331-b7c4c77520dd

replace example.test/dependency => ../dependency
`)
	if current := digest(); current != initial {
		t.Fatalf("owned dependency locator changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, moduleManifest, `module example.test/example

go 1.26.6

require example.test/dependency v0.0.0

replace example.test/dependency => ../dependency
`)
	writeFile(t, versionsFile, `GREMLINS_VERSION=v0.6.0
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest(); current != initial {
		t.Fatalf("unrelated tool version changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, versionsFile, `GREMLINS_VERSION=v0.6.0
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
POSTGRES_IMAGE=postgres:18.5-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest(); current == initial {
		t.Fatal("required service image did not change mutation digest")
	}
	writeFile(t, versionsFile, `GREMLINS_VERSION=v0.6.1
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest(); current == initial {
		t.Fatal("mutation tool version did not change mutation digest")
	}
	writeFile(t, versionsFile, `GREMLINS_VERSION=v0.6.0
GREMLINS_SUM=h1:source
GREMLINS_GOMOD_SUM=h1:module
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	moduleCatalog := filepath.Join(repository, "modules.json")
	catalogContents, err := os.ReadFile(moduleCatalog)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		moduleCatalog,
		strings.ReplaceAll(
			string(catalogContents),
			`"coverage_required": true}`,
			`"coverage_required": true, "build_required": true, "build_tags": []}`,
		),
	)
	if current := digest(); current != initial {
		t.Fatalf("non-mutation package metadata changed mutation digest: %s != %s", current, initial)
	}
	checkMutation := filepath.Join(repository, "scripts", "check-mutation.sh")
	writeFile(t, checkMutation, "revised evidence wrapper\n")
	if current := digest(); current != initial {
		t.Fatalf("evidence wrapper changed mutation digest: %s != %s", current, initial)
	}
	mutationRunner := filepath.Join(repository, "scripts", "internal", "run-mutation.sh")
	writeFile(t, mutationRunner, "revised evidence orchestrator\n")
	if current := digest(); current != initial {
		t.Fatalf("evidence orchestrator changed mutation digest: %s != %s", current, initial)
	}
	isolationRunner := filepath.Join(repository, "scripts", "internal", "isolated-go.sh")
	writeFile(t, isolationRunner, "revised opt-in environment contract\n")
	if current := digest(); current != initial {
		t.Fatalf("module isolation wrapper changed mutation digest: %s != %s", current, initial)
	}
	mutationCommand := filepath.Join(repository, "scripts", "internal", "mutation-command.sh")
	writeFile(t, mutationCommand, "revised mutation command\n")
	if current := digest(); current == initial {
		t.Fatal("mutation command did not change mutation digest")
	}
	writeFile(t, mutationCommand, "scripts/internal/mutation-command.sh\n")
	mutationCoverage := filepath.Join(repository, "scripts", "internal", "mutation-coverage.sh")
	writeFile(t, mutationCoverage, "revised mutation coverage command\n")
	if current := digest(); current == initial {
		t.Fatal("mutation coverage command did not change mutation digest")
	}
	writeFile(t, mutationCoverage, "scripts/internal/mutation-coverage.sh\n")
	coveragePatch := filepath.Join(
		repository,
		"scripts",
		"patches",
		"gremlins-shared-coverage.patch",
	)
	writeFile(t, coveragePatch, "revised shared coverage patch\n")
	if current := digest(); current == initial {
		t.Fatal("shared coverage patch did not change mutation digest")
	}
	writeFile(t, coveragePatch, "scripts/patches/gremlins-shared-coverage.patch\n")
	diffPatch := filepath.Join(
		repository,
		"scripts",
		"patches",
		"gremlins-module-relative-diff.patch",
	)
	writeFile(t, diffPatch, "revised module-relative diff patch\n")
	if current := digest(); current != initial {
		t.Fatalf("separate verifier identity changed package input digest: %s != %s", current, initial)
	}
	writeFile(t, diffPatch, "scripts/patches/gremlins-module-relative-diff.patch\n")
	writeFile(t, moduleSum, "example.test/dependency v0.0.0 h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n")
	if current := digest(); current != initial {
		t.Fatalf("module checksum changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, dependencySum, "example.test/archive v0.1.0 h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n")
	if current := digest(); current != initial {
		t.Fatalf("dependency checksum changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, readme, "# Revised documentation\n")
	if current := digest(); current != initial {
		t.Fatalf("documentation changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, dependencyTest, "package dependency\n\nfunc dependencyTestOnly() {}\n")
	if current := digest(); current != initial {
		t.Fatalf("dependency tests changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, dependencyFixture, "two\n")
	if current := digest(); current == initial {
		t.Fatal("dependency fixture did not change mutation digest")
	}
	writeFile(t, dependencyFixture, "one\n")
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 2 }\n")
	if current := digest(); current == initial {
		t.Fatal("dependency production source did not change mutation digest")
	}
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 1 }\n")
	writeFile(t, sibling, "package sibling\n\nfunc Value() int { return 2 }\n")
	if current := digest(); current != initial {
		t.Fatalf("unrelated sibling changed mutation digest: %s != %s", current, initial)
	}
	if current := digestWithResolution("legacy-stable"); current == legacyInitial {
		t.Fatal("legacy module-wide digest ignored an integration-tested sibling")
	}
	writeFile(t, sibling, "package sibling\n\nfunc Value() int { return 1 }\n")
	writeFile(t, consumerTest, `package consumer

import "testing"

func TestValue(t *testing.T) {
	if Value() < 1 {
		t.Fatal("wrong value")
	}
}
`)
	if current := digest(); current != initial {
		t.Fatalf("reverse-dependent test changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, consumerTest, `package consumer

import "testing"

func TestValue(t *testing.T) {
	if Value() != 1 {
		t.Fatal("wrong value")
	}
}
`)
	writeFile(t, consumerSource, `package consumer

import example "example.test/example"

func Value() int { return example.Value() + 0 }
`)
	if current := digest(); current != initial {
		t.Fatalf("reverse-dependent source changed mutation digest: %s != %s", current, initial)
	}
	writeFile(t, consumerSource, `package consumer

import example "example.test/example"

func Value() int { return example.Value() }
`)
	writeFile(t, fixture, "two\n")
	if current := digest(); current == initial {
		t.Fatal("test fixture did not change mutation digest")
	}
	writeFile(t, fixture, "one\n")
	writeFile(t, source, "package example\n\nfunc Value() int { return 2 }\n")
	if current := digest(); current == initial {
		t.Fatal("production source did not change mutation digest")
	}

	writeFile(t, consumerSource, `package consumer

import (
	example "example.test/example"
	"example.test/example/sibling"
)

func Value() int { return example.Value() + sibling.Value() }
`)
	siblingTest := filepath.Join(repository, "pkg", "example", "sibling", "sibling_test.go")
	writeFile(t, siblingTest, `package sibling

import "testing"

func TestValue(t *testing.T) {
	if Value() != 1 {
		t.Fatal("wrong value")
	}
}
`)
	initialConsumer := digestPackage("consumer")
	initialConsumerV1 := digestPackageWithResolution("consumer", "observer-v1")
	writeFile(t, siblingTest, `package sibling

import "testing"

func TestValue(t *testing.T) {
	if got := Value(); got != 1 {
		t.Fatalf("value = %d", got)
	}
}
`)
	if current := digestPackage("consumer"); current != initialConsumer {
		t.Fatalf("dependency tests changed consumer mutation digest: %s != %s", current, initialConsumer)
	}
	if current := digestPackageWithResolution("consumer", "observer-v1"); current == initialConsumerV1 {
		t.Fatal("observer-v1 digest did not retain dependency-test identity")
	}
}

func TestGateInputDigestTracksDocumentationOnlyForRelevantGates(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, ".golib"),
		filepath.Join(root, "pkg", "example"),
		filepath.Join(root, "scripts", "internal"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [
    {
      "directory": "pkg/example",
      "module_path": "example.test/example",
      "owned_dependencies": [],
      "required_services": ["postgresql"],
      "gates": {
        "documentation": true,
        "security": true,
        "tests": true
      },
      "packages": []
    },
    {
      "directory": "pkg/unrelated",
      "module_path": "example.test/unrelated",
      "owned_dependencies": [],
      "gates": {
        "tests": true
      },
      "packages": []
    }
  ]
}
`)
	writeTestFile(t, filepath.Join(root, "packages.json"), `{
  "packages": [
    {
      "module_directory": "pkg/example",
      "name": "example"
    },
    {
      "module_directory": "pkg/unrelated",
      "name": "unrelated"
    }
  ]
}
`)
	writeTestFile(t, filepath.Join(root, "pkg", "example", "go.mod"), `module example.test/example

go 1.26.6
`)
	workspace := filepath.Join(root, "go.work")
	writeTestFile(t, workspace, "go 1.26.6\n")
	agentPolicy := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, agentPolicy, "# Agent policy\n")
	gitleaksConfig := filepath.Join(root, ".gitleaks.toml")
	writeTestFile(t, gitleaksConfig, "[allowlist]\npaths = []\n")
	writeTestFile(
		t,
		filepath.Join(root, "pkg", "example", "example.go"),
		"package example\n",
	)
	readme := filepath.Join(root, "pkg", "example", "README.md")
	writeTestFile(t, readme, "# Example\n")
	llmsFull := filepath.Join(root, "pkg", "example", "llms-full.txt")
	writeTestFile(t, llmsFull, "# Complete documentation\n")
	guide := filepath.Join(root, "pkg", "example", "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(guide), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, guide, "# Guide\n")
	fixture := filepath.Join(root, "pkg", "example", "testdata", "README.md")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, fixture, "# Fixture\n")
	checkModuleScript := filepath.Join(root, "scripts", "check-module.sh")
	checkFuzzScript := filepath.Join(root, "scripts", "check-fuzz.sh")
	mutationCommandScript := filepath.Join(
		root,
		"scripts",
		"internal",
		"mutation-command.sh",
	)
	snapshotOrchestratorScript := filepath.Join(
		root,
		"scripts",
		"internal",
		"run-verification-snapshots.sh",
	)
	writeTestFile(t, checkModuleScript, "lint run --timeout=10m ./...\n")
	writeTestFile(t, checkFuzzScript, "check fuzz\n")
	writeTestFile(t, mutationCommandScript, "mutation command\n")
	writeTestFile(t, snapshotOrchestratorScript, "snapshot orchestration\n")
	versionsFile := filepath.Join(root, ".golib", "versions.env")
	mutationInventory := filepath.Join(root, ".golib", "mutation-zero-inventory.json")
	writeTestFile(t, versionsFile, `GOLANGCI_LINT_VERSION=v1.0.0
GITLEAKS_VERSION=v1.0.0
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:first
`)
	writeTestFile(t, mutationInventory, "{\"packages\":[]}\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = root
	if result, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, result)
	}
	commit := exec.Command(
		"git",
		"-c", "user.name=Test",
		"-c", "user.email=test@example.test",
		"commit", "--quiet", "-m", "fixture",
	)
	commit.Dir = root
	if result, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit fixture repository: %v\n%s", err, result)
	}

	digestAt := func(repository, gate string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			gate,
			"pkg/example",
		)
		command.Dir = repository
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s: %v\n%s", gate, err, result)
		}

		return strings.TrimSpace(string(result))
	}
	digest := func(gate string) string {
		t.Helper()

		return digestAt(root, gate)
	}

	testBefore := digest("test")
	lintBefore := digest("lint")
	benchmarkBefore := digest("benchmark")
	fuzzBefore := digest("fuzz")
	docsBefore := digest("docs")
	secretsBefore := digest("secrets")
	writeTestFile(t, gitleaksConfig, "[allowlist]\npaths = [\"fixture\"]\n")
	for _, gate := range []struct {
		name string
		want string
	}{
		{name: "test", want: testBefore},
		{name: "lint", want: lintBefore},
		{name: "benchmark", want: benchmarkBefore},
		{name: "fuzz", want: fuzzBefore},
		{name: "docs", want: docsBefore},
	} {
		if current := digest(gate.name); current != gate.want {
			t.Fatalf(
				"secret policy changed %s gate inputs: %s != %s",
				gate.name,
				current,
				gate.want,
			)
		}
	}
	if current := digest("secrets"); current == secretsBefore {
		t.Fatal("secret policy did not change secrets digest")
	}
	writeTestFile(t, gitleaksConfig, "[allowlist]\npaths = []\n")
	writeTestFile(t, agentPolicy, "# Revised agent policy\n")
	for _, gate := range []struct {
		name string
		want string
	}{
		{name: "test", want: testBefore},
		{name: "benchmark", want: benchmarkBefore},
		{name: "fuzz", want: fuzzBefore},
		{name: "docs", want: docsBefore},
		{name: "secrets", want: secretsBefore},
	} {
		if current := digest(gate.name); current != gate.want {
			t.Fatalf(
				"agent policy changed %s gate inputs: %s != %s",
				gate.name,
				current,
				gate.want,
			)
		}
	}
	writeTestFile(t, agentPolicy, "# Agent policy\n")
	writeTestFile(t, workspace, "go 1.26.6\n\nuse ./pkg/unrelated\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"unrelated workspace change altered isolated test digest: %s != %s",
			current,
			testBefore,
		)
	}
	if current := digest("benchmark"); current == benchmarkBefore {
		t.Fatal("workspace change did not alter workspace-backed benchmark digest")
	}
	writeTestFile(t, workspace, "go 1.26.6\n")
	writeTestFile(t, mutationInventory, "{\"packages\":[\"unrelated\"]}\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"mutation inventory changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	writeTestFile(t, versionsFile, `GOLANGCI_LINT_VERSION=v1.0.0
GITLEAKS_VERSION=v1.0.0
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest("test"); current != testBefore {
		t.Fatalf("unrelated tool version changed test digest: %s != %s", current, testBefore)
	}
	if current := digest("lint"); current != lintBefore {
		t.Fatalf("unrelated tool version changed lint digest: %s != %s", current, lintBefore)
	}
	writeTestFile(t, versionsFile, `GOLANGCI_LINT_VERSION=v1.0.1
GITLEAKS_VERSION=v1.0.0
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest("test"); current != testBefore {
		t.Fatalf("lint version changed test digest: %s != %s", current, testBefore)
	}
	if current := digest("lint"); current == lintBefore {
		t.Fatal("lint version did not change lint digest")
	}
	writeTestFile(t, versionsFile, `GOLANGCI_LINT_VERSION=v1.0.0
GITLEAKS_VERSION=v1.0.0
POSTGRES_IMAGE=postgres:18.5-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	if current := digest("test"); current == testBefore {
		t.Fatal("required service image did not change test digest")
	}
	writeTestFile(t, versionsFile, `GOLANGCI_LINT_VERSION=v1.0.0
GITLEAKS_VERSION=v1.0.0
POSTGRES_IMAGE=postgres:18.4-alpine
KEYCLOAK_IMAGE=keycloak:second
`)
	writeTestFile(t, mutationCommandScript, "revised mutation command\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"mutation-only tooling changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	if current := digest("fuzz"); current != fuzzBefore {
		t.Fatalf(
			"mutation-only tooling changed fuzz digest: %s != %s",
			current,
			fuzzBefore,
		)
	}
	writeTestFile(t, snapshotOrchestratorScript, "revised snapshot orchestration\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"snapshot-only orchestration changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	writeTestFile(t, checkFuzzScript, "revised check fuzz\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"fuzz-only tooling changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	if current := digest("fuzz"); current == fuzzBefore {
		t.Fatal("fuzz tooling did not change fuzz digest")
	}
	writeTestFile(t, checkFuzzScript, "check fuzz\n")
	writeTestFile(
		t,
		checkModuleScript,
		"lint run --allow-parallel-runners --timeout=10m ./...\n",
	)
	for _, gate := range []struct {
		name string
		want string
	}{
		{name: "test", want: testBefore},
		{name: "lint", want: lintBefore},
	} {
		if current := digest(gate.name); current != gate.want {
			t.Fatalf(
				"operational lint concurrency changed %s digest: %s != %s",
				gate.name,
				current,
				gate.want,
			)
		}
	}
	writeTestFile(t, checkModuleScript, "lint run --timeout=10m ./...\n")
	writeTestFile(t, checkModuleScript, "revised check module\n")
	if current := digest("test"); current == testBefore {
		t.Fatal("shared gate tooling did not change test digest")
	}
	writeTestFile(t, checkModuleScript, "lint run --timeout=10m ./...\n")
	legacyRootDocumentationGate := `        docs)
            applicable documentation || { skip_not_applicable documentation; return; }
            enable_local_proxy
            if target="$(find_make_target docs documentation)"; then
                make "${target}"
            else
                GOWORK=off go test ./... -run '^Example' -count=1
            fi
            ;;
`
	currentRootDocumentationGate := `        docs)
            applicable documentation || { skip_not_applicable documentation; return; }
            enable_local_proxy
            if [[ "${module}" == "." ]]; then
                GOWORK=off "${root}/scripts/check-documentation.sh"
            elif target="$(find_make_target docs documentation)"; then
                make "${target}"
            else
                GOWORK=off go test ./... -run '^Example' -count=1
            fi
            ;;
`
	writeTestFile(t, checkModuleScript, legacyRootDocumentationGate)
	nonDocumentationBefore := digest("test")
	documentationBefore := digest("docs")
	writeTestFile(t, checkModuleScript, currentRootDocumentationGate)
	if current := digest("test"); current != nonDocumentationBefore {
		t.Fatalf(
			"root documentation tooling changed test digest: %s != %s",
			current,
			nonDocumentationBefore,
		)
	}
	if current := digest("docs"); current == documentationBefore {
		t.Fatal("root documentation tooling did not change docs digest")
	}
	writeTestFile(t, checkModuleScript, "lint run --timeout=10m ./...\n")
	moduleCatalog := filepath.Join(root, "modules.json")
	catalogContents, err := os.ReadFile(moduleCatalog)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		moduleCatalog,
		strings.Replace(
			string(catalogContents),
			`"module_path": "example.test/unrelated"`,
			`"module_path": "example.test/revised-unrelated"`,
			1,
		),
	)
	for _, gate := range []struct {
		name string
		want string
	}{
		{name: "test", want: testBefore},
		{name: "docs", want: docsBefore},
		{name: "secrets", want: secretsBefore},
	} {
		if current := digest(gate.name); current != gate.want {
			t.Fatalf(
				"unrelated module policy changed %s digest: %s != %s",
				gate.name,
				current,
				gate.want,
			)
		}
	}
	unrelatedCatalogContents, err := os.ReadFile(moduleCatalog)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		moduleCatalog,
		strings.Replace(
			string(unrelatedCatalogContents),
			`"module_path": "example.test/example"`,
			`"module_path": "example.test/revised-example"`,
			1,
		),
	)
	if current := digest("test"); current == testBefore {
		t.Fatal("selected module policy did not change test digest")
	}
	writeTestFile(t, moduleCatalog, string(unrelatedCatalogContents))
	packageCatalog := filepath.Join(root, "packages.json")
	packageCatalogContents, err := os.ReadFile(packageCatalog)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		packageCatalog,
		strings.Replace(
			string(packageCatalogContents),
			`"name": "unrelated"`,
			`"name": "revised-unrelated"`,
			1,
		),
	)
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"unrelated package policy changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	unrelatedPackageCatalogContents, err := os.ReadFile(packageCatalog)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		packageCatalog,
		strings.Replace(
			string(unrelatedPackageCatalogContents),
			`"name": "example"`,
			`"name": "revised-example"`,
			1,
		),
	)
	if current := digest("test"); current == testBefore {
		t.Fatal("selected package policy did not change test digest")
	}
	writeTestFile(t, packageCatalog, string(unrelatedPackageCatalogContents))
	writeTestFile(t, readme, "# Revised example\n")
	if testAfter := digest("test"); testAfter != testBefore {
		t.Fatalf(
			"documentation changed test digest: %s != %s",
			testAfter,
			testBefore,
		)
	}
	docsAfterReadme := digest("docs")
	if docsAfterReadme == docsBefore {
		t.Fatal("documentation did not change docs digest")
	}
	secretsAfterReadme := digest("secrets")
	if secretsAfterReadme == secretsBefore {
		t.Fatal("documentation did not change secrets digest")
	}
	writeTestFile(t, guide, "# Revised guide\n")
	if testAfter := digest("test"); testAfter != testBefore {
		t.Fatalf(
			"documentation directory changed test digest: %s != %s",
			testAfter,
			testBefore,
		)
	}
	if docsAfter := digest("docs"); docsAfter == docsAfterReadme {
		t.Fatal("documentation directory did not change docs digest")
	}
	if secretsAfter := digest("secrets"); secretsAfter == secretsAfterReadme {
		t.Fatal("documentation directory did not change secrets digest")
	}
	docsBeforeLLMS := digest("docs")
	secretsBeforeLLMS := digest("secrets")
	writeTestFile(t, llmsFull, "# Revised complete documentation\n")
	if testAfter := digest("test"); testAfter != testBefore {
		t.Fatalf(
			"generated documentation changed test digest: %s != %s",
			testAfter,
			testBefore,
		)
	}
	if docsAfter := digest("docs"); docsAfter == docsBeforeLLMS {
		t.Fatal("generated documentation did not change docs digest")
	}
	if secretsAfter := digest("secrets"); secretsAfter == secretsBeforeLLMS {
		t.Fatal("generated documentation did not change secrets digest")
	}
	writeTestFile(
		t,
		filepath.Join(root, "pkg", "example", "example.go"),
		"package example\n\nconst Value = 1\n",
	)
	if testAfter := digest("test"); testAfter == testBefore {
		t.Fatal("production source did not change test digest")
	}
	writeTestFile(
		t,
		filepath.Join(root, "pkg", "example", "example.go"),
		"package example\n",
	)
	if testAfter := digest("test"); testAfter != testBefore {
		t.Fatalf(
			"restored production source did not restore test digest: %s != %s",
			testAfter,
			testBefore,
		)
	}
	writeTestFile(t, fixture, "# Revised fixture\n")
	if testAfter := digest("test"); testAfter == testBefore {
		t.Fatal("Markdown test fixture did not change test digest")
	}
	if err := os.Remove(fixture); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "repository")
	create := exec.Command(
		filepath.Join(repositoryRoot, "scripts", "create-verification-snapshot.sh"),
		root,
		snapshot,
	)
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create verification snapshot: %v\n%s", err, output)
	}
	if sourceDigest, snapshotDigest := digest("test"), digestAt(snapshot, "test"); sourceDigest != snapshotDigest {
		t.Fatalf(
			"deleted tracked input changed verification snapshot digest: %s != %s",
			sourceDigest,
			snapshotDigest,
		)
	}
}

func TestGateInputDigestScopesRootSecretPolicyToSecrets(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, ".golib"),
		filepath.Join(root, "scripts"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [{
    "directory": ".",
    "module_path": "example.test/root",
    "owned_dependencies": [],
    "required_services": [],
    "gates": {"security": true},
    "packages": []
  }]
}
`)
	writeTestFile(t, filepath.Join(root, "packages.json"), `{"packages":[]}`)
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.test/root\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(root, ".golib", "versions.env"), "GITLEAKS_VERSION=v1.0.0\n")
	writeTestFile(t, filepath.Join(root, "scripts", "check-module.sh"), "check module\n")
	gitleaksConfig := filepath.Join(root, ".gitleaks.toml")
	writeTestFile(t, gitleaksConfig, "[allowlist]\npaths = []\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = root
	if result, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, result)
	}

	digest := func(gate string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			gate,
			".",
		)
		command.Dir = root
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s: %v\n%s", gate, err, result)
		}

		return strings.TrimSpace(string(result))
	}

	formatBefore := digest("format-check")
	secretsBefore := digest("secrets")
	writeTestFile(t, gitleaksConfig, "[allowlist]\npaths = [\"fixture\"]\n")
	if current := digest("format-check"); current != formatBefore {
		t.Fatalf("secret policy changed root format inputs: %s != %s", current, formatBefore)
	}
	if current := digest("secrets"); current == secretsBefore {
		t.Fatal("secret policy did not change root secrets digest")
	}
}

func TestGateInputDigestScopesAPIBaselineToAPIGate(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, ".golib"),
		filepath.Join(root, "api"),
		filepath.Join(root, "scripts"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(root, "modules.json"), `{
  "modules": [{
    "directory": ".",
    "module_path": "example.test/root",
    "owned_dependencies": [],
    "required_services": [],
    "gates": {"api_compatibility": true},
    "packages": []
  }]
}
`)
	writeTestFile(t, filepath.Join(root, "packages.json"), `{"packages":[]}`)
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.test/root\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(root, ".golib", "versions.env"), "APIDIFF_VERSION=v1.0.0\n")
	writeTestFile(t, filepath.Join(root, "scripts", "check-module.sh"), "check module\n")
	baseline := filepath.Join(root, "api", "baseline.txt")
	writeTestFile(t, baseline, "first baseline\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if result, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, result)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = root
	if result, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, result)
	}

	digest := func(gate string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			gate,
			".",
		)
		command.Dir = root
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", root)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s: %v\n%s", gate, err, result)
		}

		return strings.TrimSpace(string(result))
	}

	raceBefore := digest("race")
	apiBefore := digest("api")
	writeTestFile(t, baseline, "second baseline\n")
	if current := digest("race"); current != raceBefore {
		t.Fatalf("API baseline changed race inputs: %s != %s", current, raceBefore)
	}
	if current := digest("api"); current == apiBefore {
		t.Fatal("API baseline did not change API digest")
	}
}

func TestGateInputDigestIgnoresVerificationOrchestrationImplementation(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{".golib", "pkg/example", "scripts"} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/package",
    "owned_dependencies": [],
    "required_services": [],
    "test_tags": [],
    "interoperability_tools": [],
    "gates": {},
    "packages": []
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, "packages.json"), `{"packages":[]}`)
	writeTestFile(t, filepath.Join(repository, "pkg/example/go.mod"), "module example.test/package\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(repository, ".golib", "versions.env"), "")
	for _, script := range []string{
		"check-module.sh",
		"create-verification-snapshot.sh",
		"run-modules.sh",
		"start-services.sh",
		"stop-services.sh",
	} {
		writeTestFile(t, filepath.Join(repository, "scripts", script), script+" first\n")
	}

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = repository
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, output)
	}

	digest := func(gate string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			gate,
			"pkg/example",
		)
		command.Dir = repository
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest gate inputs: %v\n%s", err, output)
		}

		return strings.TrimSpace(string(output))
	}

	baseline := digest("format-check")
	assuranceBaseline := digest("operational-assurance")
	runAssuranceDigestWithEnvironment := func(kernel, cgoEnabled string) ([]byte, error) {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			"operational-assurance",
			"pkg/example",
		)
		command.Dir = repository
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
		for name, value := range map[string]string{
			"GOLIB_ASSURANCE_GO_VERSION":  "go1.test",
			"GOLIB_ASSURANCE_GOOS":        "testos",
			"GOLIB_ASSURANCE_GOARCH":      "testarch",
			"GOLIB_ASSURANCE_CGO_ENABLED": cgoEnabled,
			"GOLIB_ASSURANCE_KERNEL":      kernel,
			"GOLIB_ASSURANCE_NODE":        "missing",
		} {
			command.Env = environmentWithValues(command.Env, name, value)
		}
		return command.CombinedOutput()
	}
	assuranceDigestWithEnvironment := func(kernel string) string {
		t.Helper()
		output, err := runAssuranceDigestWithEnvironment(kernel, "0")
		if err != nil {
			t.Fatalf("digest gate inputs with captured environment: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if first, second := assuranceDigestWithEnvironment("Kernel A"),
		assuranceDigestWithEnvironment("Kernel B"); first == second {
		t.Fatal("captured assurance environment did not alter aggregate inputs")
	}
	if output, err := runAssuranceDigestWithEnvironment("Kernel\nA", "0"); err == nil ||
		!strings.Contains(string(output), "contains control characters") {
		t.Fatalf("ambiguous assurance environment error = %v, output = %s", err, output)
	}
	if output, err := runAssuranceDigestWithEnvironment("Kernel A", "enabled"); err == nil ||
		!strings.Contains(string(output), "cgo override must be 0 or 1") {
		t.Fatalf("invalid assurance cgo error = %v, output = %s", err, output)
	}
	partial := exec.Command(
		filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
		"operational-assurance",
		"pkg/example",
	)
	partial.Dir = repository
	partial.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
	partial.Env = environmentWithValues(
		partial.Env,
		"GOLIB_ASSURANCE_GO_VERSION",
		"go1.test",
	)
	if output, err := partial.CombinedOutput(); err == nil ||
		!strings.Contains(string(output), "environment override is incomplete") {
		t.Fatalf("partial assurance environment error = %v, output = %s", err, output)
	}
	for _, script := range []string{
		"create-verification-snapshot.sh",
		"run-modules.sh",
		"stop-services.sh",
	} {
		writeTestFile(t, filepath.Join(repository, "scripts", script), script+" second\n")
		if current := digest("format-check"); current != baseline {
			t.Fatalf("orchestration-only %s changed gate inputs: %s != %s", script, current, baseline)
		}
	}
	if current := digest("operational-assurance"); current == assuranceBaseline {
		t.Fatal("runner changes did not alter aggregate assurance inputs")
	}

	writeTestFile(t, filepath.Join(repository, "scripts", "start-services.sh"), "changed service contract\n")
	if current := digest("format-check"); current == baseline {
		t.Fatal("service setup did not change gate inputs")
	}
}

func TestGateInputDigestScopesFormatterDispatchToFormatGates(t *testing.T) {
	t.Parallel()

	repositoryRoot := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{".golib", "pkg/example", "scripts"} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/package",
    "owned_dependencies": [],
    "required_services": [],
    "test_tags": [],
    "interoperability_tools": [],
    "gates": {},
    "packages": []
  }]
}
`)
	writeTestFile(t, filepath.Join(repository, "packages.json"), `{"packages":[]}`)
	writeTestFile(t, filepath.Join(repository, "pkg/example/go.mod"), "module example.test/package\n\ngo 1.26.6\n")
	writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "")
	for _, script := range []string{
		"create-verification-snapshot.sh",
		"run-modules.sh",
		"start-services.sh",
		"stop-services.sh",
	} {
		writeTestFile(t, filepath.Join(repository, "scripts", script), script+"\n")
	}
	checkModule := filepath.Join(repository, "scripts/check-module.sh")
	writeTestFile(t, checkModule, `
run_gate() {
    case "${selected}" in
        format)
            find . -name '*.go' -not -path './.tools/*' -print0 | xargs -0 gofmt -w
            ;;
        format-check)
            unformatted="$(find . -name '*.go' -not -path './.tools/*' -print0 | xargs -0 gofmt -l)"
            [[ -z "${unformatted}" ]] || {
                printf 'unformatted Go files:\n%s\n' "${unformatted}" >&2
                exit 1
            }
            ;;
        tidy-check)
            :
            ;;
    esac
}
`)

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = repository
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture repository: %v\n%s", err, output)
	}

	digest := func(gate string) string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repositoryRoot, "scripts", "gate-input-digest.sh"),
			gate,
			"pkg/example",
		)
		command.Dir = repository
		command.Env = environmentWithValues(os.Environ(), "GOLIB_ROOT", repository)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("digest %s inputs: %v\n%s", gate, err, output)
		}

		return strings.TrimSpace(string(output))
	}
	formatBefore := digest("format-check")
	assuranceBefore := digest("operational-assurance")
	writeTestFile(t, checkModule, `
run_gate() {
    case "${selected}" in
        format)
            if target="$(find_make_target format)"; then
                make GOWORK=off "${target}"
            else
                find . -name '*.go' -not -path './.tools/*' -print0 | xargs -0 gofmt -w
            fi
            ;;
        format-check)
            if target="$(find_make_target format-check)"; then
                make GOWORK=off "${target}"
            else
                unformatted="$(find . -name '*.go' -not -path './.tools/*' -print0 | xargs -0 gofmt -l)"
                [[ -z "${unformatted}" ]] || {
                    printf 'unformatted Go files:\n%s\n' "${unformatted}" >&2
                    exit 1
                }
            fi
            ;;
        tidy-check)
            :
            ;;
    esac
}
`)
	if current := digest("operational-assurance"); current != assuranceBefore {
		t.Fatalf("formatter dispatch changed assurance inputs: %s != %s", current, assuranceBefore)
	}
	if current := digest("format-check"); current == formatBefore {
		t.Fatal("formatter dispatch did not change format-check inputs")
	}
}

func TestRunnerIsolationEvidenceMigrationPreservesExecutedProof(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		"scripts",
		".artifacts/pkg/example/evidence",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "scripts/check-gates.txt"), "test\n")
	digestScript := filepath.Join(repository, "scripts/gate-input-digest.sh")
	writeFile(t, digestScript, `#!/bin/sh
if [ "${GOLIB_GATE_INPUT_POLICY:-current}" = legacy-runner-isolation ]; then
    printf 'legacy-digest\n'
else
    printf 'current-digest\n'
fi
`)
	if err := os.Chmod(digestScript, 0o700); err != nil {
		t.Fatal(err)
	}
	logContents := "test passed\n"
	logPath := filepath.Join(repository, ".artifacts/pkg/example/evidence/test.log")
	writeFile(t, logPath, logContents)
	logDigest := sha256.Sum256([]byte(logContents))
	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/example/evidence/test.json"),
		fmt.Sprintf(`{
  "schema_version": 1,
  "module": "pkg/example",
  "gate": "test",
  "result": "passed",
  "exit_code": 0,
  "execution_revision": "executed-proof",
  "completed_revision": "executed-proof",
  "input_digest": "legacy-digest",
  "completed_input_digest": "legacy-digest",
  "log_sha256": "%x",
  "started_at": "2026-08-11T00:00:00Z",
  "completed_at": "2026-08-11T00:01:00Z"
}`, logDigest),
	)
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	commit := exec.Command(
		"git", "-C", repository,
		"-c", "user.name=Test", "-c", "user.email=test@example.test",
		"commit", "--allow-empty", "-m", "test",
	)
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("create fixture revision: %v\n%s", err, output)
	}

	command := exec.Command(
		filepath.Join(root, "scripts/internal/migrate-runner-isolation-evidence.sh"),
		"pkg/example",
		"test",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("migrate evidence: %v\n%s", err, output)
	}

	migratedPath := filepath.Join(
		repository,
		".artifacts/pkg/example/evidence/by-input/test/current-digest.json",
	)
	contents, err := os.ReadFile(migratedPath)
	if err != nil {
		t.Fatal(err)
	}
	var migrated struct {
		ExecutionRevision string   `json:"execution_revision"`
		InputDigest       string   `json:"input_digest"`
		CompletedInput    string   `json:"completed_input_digest"`
		IdentityLineage   []string `json:"identity_lineage"`
		IdentityMigration struct {
			Reason                  string `json:"reason"`
			PreviousGateInputDigest string `json:"previous_gate_input_digest"`
		} `json:"identity_migration"`
	}
	if err := json.Unmarshal(contents, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.ExecutionRevision != "executed-proof" ||
		migrated.InputDigest != "current-digest" ||
		migrated.CompletedInput != "current-digest" ||
		!slices.Contains(migrated.IdentityLineage, "legacy-digest") ||
		migrated.IdentityMigration.Reason != "non-semantic-runner-isolation-scope-narrowing" ||
		migrated.IdentityMigration.PreviousGateInputDigest != "legacy-digest" {
		t.Fatalf("migrated evidence = %+v", migrated)
	}

	assuranceEvidence := fmt.Sprintf(`{
  "schema_version": 1,
  "module": "pkg/example",
  "gate": "operational-assurance",
  "result": "passed",
  "exit_code": 0,
  "execution_revision": "executed-proof",
  "completed_revision": "executed-proof",
  "input_digest": "legacy-digest",
  "completed_input_digest": "legacy-digest",
  "log_sha256": "%x"
}`, logDigest)
	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/example/evidence/operational-assurance.json"),
		assuranceEvidence,
	)
	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/example/evidence/operational-assurance.log"),
		logContents,
	)
	command = exec.Command(
		filepath.Join(root, "scripts/internal/migrate-runner-isolation-evidence.sh"),
		"pkg/example",
		"operational-assurance",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("skip aggregate assurance evidence: %v\n%s", err, output)
	}
	assurancePath := filepath.Join(
		repository,
		".artifacts/pkg/example/evidence/by-input/operational-assurance/current-digest.json",
	)
	if _, err := os.Stat(assurancePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("aggregate assurance evidence migrated across runner semantics: %v", err)
	}
}

func TestAPIBaselineEvidenceMigrationPreservesExecutedProof(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		"pkg/example/api",
		"scripts",
		".artifacts/pkg/example/evidence",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "pkg/example/api/baseline.txt"), "baseline\n")
	writeFile(t, filepath.Join(repository, "scripts/check-gates.txt"), "race\napi\n")
	digestScript := filepath.Join(repository, "scripts/gate-input-digest.sh")
	writeFile(t, digestScript, `#!/bin/sh
if [ "${GOLIB_GATE_INPUT_POLICY:-current}" = legacy-api-baseline ]; then
    printf 'legacy-digest\n'
else
    printf 'current-digest\n'
fi
`)
	if err := os.Chmod(digestScript, 0o700); err != nil {
		t.Fatal(err)
	}
	logContents := "race passed\n"
	logPath := filepath.Join(repository, ".artifacts/pkg/example/evidence/race.log")
	writeFile(t, logPath, logContents)
	logDigest := sha256.Sum256([]byte(logContents))
	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/example/evidence/race.json"),
		fmt.Sprintf(`{
  "schema_version": 1,
  "module": "pkg/example",
  "gate": "race",
  "result": "passed",
  "exit_code": 0,
  "execution_revision": "executed-proof",
  "completed_revision": "executed-proof",
  "input_digest": "legacy-digest",
  "completed_input_digest": "legacy-digest",
  "log_sha256": "%x",
  "started_at": "2026-08-11T00:00:00Z",
  "completed_at": "2026-08-11T00:01:00Z"
}`, logDigest),
	)
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	commit := exec.Command(
		"git", "-C", repository,
		"-c", "user.name=Test", "-c", "user.email=test@example.test",
		"commit", "--allow-empty", "-m", "test",
	)
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("create fixture revision: %v\n%s", err, output)
	}

	command := exec.Command(
		filepath.Join(root, "scripts/internal/migrate-api-baseline-evidence.sh"),
		"pkg/example",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("migrate evidence: %v\n%s", err, output)
	}

	migratedPath := filepath.Join(
		repository,
		".artifacts/pkg/example/evidence/by-input/race/current-digest.json",
	)
	contents, err := os.ReadFile(migratedPath)
	if err != nil {
		t.Fatal(err)
	}
	var migrated struct {
		ExecutionRevision string   `json:"execution_revision"`
		InputDigest       string   `json:"input_digest"`
		CompletedInput    string   `json:"completed_input_digest"`
		IdentityLineage   []string `json:"identity_lineage"`
		IdentityMigration struct {
			Reason                  string `json:"reason"`
			PreviousGateInputDigest string `json:"previous_gate_input_digest"`
		} `json:"identity_migration"`
	}
	if err := json.Unmarshal(contents, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.ExecutionRevision != "executed-proof" ||
		migrated.InputDigest != "current-digest" ||
		migrated.CompletedInput != "current-digest" ||
		!slices.Contains(migrated.IdentityLineage, "legacy-digest") ||
		migrated.IdentityMigration.Reason != "non-semantic-gate-input-scope-narrowing" ||
		migrated.IdentityMigration.PreviousGateInputDigest != "legacy-digest" {
		t.Fatalf("migrated evidence = %+v", migrated)
	}
	legacyContents, err := os.ReadFile(filepath.Join(
		repository,
		".artifacts/pkg/example/evidence/race.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	if string(legacyContents) != string(contents) {
		t.Fatal("legacy evidence pointer does not match migrated content")
	}

	if err := os.Remove(migratedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(strings.TrimSuffix(migratedPath, ".json") + ".log"); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/example/evidence/race.json"),
		fmt.Sprintf(`{
  "schema_version": 1,
  "module": "pkg/example",
  "gate": "race",
  "result": "passed",
  "exit_code": 0,
  "execution_revision": "different-input-proof",
  "completed_revision": "different-input-proof",
  "input_digest": "unrelated-digest",
  "completed_input_digest": "unrelated-digest",
  "log_sha256": "%x",
  "started_at": "2026-08-11T00:00:00Z",
  "completed_at": "2026-08-11T00:01:00Z"
}`, logDigest),
	)
	command = exec.Command(
		filepath.Join(root, "scripts/internal/migrate-api-baseline-evidence.sh"),
		"pkg/example",
	)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reject unrelated evidence: %v\n%s", err, output)
	}
	if _, err := os.Stat(migratedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unrelated evidence was migrated: %v", err)
	}
}

func TestGateEvidenceVerificationAndGoalAuditFailClosed(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	for _, directory := range []string{
		"scripts",
		".artifacts/pkg/sample/evidence",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []string{
		"audit-goals.sh",
		"verify-gate-evidence.sh",
	} {
		contents, err := os.ReadFile(filepath.Join(root, "scripts", script))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(repository, "scripts", script)
		writeFile(t, path, string(contents))
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	digestScript := filepath.Join(repository, "scripts", "gate-input-digest.sh")
	writeFile(t, digestScript, "#!/bin/sh\nprintf 'current-digest\\n'\n")
	if err := os.Chmod(digestScript, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(repository, "scripts", "check-gates.txt"),
		"test\nmutation\nnilaway\n",
	)
	writeFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/sample",
    "module_path": "example.test/sample",
    "goal_status": "implementation-evidence-inventoried",
    "goal_evidence": [{
      "file": "pkg/sample/.ai/GOAL.md",
      "requirements_sha256": "goal-digest",
      "implementation_evidence": ["pkg/sample/README.md"],
      "verification_gates": ["test", "mutation"],
      "implementation_status": "implemented-requires-fresh-verification"
    }, {
      "file": "pkg/sample/.ai/GOAL_SECURITY.md",
      "requirements_sha256": "future-goal-digest",
      "implementation_evidence": [],
      "verification_gates": [],
      "implementation_status": "future-not-started"
    }]
  }]
}`)
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize evidence repository: %v\n%s", err, output)
	}
	commit := exec.Command(
		"git",
		"-C",
		repository,
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.test",
		"commit",
		"--allow-empty",
		"-m",
		"test",
	)
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("create evidence revision: %v\n%s", err, output)
	}

	writeEvidence := func(gate, result, logContents string) {
		t.Helper()
		logPath := filepath.Join(
			repository,
			".artifacts/pkg/sample/evidence",
			gate+".log",
		)
		writeFile(t, logPath, logContents)
		logDigest := sha256.Sum256([]byte(logContents))
		writeFile(
			t,
			filepath.Join(
				repository,
				".artifacts/pkg/sample/evidence",
				gate+".json",
			),
			fmt.Sprintf(`{
  "schema_version": 1,
  "module": "pkg/sample",
  "gate": %q,
  "result": %q,
  "exit_code": 0,
  "execution_revision": "proof",
  "input_digest": "current-digest",
  "completed_input_digest": "current-digest",
  "log_sha256": "%x",
  "completed_at": "2026-07-24T00:00:00Z"
}`, gate, result, logDigest),
		)
	}
	writeEvidence("test", "passed", "test passed\n")
	writeEvidence("mutation", "passed", "mutation passed\n")

	verify := exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify current evidence: %v\n%s", err, output)
	}

	writeEvidence(
		"nilaway",
		"advisory",
		"[pkg/sample] NilAway advisory exit status: 1\n",
	)
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"nilaway",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify NilAway advisory evidence: %v\n%s", err, output)
	}

	writeEvidence("nilaway", "advisory", "nilaway passed without diagnostics\n")
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"nilaway",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted malformed NilAway advisory evidence:\n%s", output)
	}

	writeEvidence(
		"test",
		"advisory",
		"[pkg/sample] NilAway advisory exit status: 1\n",
	)
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted advisory evidence for a mandatory gate:\n%s", output)
	}

	writeEvidence(
		"conformance",
		"not_applicable",
		"[pkg/sample] conformance: not applicable by catalog policy\n",
	)
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"conformance",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify catalog-not-applicable evidence: %v\n%s", err, output)
	}
	writeEvidence("conformance", "not_applicable", "conformance skipped without catalog policy\n")
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"conformance",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted malformed not-applicable evidence:\n%s", output)
	}

	writeEvidence("test", "passed", "test passed\n")
	writeEvidence(
		"nilaway",
		"advisory",
		"[pkg/sample] NilAway advisory exit status: 1\n",
	)

	writeFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/sample/evidence/test.log"),
		"tampered\n",
	)
	verify = exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err == nil {
		t.Fatalf("verifier accepted a tampered log:\n%s", output)
	}
	writeEvidence("test", "passed", "test passed\n")

	audit := exec.Command(
		filepath.Join(repository, "scripts", "audit-goals.sh"),
		"pkg/sample",
	)
	audit.Dir = repository
	if output, err := audit.CombinedOutput(); err != nil {
		t.Fatalf("audit current goal evidence: %v\n%s", err, output)
	}
	var report struct {
		VerificationStatus string `json:"verification_status"`
		Goals              []struct {
			File               string `json:"file"`
			VerificationStatus string `json:"verification_status"`
		} `json:"goals"`
		GateEvidence []json.RawMessage `json:"gate_evidence"`
	}
	decodeJSONFile(
		t,
		filepath.Join(repository, ".artifacts/pkg/sample/goal-traceability.json"),
		&report,
	)
	if report.VerificationStatus != "verified" ||
		len(report.Goals) != 2 ||
		report.Goals[0].VerificationStatus != "verified" ||
		report.Goals[1].VerificationStatus != "deferred" ||
		len(report.GateEvidence) != 2 {
		t.Fatalf("goal audit report = %+v", report)
	}
}

func TestRootDocumentationGateRunsEveryCanonicalCheck(t *testing.T) {
	root := testRepositoryRoot(t)
	bin := t.TempDir()
	makeMarker := filepath.Join(t.TempDir(), "make-called")
	goMarker := filepath.Join(t.TempDir(), "go-called")
	cspellMarker := filepath.Join(t.TempDir(), "cspell-called")
	lycheeMarker := filepath.Join(t.TempDir(), "lychee-called")
	makePath := filepath.Join(bin, "make")
	writeFile(t, makePath, "#!/bin/sh\nprintf called >\"$MAKE_MARKER\"\nexit 99\n")
	if err := os.Chmod(makePath, 0o700); err != nil {
		t.Fatal(err)
	}
	goPath := filepath.Join(bin, "go")
	writeFile(t, goPath, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$GO_MARKER\"\nexit 0\n")
	if err := os.Chmod(goPath, 0o700); err != nil {
		t.Fatal(err)
	}
	cspellPath := filepath.Join(bin, "cspell")
	writeFile(t, cspellPath, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$CSPELL_MARKER\"\n")
	if err := os.Chmod(cspellPath, 0o700); err != nil {
		t.Fatal(err)
	}
	lycheePath := filepath.Join(bin, "lychee")
	writeFile(t, lycheePath, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$LYCHEE_MARKER\"\n")
	if err := os.Chmod(lycheePath, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(filepath.Join(root, "scripts", "check-module.sh"), ".", "docs")
	command.Dir = root
	command.Env = environmentWithValues(
		environmentWith("MAKE_MARKER", makeMarker),
		"GO_MARKER",
		goMarker,
	)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_LOCAL_PROXY",
		t.TempDir(),
	)
	command.Env = environmentWithValues(
		command.Env,
		"PATH",
		bin+":"+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(command.Env, "GOLIB_CSPELL", cspellPath)
	command.Env = environmentWithValues(command.Env, "GOLIB_LYCHEE", lycheePath)
	command.Env = environmentWithValues(command.Env, "CSPELL_MARKER", cspellMarker)
	command.Env = environmentWithValues(command.Env, "LYCHEE_MARKER", lycheeMarker)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("root documentation gate: %v\n%s", err, output)
	}
	if _, err := os.Stat(makeMarker); !os.IsNotExist(err) {
		t.Fatalf("root documentation gate delegated to root Makefile")
	}
	invocation, err := os.ReadFile(goMarker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(invocation), "./cmd/golib documentation") {
		t.Fatalf("root documentation invocation = %q", invocation)
	}
	for name, marker := range map[string]string{
		"cspell": cspellMarker,
		"lychee": lycheeMarker,
	} {
		invocation, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("read %s invocation: %v", name, err)
		}
		if !strings.Contains(string(invocation), "README.md") ||
			!strings.Contains(string(invocation), "docs/**/*.md") {
			t.Fatalf("%s invocation = %q", name, invocation)
		}
	}
}

func TestDocumentationGatePropagatesToolFailure(t *testing.T) {
	root := testRepositoryRoot(t)
	for _, test := range []struct {
		name       string
		cspellExit int
		lycheeExit int
	}{
		{name: "spelling", cspellExit: 16},
		{name: "links", lycheeExit: 17},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			goPath := filepath.Join(bin, "go")
			writeFile(t, goPath, "#!/bin/sh\nexit 0\n")
			if err := os.Chmod(goPath, 0o700); err != nil {
				t.Fatal(err)
			}
			cspellPath := filepath.Join(bin, "cspell")
			writeFile(t, cspellPath, fmt.Sprintf("#!/bin/sh\nexit %d\n", test.cspellExit))
			if err := os.Chmod(cspellPath, 0o700); err != nil {
				t.Fatal(err)
			}
			lycheePath := filepath.Join(bin, "lychee")
			writeFile(t, lycheePath, fmt.Sprintf("#!/bin/sh\nexit %d\n", test.lycheeExit))
			if err := os.Chmod(lycheePath, 0o700); err != nil {
				t.Fatal(err)
			}

			command := exec.Command(filepath.Join(root, "scripts", "check-documentation.sh"))
			command.Dir = root
			command.Env = environmentWithValues(os.Environ(), "PATH", bin+":"+os.Getenv("PATH"))
			command.Env = environmentWithValues(command.Env, "GOLIB_CSPELL", cspellPath)
			command.Env = environmentWithValues(command.Env, "GOLIB_LYCHEE", lycheePath)
			if output, err := command.CombinedOutput(); err == nil {
				t.Fatalf("documentation gate accepted failed %s check:\n%s", test.name, output)
			}
		})
	}
}

func TestRepositoryCheckIncludesGovernanceValidation(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	contents, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(contents),
		"repository-check: inventory cohesion specification-decisions operational-assurance root-test workflow-lint",
	) {
		t.Fatal("repository-check does not enforce cohesion, specification, and assurance validation")
	}
}

func TestAPIGateSupportsExplicitAndCanonicalBaselineScripts(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(script)
	for _, required := range []string{
		`target="$(find_make_target api-compat api-check api compatibility)"`,
		`[[ -x "./scripts/check-api.sh" ]]`,
		`GOWORK=off ./scripts/check-api.sh`,
		`install_go_tool`,
		`GOLIB_APIDIFF="${GOLIB_GO_TOOL_PATH}"`,
		`"${root}/scripts/check-api-baseline.sh" "${module}"`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("API gate dispatcher lacks %q", required)
		}
	}
}

func TestPackageAPIGatesUseCanonicalToolVersion(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	modules := []string{
		"api-query",
		"authentication",
		"authorization",
		"geo",
		"log",
		"openapi",
		"openrpc",
		"password",
		"postgres",
		"xsd",
	}
	for _, module := range modules {
		path := filepath.Join(root, "pkg", module, "scripts", "check-api.sh")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s API gate: %v", module, err)
		}
		if !strings.Contains(string(contents), "APIDIFF_VERSION") {
			t.Errorf("%s API gate does not consume APIDIFF_VERSION", module)
		}
		if !strings.Contains(string(contents), "GOLIB_APIDIFF") {
			t.Errorf("%s API gate does not support the isolated apidiff executable", module)
		}
	}

	authentication, err := os.ReadFile(
		filepath.Join(root, "pkg", "authentication", "scripts", "check-api.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, nested := range []string{"check jwt", "check oidc", "check authotel"} {
		if strings.Contains(string(authentication), nested) {
			t.Errorf("authentication API gate crosses module boundary %q", nested)
		}
	}
}

func TestModuleFormatGatesDelegateToExplicitMakeTargets(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	moduleScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"format", "format-check"} {
		gate := gate
		t.Run(gate, func(t *testing.T) {
			t.Parallel()

			repository := t.TempDir()
			for _, directory := range []string{".golib", "pkg/sample", "scripts"} {
				if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeTestFile(t, filepath.Join(repository, "scripts/check-module.sh"), string(moduleScript))
			if err := os.Chmod(filepath.Join(repository, "scripts/check-module.sh"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "")
			writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/sample",
    "module_path": "example.test/sample"
  }]
}
`)
			writeTestFile(t, filepath.Join(repository, "pkg/sample/go.mod"), "module example.test/sample\n\ngo 1.26.6\n")
			writeTestFile(t, filepath.Join(repository, "pkg/sample/sample.go"), "package sample\nfunc Value( ) int { return 1 }\n")
			writeTestFile(t, filepath.Join(repository, "pkg/sample/Makefile"), `
format:
	@printf '%s' format > "$(FORMAT_MARKER)"

format-check:
	@printf '%s' format-check > "$(FORMAT_MARKER)"
`)
			initialize := exec.Command("git", "init", "--quiet")
			initialize.Dir = repository
			if output, err := initialize.CombinedOutput(); err != nil {
				t.Fatalf("initialize fixture repository: %v\n%s", err, output)
			}

			marker := filepath.Join(t.TempDir(), "format-gate")
			command := exec.Command(filepath.Join(repository, "scripts/check-module.sh"), "pkg/sample", gate)
			command.Dir = repository
			command.Env = environmentWithValues(os.Environ(), "FORMAT_MARKER", marker)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("run %s gate: %v\n%s", gate, err, output)
			}
			invocation, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("read %s marker: %v", gate, err)
			}
			if string(invocation) != gate {
				t.Fatalf("%s marker = %q", gate, invocation)
			}
		})
	}
}

func TestRootFormatFallbackExcludesNestedModules(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	moduleScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"format", "format-check"} {
		gate := gate
		t.Run(gate, func(t *testing.T) {
			t.Parallel()

			repository := t.TempDir()
			for _, directory := range []string{".golib", "cmd/root", "pkg/nested", "scripts"} {
				if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeTestFile(t, filepath.Join(repository, "scripts/check-module.sh"), string(moduleScript))
			if err := os.Chmod(filepath.Join(repository, "scripts/check-module.sh"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(repository, ".golib/versions.env"), "")
			writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [
    {
      "directory": ".",
      "module_path": "example.test/root",
      "packages": [{"directory": "cmd/root"}]
    },
    {
      "directory": "pkg/nested",
      "module_path": "example.test/nested",
      "packages": [{"directory": "."}]
    }
  ]
}
`)
			writeTestFile(t, filepath.Join(repository, "go.mod"), "module example.test/root\n\ngo 1.26.6\n")
			writeTestFile(t, filepath.Join(repository, "cmd/root/main.go"), "package main\n\nfunc main() {}\n")
			writeTestFile(t, filepath.Join(repository, "pkg/nested/go.mod"), "module example.test/nested\n\ngo 1.26.6\n")
			nestedSource := filepath.Join(repository, "pkg/nested/nested.go")
			unformatted := "package nested\nfunc Value( ) int { return 1 }\n"
			writeTestFile(t, nestedSource, unformatted)
			initialize := exec.Command("git", "init", "--quiet")
			initialize.Dir = repository
			if output, err := initialize.CombinedOutput(); err != nil {
				t.Fatalf("initialize fixture repository: %v\n%s", err, output)
			}

			command := exec.Command(filepath.Join(repository, "scripts/check-module.sh"), ".", gate)
			command.Dir = repository
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("run root %s gate: %v\n%s", gate, err, output)
			}
			contents, err := os.ReadFile(nestedSource)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != unformatted {
				t.Fatalf("root %s gate modified nested-module source", gate)
			}
		})
	}
}

func TestModuleGateFallbacksCannotMaskFailingMakeTargets(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "scripts", "check-module.sh"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(script)
	gates, err := os.ReadFile(filepath.Join(root, "scripts", "check-gates.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gates), "benchmark\n") {
		t.Fatal("canonical module contract lacks benchmark gate")
	}
	if strings.Contains(contract, "run_make_target") {
		t.Fatal("module gate still conflates missing and failing Make targets")
	}
	for _, required := range []string{
		`target="$(find_make_target fuzz fuzz-smoke)"`,
		`target="$(find_make_target docs documentation)"`,
		`target="$(find_make_target api-compat api-check api compatibility)"`,
		`target="$(find_make_target conformance specification)"`,
		`target="$(find_make_target interoperability integration conformance)"`,
		"conformance is declared but has no command",
		`run_make_evidence conformance "${target}"`,
		`run_make_evidence interoperability "${target}"`,
		`"${root}/.artifacts/${module}/${selected}.txt"`,
		`target="$(find_make_target benchmark performance)"`,
		`elif interoperability_declared; then`,
		"interoperability is declared but has no command",
		"benchmark gate produced no Go benchmark results",
		`make GOWORK=off "${target}"`,
		`GOWORK=off go test ./... -run '^$' -bench . -benchmem`,
		`make "${target}"`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("module gate fail-closed dispatch lacks %q", required)
		}
	}
	if strings.Contains(contract, `make GOWORK="${root}/go.work" "${target}"`) {
		t.Fatal("package benchmark gate loads the repository workspace")
	}
}

func TestWSDLInteroperabilityUsesPinnedContainerRuntime(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	path := filepath.Join(root, "pkg", "wsdl", "scripts", "check-woden.sh")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read WSDL interoperability runner: %v", err)
	}

	runner := string(content)
	for _, forbidden := range []string{
		"command -v java",
		"command -v javac",
	} {
		if strings.Contains(runner, forbidden) {
			t.Errorf("WSDL interoperability runner depends on host runtime %q", forbidden)
		}
	}
	for _, required := range []string{
		"command -v docker",
		"docker run --rm",
		"eclipse-temurin:25-jdk@sha256:",
	} {
		if !strings.Contains(runner, required) {
			t.Errorf("WSDL interoperability runner lacks %q", required)
		}
	}
}

func TestModuleRunnerDoesNotLeakServiceEnvironmentBetweenModules(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	bin := filepath.Join(repository, "bin")
	for _, directory := range []string{bin, filepath.Join(repository, "scripts")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "run-modules.sh"),
		mustReadFile(t, filepath.Join(root, "scripts", "run-modules.sh")),
	)
	writeTestFile(t, filepath.Join(repository, "scripts", "start-services.sh"), `#!/usr/bin/env bash
set -euo pipefail
: >"$2"
: >"$3"
if [[ "$1" == "pkg/first" ]]; then
    printf '%s\n' 'MODULE_SERVICE_VALUE=first' >"$2"
fi
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "stop-services.sh"), `#!/usr/bin/env bash
set -euo pipefail
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "check-module.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "pkg/first" && "${MODULE_SERVICE_VALUE:-}" != "first" ]]; then
    printf '%s\n' 'first module did not receive its service environment' >&2
    exit 1
fi
if [[ "$1" == "pkg/second" && -n "${MODULE_SERVICE_VALUE+x}" ]]; then
    printf '%s\n' 'second module inherited the first module environment' >&2
    exit 1
fi
`)
	writeTestFile(t, filepath.Join(bin, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' pkg/first pkg/second
`)
	for _, executable := range []string{
		filepath.Join(repository, "scripts", "run-modules.sh"),
		filepath.Join(repository, "scripts", "start-services.sh"),
		filepath.Join(repository, "scripts", "stop-services.sh"),
		filepath.Join(repository, "scripts", "check-module.sh"),
		filepath.Join(bin, "go"),
	} {
		if err := os.Chmod(executable, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	command := exec.Command(
		filepath.Join(repository, "scripts", "run-modules.sh"),
		"format", "--modules", "pkg/first,pkg/second",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_VERIFICATION_SNAPSHOT",
		"1",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run sequential modules: %v\n%s", err, output)
	}
}

func TestReleaseSnapshotDoesNotExpandDependenciesTwice(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := t.TempDir()
	bin := filepath.Join(repository, "bin")
	state := filepath.Join(repository, "selector-arguments")
	for _, directory := range []string{bin, filepath.Join(repository, "scripts")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeTestFile(
		t,
		filepath.Join(repository, "scripts", "run-modules.sh"),
		mustReadFile(t, filepath.Join(root, "scripts", "run-modules.sh")),
	)
	writeTestFile(t, filepath.Join(repository, "scripts", "filter-releasable-modules.sh"), `#!/usr/bin/env bash
set -euo pipefail
cat
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "build-local-proxy.sh"), `#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$1"
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "start-services.sh"), `#!/usr/bin/env bash
set -euo pipefail
: >"$2"
: >"$3"
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "stop-services.sh"), `#!/usr/bin/env bash
set -euo pipefail
`)
	writeTestFile(t, filepath.Join(repository, "scripts", "run-gate-with-evidence.sh"), `#!/usr/bin/env bash
set -euo pipefail
`)
	writeTestFile(t, filepath.Join(bin, "go"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "run" ]]; then
    printf '%s\n' "$@" >"${TEST_STATE}"
    printf '%s\n' pkg/selected
    exit 0
fi
if [[ "$1" == "env" && "$2" == "GOPROXY" ]]; then
    printf '%s\n' 'https://proxy.golang.org,direct'
    exit 0
fi
if [[ "$1" == "env" && "$2" == "GONOSUMDB" ]]; then
    exit 0
fi
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 1
`)
	for _, executable := range []string{
		filepath.Join(repository, "scripts", "run-modules.sh"),
		filepath.Join(repository, "scripts", "filter-releasable-modules.sh"),
		filepath.Join(repository, "scripts", "build-local-proxy.sh"),
		filepath.Join(repository, "scripts", "start-services.sh"),
		filepath.Join(repository, "scripts", "stop-services.sh"),
		filepath.Join(repository, "scripts", "run-gate-with-evidence.sh"),
		filepath.Join(bin, "go"),
	} {
		if err := os.Chmod(executable, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	command := exec.Command(
		filepath.Join(repository, "scripts", "run-modules.sh"),
		"release-dry-run", "--modules", "pkg/selected",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		os.Environ(),
		"PATH",
		bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command.Env = environmentWithValues(command.Env, "TEST_STATE", state)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_VERIFICATION_SNAPSHOT",
		"1",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run release snapshot: %v\n%s", err, output)
	}
	arguments := strings.Fields(mustReadFile(t, state))
	if slices.Contains(arguments, "--dependencies") {
		t.Fatalf("release snapshot expanded dependencies again: %v", arguments)
	}
}

func TestGateEvidenceIsCheckpointedPerResult(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	runner, err := os.ReadFile(filepath.Join(root, "scripts", "run-modules.sh"))
	if err != nil {
		t.Fatalf("read module runner: %v", err)
	}
	snapshotRunner, err := os.ReadFile(filepath.Join(root, "scripts", "internal", "run-verification-snapshots.sh"))
	if err != nil {
		t.Fatalf("read snapshot runner: %v", err)
	}
	evidence, err := os.ReadFile(filepath.Join(root, "scripts", "run-gate-with-evidence.sh"))
	if err != nil {
		t.Fatalf("read evidence runner: %v", err)
	}
	fingerprint, err := os.ReadFile(filepath.Join(root, "scripts", "gate-input-digest.sh"))
	if err != nil {
		t.Fatalf("read gate fingerprint runner: %v", err)
	}

	for name, contract := range map[string]string{
		"module runner":   string(runner),
		"evidence runner": string(evidence),
	} {
		required := "check-gates.txt"
		if name == "evidence runner" {
			required = "gate-input-digest.sh"
		}
		if !strings.Contains(contract, required) {
			t.Errorf("%s lacks %q", name, required)
		}
	}
	if !strings.Contains(string(fingerprint), `append_value gate "${gate}"`) {
		t.Error("fingerprint runner lacks gate-specific input identity")
	}

	evidenceContract := string(evidence)
	for _, required := range []string{
		"execution_revision",
		"input_digest",
		"log_sha256",
		"completed_at",
		"revalidated_revision",
		"reuse_count",
		`trap 'exit 130' HUP INT TERM`,
		`mv "${temporary_log}" "${log}"`,
		`mv "${temporary_evidence}" "${evidence}"`,
	} {
		if !strings.Contains(evidenceContract, required) {
			t.Errorf("evidence runner lacks %q", required)
		}
	}
	if !strings.Contains(string(runner), "format|tidy|api-update)") {
		t.Error("module runner records verification evidence for mutating commands")
	}
	runnerContract := string(runner) + string(snapshotRunner)
	if strings.Contains(runnerContract, "mapfile") ||
		strings.Contains(runnerContract, "readarray") {
		t.Error("module runner requires Bash features unavailable on macOS")
	}
	for _, required := range []string{
		"create-verification-snapshot.sh",
		"GOLIB_VERIFICATION_SNAPSHOT",
	} {
		if !strings.Contains(runnerContract, required) {
			t.Errorf("module runner lacks parallel-safe snapshot contract %q", required)
		}
	}
}

func TestVerificationSnapshotMirrorsDirtyTreeWithoutSharingWrites(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
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
	writeFile(t, filepath.Join(repository, ".gitignore"), "ignored.txt\nstaged-ignored.txt\n.artifacts/\n")
	writeFile(t, filepath.Join(repository, "tracked.txt"), "committed\n")
	writeFile(t, filepath.Join(repository, "removed.txt"), "remove me\n")
	runGit("add", ".gitignore", "tracked.txt", "removed.txt")
	runGit("commit", "-m", "initial")

	writeFile(t, filepath.Join(repository, "tracked.txt"), "working tree\n")
	if err := os.Remove(filepath.Join(repository, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "untracked.txt"), "untracked\n")
	writeFile(t, filepath.Join(repository, "ignored.txt"), "ignored\n")
	writeFile(t, filepath.Join(repository, "staged-ignored.txt"), "staged and tracked\n")
	runGit("add", "-f", "staged-ignored.txt")

	snapshot := filepath.Join(t.TempDir(), "snapshot")
	command := exec.Command(
		filepath.Join(root, "scripts", "create-verification-snapshot.sh"),
		repository,
		snapshot,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create verification snapshot: %v\n%s", err, output)
	}

	for path, expected := range map[string]string{
		"staged-ignored.txt": "staged and tracked\n",
		"tracked.txt":        "working tree\n",
		"untracked.txt":      "untracked\n",
	} {
		contents, err := os.ReadFile(filepath.Join(snapshot, path))
		if err != nil {
			t.Fatalf("read snapshot %s: %v", path, err)
		}
		if string(contents) != expected {
			t.Fatalf("snapshot %s = %q, want %q", path, contents, expected)
		}
	}
	for _, path := range []string{"removed.txt", "ignored.txt"} {
		if _, err := os.Stat(filepath.Join(snapshot, path)); !os.IsNotExist(err) {
			t.Fatalf("snapshot unexpectedly contains %s: %v", path, err)
		}
	}
	index := exec.Command("git", "-C", snapshot, "ls-files", "--cached")
	indexOutput, err := index.Output()
	if err != nil {
		t.Fatalf("read snapshot index: %v", err)
	}
	if strings.Contains(string(indexOutput), "removed.txt") {
		t.Fatalf("snapshot index retained removed path:\n%s", indexOutput)
	}
	if !strings.Contains(string(indexOutput), "staged-ignored.txt") {
		t.Fatalf("snapshot index omitted staged ignored path:\n%s", indexOutput)
	}

	writeFile(t, filepath.Join(snapshot, "tracked.txt"), "snapshot only\n")
	contents, err := os.ReadFile(filepath.Join(repository, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "working tree\n" {
		t.Fatalf("snapshot write changed source to %q", contents)
	}

	writeFile(t, filepath.Join(snapshot, ".artifacts", "proof.txt"), "shared\n")
	contents, err = os.ReadFile(filepath.Join(repository, ".artifacts", "proof.txt"))
	if err != nil {
		t.Fatalf("read shared artifact: %v", err)
	}
	if string(contents) != "shared\n" {
		t.Fatalf("shared artifact = %q, want shared", contents)
	}

	command = exec.Command(
		"git",
		"-C",
		snapshot,
		"ls-files",
		"--others",
		"--exclude-standard",
		"--",
		".artifacts",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect snapshot artifacts: %v\n%s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("shared artifact mount appears in snapshot inputs: %q", output)
	}
}

func TestVerificationSnapshotWaitsForDescendantsBeforeCleanup(t *testing.T) {
	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	state := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository, "cmd", "golib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "scripts", "run-modules.sh"), mustReadFile(t, filepath.Join(root, "scripts", "run-modules.sh")))
	if err := os.MkdirAll(filepath.Join(repository, "scripts", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(repository, "scripts", "internal", "run-verification-snapshots.sh"),
		mustReadFile(t, filepath.Join(root, "scripts", "internal", "run-verification-snapshots.sh")),
	)
	writeFile(t, filepath.Join(repository, "scripts", "create-verification-snapshot.sh"), `#!/usr/bin/env bash
set -euo pipefail
snapshot="$2"
mkdir -p "${snapshot}/scripts"
printf '%s\n' "${snapshot}" >"${TEST_STATE}/snapshot"
cat >"${snapshot}/scripts/run-modules.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
(
    trap '
        if [[ -d "${PWD}" ]]; then
            printf present >"${TEST_STATE}/snapshot-state"
        else
            printf missing >"${TEST_STATE}/snapshot-state"
        fi
        exit 0
    ' TERM
    printf ready >"${TEST_STATE}/ready"
    while true; do sleep 1; done
) &
wait "$!"
SCRIPT
chmod +x "${snapshot}/scripts/run-modules.sh"
`)
	writeFile(t, filepath.Join(repository, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf 'pkg/example\n'
`)
	for _, path := range []string{
		filepath.Join(repository, "scripts", "run-modules.sh"),
		filepath.Join(repository, "scripts", "internal", "run-verification-snapshots.sh"),
		filepath.Join(repository, "scripts", "create-verification-snapshot.sh"),
		filepath.Join(repository, "go"),
	} {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = repository
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}

	commandContext, cancelCommand := context.WithTimeout(context.Background(), time.Minute)
	defer cancelCommand()
	command := exec.CommandContext(
		commandContext,
		filepath.Join(repository, "scripts", "run-modules.sh"),
		"check",
		"--modules", "pkg/example",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		environmentWithValues(os.Environ(), "PATH", repository+string(os.PathListSeparator)+os.Getenv("PATH")),
		"TEST_STATE", state,
	)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_VERIFICATION_SNAPSHOT",
		"0",
	)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}()

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	waitForFile(t, startupContext, filepath.Join(state, "ready"))
	cancelStartup()
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate module runner: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("terminated module runner exited successfully")
	} else if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 143 {
		t.Fatalf("terminated module runner exit = %v, want 143\n%s", err, output.String())
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	waitForFile(t, shutdownContext, filepath.Join(state, "snapshot-state"))
	contents, err := os.ReadFile(filepath.Join(state, "snapshot-state"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "present" {
		t.Fatalf("snapshot state during descendant shutdown = %q, want present", contents)
	}
	snapshot, err := os.ReadFile(filepath.Join(state, "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(snapshot))); !os.IsNotExist(err) {
		t.Fatalf("verification snapshot remains after shutdown: %v", err)
	}
}

func TestGateEvidenceSerializesConcurrentSameGate(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.6\n")
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
printf 'stable-input\n'
`)
	writeFile(t, filepath.Join(repository, "scripts", "check-module.sh"), `#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
if ! mkdir "$root/only-once" 2>/dev/null; then
	printf 'gate executed concurrently\n' >&2
	exit 77
fi
sleep 1
printf 'gate passed\n'
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

	evidenceRunner := filepath.Join(
		testRepositoryRoot(t),
		"scripts",
		"run-gate-with-evidence.sh",
	)
	first := exec.Command(evidenceRunner, ".", "test")
	second := exec.Command(evidenceRunner, ".", "test")
	first.Dir = repository
	second.Dir = repository
	var firstOutput bytes.Buffer
	var secondOutput bytes.Buffer
	first.Stdout = &firstOutput
	first.Stderr = &firstOutput
	second.Stdout = &secondOutput
	second.Stderr = &secondOutput
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first evidence run: %v\n%s", err, firstOutput.String())
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("second evidence run: %v\n%s", err, secondOutput.String())
	}
	if !strings.Contains(
		firstOutput.String()+secondOutput.String(),
		"evidence: reused",
	) {
		t.Fatalf(
			"concurrent evidence did not reuse checkpoint:\n%s\n%s",
			firstOutput.String(),
			secondOutput.String(),
		)
	}
}

func TestGateEvidenceRecoversOwnerlessLock(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.6\n")
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
printf 'stable-input\n'
`)
	writeFile(t, filepath.Join(repository, "scripts", "check-module.sh"), `#!/bin/sh
set -eu
printf 'gate passed\n'
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

	lock := filepath.Join(repository, ".artifacts", "evidence", ".locks", "test.lock")
	if err := os.MkdirAll(lock, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(testRepositoryRoot(t), "scripts", "run-gate-with-evidence.sh"),
		".",
		"test",
	)
	command.Dir = repository
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("recover ownerless evidence lock: %v\n%s", err, output)
	}
}

func TestGateEvidenceVerifierWaitsForAtomicPublication(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	for _, directory := range []string{
		"scripts",
		".artifacts/evidence/.locks",
		".artifacts/evidence/by-input/test",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []string{"verify-gate-evidence.sh"} {
		contents, err := os.ReadFile(filepath.Join(root, "scripts", script))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(repository, "scripts", script)
		writeFile(t, path, string(contents))
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
printf 'stable-input\n'
`)
	if err := os.Chmod(filepath.Join(repository, "scripts", "gate-input-digest.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize evidence repository: %v\n%s", err, output)
	}

	evidenceDirectory := filepath.Join(
		repository,
		".artifacts/evidence/by-input/test",
	)
	logPath := filepath.Join(evidenceDirectory, "stable-input.log")
	evidencePath := filepath.Join(evidenceDirectory, "stable-input.json")
	writeFile(t, logPath, "publication in progress\n")
	writeFile(t, evidencePath, "{}\n")
	validLog := "gate passed\n"
	validLogDigest := sha256.Sum256([]byte(validLog))
	validEvidence := fmt.Sprintf(`{
  "schema_version": 1,
  "module": ".",
  "gate": "test",
  "result": "passed",
  "exit_code": 0,
  "input_digest": "stable-input",
  "completed_input_digest": "stable-input",
  "log_sha256": "%x"
}`, validLogDigest)
	validLogPath := filepath.Join(repository, "valid.log")
	validEvidencePath := filepath.Join(repository, "valid.json")
	writeFile(t, validLogPath, validLog)
	writeFile(t, validEvidencePath, validEvidence)
	ready := filepath.Join(repository, "writer-ready")
	lock := filepath.Join(repository, ".artifacts/evidence/.locks/test.lock")

	writer := exec.Command("sh", "-c", `
set -eu
ln -s "$$" "$LOCK"
: >"$READY"
sleep 0.2
cp "$VALID_LOG" "$LOG"
sleep 0.05
cp "$VALID_EVIDENCE" "$EVIDENCE"
rm "$LOCK"
`)
	writer.Env = append(os.Environ(),
		"LOCK="+lock,
		"READY="+ready,
		"VALID_LOG="+validLogPath,
		"VALID_EVIDENCE="+validEvidencePath,
		"LOG="+logPath,
		"EVIDENCE="+evidencePath,
	)
	writer.Dir = repository
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if writer.ProcessState == nil {
			_ = writer.Process.Kill()
			_ = writer.Wait()
		}
	})
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("evidence writer did not acquire its lock")
		}
		time.Sleep(time.Millisecond)
	}

	verify := exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		".",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify during publication: %v\n%s", err, output)
	}
	if err := writer.Wait(); err != nil {
		t.Fatalf("evidence writer: %v", err)
	}
}

func TestGateEvidencePreservesEachInputDigestAcrossSharedArtifacts(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(filepath.Join(repository, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		"audit-goals.sh",
		"verify-gate-evidence.sh",
	} {
		contents, err := os.ReadFile(filepath.Join(root, "scripts", script))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(repository, "scripts", script)
		writeFile(t, path, string(contents))
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.6\n")
	writeFile(t, filepath.Join(repository, "input-digest"), "input-a\n")
	writeFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
cat "$root/input-digest"
`)
	writeFile(t, filepath.Join(repository, "scripts", "check-module.sh"), `#!/bin/sh
set -eu
printf 'gate passed\n'
`)
	writeFile(t, filepath.Join(repository, "scripts", "check-gates.txt"), "test\n")
	writeFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": ".",
    "module_path": "example.test/evidence",
    "goal_status": "implementation-evidence-inventoried",
    "goal_evidence": [{
      "file": ".ai/GOAL.md",
      "requirements_sha256": "goal-digest",
      "implementation_evidence": ["go.mod"],
      "verification_gates": ["test"],
      "implementation_status": "implemented-requires-fresh-verification"
    }]
  }]
}`)
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
	runGit("add", "go.mod", "input-digest", "modules.json", "scripts")
	runGit("commit", "-m", "initial")

	evidenceRunner := filepath.Join(root, "scripts", "run-gate-with-evidence.sh")
	runEvidence := func() {
		t.Helper()
		command := exec.Command(evidenceRunner, ".", "test")
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("run evidence: %v\n%s", err, output)
		}
	}
	runEvidence()
	writeFile(t, filepath.Join(repository, "input-digest"), "input-b\n")
	runEvidence()
	writeFile(t, filepath.Join(repository, "input-digest"), "input-a\n")

	verify := exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		".",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify first input after shared alias advanced: %v\n%s", err, output)
	}

	audit := exec.Command(
		filepath.Join(repository, "scripts", "audit-goals.sh"),
		".",
	)
	audit.Dir = repository
	if output, err := audit.CombinedOutput(); err != nil {
		t.Fatalf("audit first input after shared alias advanced: %v\n%s", err, output)
	}
	var report struct {
		GateEvidence []struct {
			InputDigest string `json:"input_digest"`
		} `json:"gate_evidence"`
	}
	decodeJSONFile(
		t,
		filepath.Join(repository, ".artifacts", "goal-traceability.json"),
		&report,
	)
	if len(report.GateEvidence) != 1 ||
		report.GateEvidence[0].InputDigest != "input-a" {
		t.Fatalf("goal audit gate evidence = %+v, want input-a", report.GateEvidence)
	}
}

func TestMutationCoverageUsesBoundedGoDeadline(t *testing.T) {
	root := testRepositoryRoot(t)
	bin := t.TempDir()
	invocation := filepath.Join(t.TempDir(), "go-invocation")
	fakeGo := filepath.Join(bin, "go")
	writeTestFile(t, fakeGo, `#!/bin/sh
set -eu
printf '%s\n' "$*" >"$GOLIB_FAKE_GO_OUTPUT"
`)
	if err := os.Chmod(fakeGo, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(
		filepath.Join(root, "scripts", "internal", "mutation-coverage.sh"),
		filepath.Join(t.TempDir(), "coverage.out"),
		"integration",
	)
	command.Dir = root
	command.Env = environmentWithValues(
		environmentWithValues(os.Environ(), "PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")),
		"GOLIB_FAKE_GO_OUTPUT",
		invocation,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run mutation coverage: %v\n%s", err, output)
	}

	arguments, err := os.ReadFile(invocation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(arguments), "-timeout=20m") {
		t.Fatalf("mutation coverage Go invocation is unbounded: %s", arguments)
	}
}

func TestCoverageProfileIdentityRejectsInputsChangedDuringExecution(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	repository := filepath.Join(t.TempDir(), "repository")
	for _, directory := range []string{"scripts", "pkg/example", "bin"} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	coverageScript, err := os.ReadFile(filepath.Join(root, "scripts", "check-coverage.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "scripts", "check-coverage.sh"), string(coverageScript))
	if err := os.Chmod(filepath.Join(repository, "scripts", "check-coverage.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "scripts", "gate-input-digest.sh"), `#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
cat "$root/input-digest"
`)
	if err := os.Chmod(filepath.Join(repository, "scripts", "gate-input-digest.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "test_tags": [],
    "packages": [{
      "coverage_required": true,
      "import_path": "example.test/example"
    }]
  }]
}
`)
	digestFile := filepath.Join(repository, "input-digest")
	writeTestFile(t, digestFile, "before\n")
	fakeGo := filepath.Join(repository, "bin", "go")
	writeTestFile(t, fakeGo, `#!/bin/sh
set -eu
profile=
for argument in "$@"; do
    case "$argument" in
        -coverprofile=*) profile=${argument#-coverprofile=} ;;
    esac
done
test -n "$profile"
printf 'mode: atomic\nexample.test/example/example.go:1.1,1.2 1 1\n' >"$profile"
printf 'after\n' >"$GOLIB_COVERAGE_DIGEST_FILE"
`)
	if err := os.Chmod(fakeGo, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize coverage fixture: %v\n%s", err, output)
	}

	command := exec.Command(
		filepath.Join(repository, "scripts", "check-coverage.sh"),
		"pkg/example",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		environmentWithValues(
			os.Environ(),
			"PATH",
			filepath.Join(repository, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		),
		"GOLIB_COVERAGE_DIGEST_FILE",
		digestFile,
	)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("coverage accepted inputs changed during execution:\n%s", output)
	}
	identity := filepath.Join(
		repository,
		".artifacts",
		"pkg/example",
		"coverage-profile.json",
	)
	if _, err := os.Stat(identity); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalidated coverage identity exists: %v", err)
	}
}

func TestMutationCoverageReusesOnlyContentBoundCurrentProfile(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	directory := t.TempDir()
	profile := filepath.Join(directory, "coverage.out")
	identity := filepath.Join(directory, "coverage-profile.json")
	destination := filepath.Join(directory, "mutation.coverage")
	contents := []byte("mode: atomic\nexample.go:1.1,1.2 1 1\n")
	writeTestFile(t, profile, string(contents))
	digest := sha256.Sum256(contents)
	writeTestFile(t, identity, fmt.Sprintf(`{
  "schema_version": 1,
  "input_digest": "current-input",
  "test_tags": "integration",
  "profile_sha256": "%x",
  "elapsed": "7s"
}
`, digest))

	command := exec.Command(
		filepath.Join(root, "scripts", "internal", "reuse-mutation-coverage.sh"),
		profile,
		identity,
		destination,
		"current-input",
		"integration",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reuse current coverage profile: %v\n%s", err, output)
	}

	reused, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reused, contents) {
		t.Fatalf("reused coverage profile = %q, want %q", reused, contents)
	}
	assertRejected := func(name string, inputDigest string, tags string) {
		t.Helper()
		rejectedDestination := filepath.Join(directory, name+".coverage")
		command := exec.Command(
			filepath.Join(root, "scripts", "internal", "reuse-mutation-coverage.sh"),
			profile,
			identity,
			rejectedDestination,
			inputDigest,
			tags,
		)
		if output, err := command.CombinedOutput(); err == nil {
			t.Fatalf("reused %s coverage profile: %s", name, output)
		}
		if _, err := os.Stat(rejectedDestination); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s coverage destination exists: %v", name, err)
		}
	}
	assertRejected("stale-input", "stale-input", "integration")
	assertRejected("wrong-tags", "current-input", "unit")

	writeTestFile(t, profile, string(append(contents, []byte("tampered\n")...)))
	tamperedDestination := filepath.Join(directory, "tampered.coverage")
	command = exec.Command(
		filepath.Join(root, "scripts", "internal", "reuse-mutation-coverage.sh"),
		profile,
		identity,
		tamperedDestination,
		"current-input",
		"integration",
	)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("reused tampered coverage profile: %s", output)
	}
	if _, err := os.Stat(tamperedDestination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tampered coverage destination exists: %v", err)
	}
}

func TestMutationCommandHonorsBoundedWorkerCount(t *testing.T) {
	t.Parallel()

	root := testRepositoryRoot(t)
	command := exec.Command(
		"bash",
		"-c",
		`. "$1"
. "$2"
build_mutation_arguments . report.json integration 0
configure_mutation_workers 1
printf '%s\n' "${mutation_arguments[@]}"`,
		"mutation-worker-test",
		filepath.Join(root, "scripts", "internal", "mutation-command.sh"),
		filepath.Join(root, "scripts", "internal", "configure-mutation-workers.sh"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build mutation command: %v\n%s", err, output)
	}
	arguments := strings.Split(strings.TrimSpace(string(output)), "\n")
	workers := slices.Index(arguments, "--workers")
	if workers < 0 || workers+1 >= len(arguments) || arguments[workers+1] != "1" {
		t.Fatalf("mutation worker arguments = %q, want --workers 1", arguments)
	}
}

func TestGolibGremlinsExecutesDeclarationMutants(t *testing.T) {
	root := testRepositoryRoot(t)
	module := standaloneModule(t, `package fixture

const Value = 1 << 4
`)
	writeFile(t, filepath.Join(module, "fixture_test.go"), `package fixture

import "testing"

func TestValue(t *testing.T) {
	if Value != 16 {
		t.Fatalf("Value = %d, want 16", Value)
	}
}
`)
	build := exec.Command(filepath.Join(root, "scripts", "build-golib-gremlins.sh"))
	build.Dir = root
	output, err := build.Output()
	if err != nil {
		t.Fatalf("build golib-gremlins: %v", err)
	}
	binary := strings.TrimSpace(string(output))
	report := filepath.Join(t.TempDir(), "mutation.json")
	command := exec.Command(binary,
		"unleash", ".", "--integration", "--coverpkg", ".",
		"--workers", "1", "--test-cpu", "1", "--timeout-coefficient", "10",
		"--threshold-efficacy", "100", "--threshold-mcover", "100",
		"--invert-bitwise", "--output-statuses", "lctvsr", "--output", report,
	)
	command.Dir = module
	command.Env = environmentWith("GOWORK", "off")
	if output, err = command.CombinedOutput(); err != nil {
		t.Fatalf("execute declaration mutant: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	result := struct {
		Files []struct {
			Mutations []struct {
				Status string `json:"status"`
				Line   int    `json:"line"`
			} `json:"mutations"`
		} `json:"files"`
	}{}
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode mutation report: %v", err)
	}
	mutations := 0
	for _, file := range result.Files {
		for _, mutation := range file.Mutations {
			mutations++
			if mutation.Status != "KILLED" {
				t.Fatalf("declaration mutant on line %d has status %s", mutation.Line, mutation.Status)
			}
		}
	}
	if mutations == 0 {
		t.Fatal("golib-gremlins did not discover the declaration mutant")
	}
}

func TestGolibGremlinsReusesExternalCoverageProfile(t *testing.T) {
	root := testRepositoryRoot(t)
	module := standaloneModule(t, `package fixture

func Value(input int) int {
	return input & 1
}
`)
	testFile := filepath.Join(module, "fixture_test.go")
	writeFile(t, testFile, `package fixture

import "testing"

func TestValue(t *testing.T) {
	if Value(3) != 1 {
		t.Fatal("wrong value")
	}
}
`)
	profile := filepath.Join(t.TempDir(), "coverage.out")
	coverage := exec.Command(
		"go",
		"test",
		"-coverpkg=.",
		"-coverprofile="+profile,
		".",
	)
	coverage.Dir = module
	coverage.Env = environmentWith("GOWORK", "off")
	if output, err := coverage.CombinedOutput(); err != nil {
		t.Fatalf("generate external coverage: %v\n%s", err, output)
	}

	// A tagged failing test proves the patched binary does not recollect
	// coverage while the required untagged unit baseline remains healthy.
	writeFile(t, filepath.Join(module, "integration_test.go"), `//go:build integration

package fixture

import "testing"

func TestIntegrationCoverage(t *testing.T) {
	t.Fatal("coverage was recollected")
}
`)
	build := exec.Command(filepath.Join(root, "scripts", "build-golib-gremlins.sh"))
	build.Dir = root
	output, err := build.Output()
	if err != nil {
		t.Fatalf("build golib-gremlins: %v", err)
	}
	binary := strings.TrimSpace(string(output))
	report := filepath.Join(t.TempDir(), "mutation.json")
	command := exec.Command(
		binary,
		"unleash",
		".",
		"--dry-run",
		"--integration",
		"--coverpkg",
		".",
		"--tags",
		"integration",
		"--invert-bitwise",
		"--output-statuses",
		"r",
		"--output",
		report,
	)
	command.Dir = module
	command.Env = environmentWith("GOWORK", "off")
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_GREMLINS_COVERAGE_PROFILE",
		profile,
	)
	command.Env = environmentWithValues(
		command.Env,
		"GOLIB_GREMLINS_COVERAGE_ELAPSED",
		"1s",
	)
	if output, err = command.CombinedOutput(); err != nil {
		t.Fatalf("reuse external coverage: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "done in 1s") {
		t.Fatalf("external coverage duration was not retained:\n%s", output)
	}
	contents, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Files []struct {
			Mutations []json.RawMessage `json:"mutations"`
		} `json:"files"`
	}
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode mutation report: %v", err)
	}
	mutations := 0
	for _, file := range result.Files {
		mutations += len(file.Mutations)
	}
	if mutations == 0 {
		t.Fatal("external coverage profile produced no runnable mutants")
	}
}

func TestZeroMutantInventoryIsExactAndCurrent(t *testing.T) {
	root := testRepositoryRoot(t)
	type zeroEntry struct {
		ModuleDirectory  string `json:"module_directory"`
		PackageDirectory string `json:"package_directory"`
		SourceDigest     string `json:"source_digest"`
		GremlinsVersion  string `json:"gremlins_version"`
		GremlinsIdentity string `json:"gremlins_verifier_sha256"`
		Reason           string `json:"reason"`
	}
	inventory := struct {
		SchemaVersion int         `json:"schema_version"`
		Packages      []zeroEntry `json:"packages"`
	}{}
	decodeJSONFile(t, filepath.Join(root, ".golib", "mutation-zero-inventory.json"), &inventory)
	if inventory.SchemaVersion != 1 || len(inventory.Packages) == 0 {
		t.Fatalf("zero-mutant inventory schema = %d, packages = %d", inventory.SchemaVersion, len(inventory.Packages))
	}
	catalog := struct {
		Packages []struct {
			ModuleDirectory  string `json:"module_directory"`
			PackageDirectory string `json:"directory"`
			Production       bool   `json:"production"`
			CoverageRequired bool   `json:"coverage_required"`
		} `json:"packages"`
	}{}
	decodeJSONFile(t, filepath.Join(root, "packages.json"), &catalog)
	versions, err := os.ReadFile(filepath.Join(root, ".golib", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, entry := range inventory.Packages {
		key := entry.ModuleDirectory + "/" + entry.PackageDirectory
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate zero-mutant inventory entry %s", key)
		}
		seen[key] = struct{}{}
		if entry.Reason == "" || entry.GremlinsIdentity == "" ||
			!strings.Contains(string(versions), "GREMLINS_VERSION="+entry.GremlinsVersion+"\n") {
			t.Fatalf("zero-mutant inventory entry %s has stale tool or empty rationale", key)
		}
		matches := 0
		for _, candidate := range catalog.Packages {
			if candidate.ModuleDirectory == entry.ModuleDirectory &&
				candidate.PackageDirectory == entry.PackageDirectory &&
				candidate.Production && candidate.CoverageRequired {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("zero-mutant inventory entry %s matches %d production packages", key, matches)
		}
		command := exec.Command(filepath.Join(root, "scripts", "package-source-digest.sh"), key)
		command.Dir = root
		digest, err := command.Output()
		if err != nil {
			t.Fatalf("digest zero-mutant package %s: %v", key, err)
		}
		if strings.TrimSpace(string(digest)) != entry.SourceDigest {
			t.Fatalf("zero-mutant inventory digest for %s is stale", key)
		}
	}
}

func standaloneFuzzModule(t *testing.T) string {
	t.Helper()
	directory := standaloneModule(t, "package fixture\n")
	writeFile(t, filepath.Join(directory, "fixture_test.go"), `package fixture

import "testing"

func FuzzIdentity(fuzz *testing.F) {
	fuzz.Add("seed")
	fuzz.Fuzz(func(t *testing.T, value string) {
		if got := string([]byte(value)); got != value {
			t.Fatalf("round trip = %q, want %q", got, value)
		}
	})
}
`)
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(directory, "nested", "go.mod"),
		"module example.test/nested\n\ngo 1.26.6\n",
	)
	writeFile(t, filepath.Join(directory, "nested", "nested_test.go"), `package nested

import "testing"

func FuzzNestedModule(f *testing.F) {
	f.Fatal("parent fuzz discovery crossed a module boundary")
}
`)
	return directory
}

func standaloneModule(t *testing.T, source string) string {
	t.Helper()
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module example.test/fixture\n\ngo 1.26.6\n")
	writeFile(t, filepath.Join(directory, "fixture.go"), source)
	return directory
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("locate repository: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func cleanRepositorySnapshot(t *testing.T, source string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "repository")
	headCommand := exec.Command("git", "-C", source, "rev-parse", "HEAD")
	headOutput, err := headCommand.Output()
	if err != nil {
		t.Fatalf("resolve repository head: %v", err)
	}
	head := strings.TrimSpace(string(headOutput))

	clone := exec.Command(
		"git",
		"clone",
		"--shared",
		"--no-checkout",
		"--quiet",
		source,
		destination,
	)
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone clean repository snapshot: %v\n%s", err, output)
	}
	for _, arguments := range [][]string{
		{"config", "--local", "core.fsmonitor", "false"},
		{"checkout", "--detach", "--quiet", head},
	} {
		command := exec.Command("git", append([]string{"-C", destination}, arguments...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("prepare clean repository snapshot: %v\n%s", err, output)
		}
	}
	return destination
}

func restrictedToolPath(t *testing.T) string {
	t.Helper()
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("locate go executable: %v", err)
	}
	return filepath.Dir(goExecutable) + ":/usr/bin:/bin"
}

func directGoEnvironment(t *testing.T) []string {
	t.Helper()
	goExecutable := os.Getenv("GOLIB_REAL_GO")
	if goExecutable == "" {
		var err error
		goExecutable, err = exec.LookPath("go")
		if err != nil {
			t.Fatalf("locate Go executable: %v", err)
		}
	}
	environment := environmentWithValues(
		os.Environ(),
		"PATH",
		filepath.Dir(goExecutable)+":"+os.Getenv("PATH"),
	)
	environment = environmentWithValues(
		environment,
		"GOFLAGS",
		os.Getenv("GOLIB_UPSTREAM_GOFLAGS"),
	)
	return environmentWithValues(environment, "GOWORK", "off")
}

func environmentWithPath(path string) []string {
	return environmentWith("PATH", path)
}

func environmentWith(name, value string) []string {
	return environmentWithValues(os.Environ(), name, value)
}

func environmentWithValues(base []string, name, value string) []string {
	environment := make([]string, 0, len(base))
	prefix := name + "="
	for _, variable := range base {
		if !strings.HasPrefix(variable, prefix) {
			environment = append(environment, variable)
		}
	}
	return append(environment, prefix+value)
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(contents)
}

func waitForFile(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for %s: %v", path, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func decodeJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
