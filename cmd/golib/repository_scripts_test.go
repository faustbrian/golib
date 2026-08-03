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

func TestVerificationSnapshotDisablesInheritedFileSystemMonitor(t *testing.T) {
	root := testRepositoryRoot(t)
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	writeTestFile(t, globalConfig, "[core]\n\tfsmonitor = true\n")
	snapshot := filepath.Join(t.TempDir(), "repository")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(root, "scripts", "create-verification-snapshot.sh"),
		root,
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

func TestLocalProxyBuildsSelectedDependencyClosureDeterministically(t *testing.T) {
	root := testRepositoryRoot(t)
	script := filepath.Join(root, "scripts", "build-local-proxy.sh")
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

go 1.26.5

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
	if strings.Contains(
		strings.SplitN(string(invocation), "\n", 2)[1],
		"-modfile=",
	) {
		t.Fatalf("isolated dependency update passed modfile as an argument: %s", invocation)
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
		`path: ${{ matrix.directory == '.' && '.artifacts' || format('.artifacts/{0}', matrix.directory) }}`,
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
		`sudo apt-get install --yes --no-install-recommends zsh=5.9-6ubuntu2`,
		`package-manager-cache: false`,
		`denoland/setup-deno@22d081ff2d3a40755e97629de92e3bcbfa7cf2ed`,
		`deno-version: '2.9.4'`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("CI workflow lacks %q", required)
		}
	}
	if strings.Contains(contract, "actions/setup-node@") &&
		strings.Contains(contract, "node-version: '24.4.1'\n          cache: false") {
		t.Fatal("setup-node receives unsupported cache=false package-manager input")
	}
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
		`mutation-legacy`,
		`GOLIB_MUTATION_DIGEST_RESOLUTION=caller`,
		`GOLIB_MUTATION_DIGEST_RESOLUTION=legacy-stable`,
		`migrated module-wide mutation identity`,
		`identity_lineage`,
		`migrated caller-dependent mutation identity`,
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
	if _, err := os.Stat(filepath.Join(root, "scripts", "gate-input-digest.sh")); err != nil {
		t.Fatalf("mutation evidence fingerprint tool: %v", err)
	}
}

func TestHistoryMigrationScopeUsesContentInsteadOfRepositoryHistory(t *testing.T) {
	root := testRepositoryRoot(t)
	script := filepath.Join(root, "scripts", "history-migration-scope-digest.sh")
	repository := t.TempDir()
	ledger := filepath.Join(
		repository,
		".golib",
		"mutation-history-migrations.json",
	)

	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ledger, `{
  "allowed_changes": [
    ".golib/mutation-history-migrations.json",
    "allowed.txt"
  ]
}`)
	writeFile(t, filepath.Join(repository, "allowed.txt"), "first\n")
	stable := filepath.Join(repository, "stable.txt")
	writeFile(t, stable, "stable\n")
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	add := exec.Command("git", "-C", repository, "add", "--", ".golib", "allowed.txt", "stable.txt")
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("index fixture repository: %v\n%s", err, output)
	}

	digest := func() string {
		t.Helper()
		command := exec.Command(script, repository, ledger)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("calculate history migration scope: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}

	initial := digest()
	writeFile(t, filepath.Join(repository, "allowed.txt"), "second\n")
	if current := digest(); current != initial {
		t.Fatalf("allowed bookkeeping changed scope digest: %s != %s", current, initial)
	}

	if err := os.RemoveAll(filepath.Join(repository, ".git")); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("reinitialize fixture repository: %v\n%s", err, output)
	}
	add = exec.Command("git", "-C", repository, "add", "--", ".golib", "allowed.txt", "stable.txt")
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("reindex fixture repository: %v\n%s", err, output)
	}
	if current := digest(); current != initial {
		t.Fatalf("history rewrite changed scope digest: %s != %s", current, initial)
	}

	writeFile(t, stable, "changed\n")
	if current := digest(); current == initial {
		t.Fatal("unapproved tracked input did not change scope digest")
	}
	writeFile(t, stable, "stable\n")
	writeFile(t, filepath.Join(repository, "untracked.txt"), "unexpected\n")
	if current := digest(); current == initial {
		t.Fatal("unapproved untracked input did not change scope digest")
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
		"scripts/internal",
		"scripts/patches",
	} {
		if err := os.MkdirAll(filepath.Join(repository, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/root\n\ngo 1.26.5\n")
	writeFile(t, filepath.Join(repository, "modules.json"), `{
  "modules": [{
    "directory": "pkg/example",
    "module_path": "example.test/example",
    "owned_dependencies": [],
    "test_tags": [],
    "required_services": [],
    "go_version": "1.26.5",
    "gates": {"mutation": true},
    "packages": [
      {"directory": ".", "coverage_required": true},
      {"directory": "consumer", "coverage_required": true},
      {"directory": "sibling", "coverage_required": true}
    ]
  }]
}`)
	writeFile(t, filepath.Join(repository, "packages.json"), `{"packages":[]}`)
	writeFile(t, filepath.Join(repository, ".golib", "versions.env"), "GREMLINS_VERSION=v0.6.0\n")
	writeFile(t, filepath.Join(repository, ".golib", "mutation-zero-inventory.json"), `{"packages":[]}`)
	for _, path := range []string{
		"scripts/build-golib-gremlins.sh",
		"scripts/check-mutation.sh",
		"scripts/internal/run-mutation.sh",
		"scripts/internal/mutation-command.sh",
		"scripts/internal/mutation-coverage.sh",
		"scripts/package-source-digest.sh",
		"scripts/patches/gremlins-run-all-mutants.patch",
		"scripts/patches/gremlins-shared-coverage.patch",
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
	writeFile(t, filepath.Join(repository, "pkg", "dependency", "go.mod"), "module example.test/dependency\n\ngo 1.26.5\n")
	dependencySum := filepath.Join(repository, "pkg", "dependency", "go.sum")
	writeFile(t, dependencySum, "example.test/archive v0.1.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	dependencySource := filepath.Join(repository, "pkg", "dependency", "dependency.go")
	dependencyTest := filepath.Join(repository, "pkg", "dependency", "dependency_test.go")
	dependencyFixture := filepath.Join(repository, "pkg", "dependency", "testdata", "value.txt")
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 1 }\n")
	writeFile(t, dependencyTest, "package dependency\n\n// Dependency tests are not observers of another module's mutants.\n")
	writeFile(t, dependencyFixture, "one\n")
	writeFile(t, filepath.Join(repository, "pkg", "example", "go.mod"), `module example.test/example

go 1.26.5

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

	initial := digest()
	legacyInitial := digestWithResolution("legacy-stable")
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

go 1.26.5
`)
	workspace := filepath.Join(root, "go.work")
	writeTestFile(t, workspace, "go 1.26.5\n")
	writeTestFile(
		t,
		filepath.Join(root, "pkg", "example", "example.go"),
		"package example\n",
	)
	readme := filepath.Join(root, "pkg", "example", "README.md")
	writeTestFile(t, readme, "# Example\n")
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
	writeTestFile(t, checkModuleScript, "check module\n")
	writeTestFile(t, checkFuzzScript, "check fuzz\n")
	writeTestFile(t, mutationCommandScript, "mutation command\n")
	writeTestFile(t, snapshotOrchestratorScript, "snapshot orchestration\n")
	versionsFile := filepath.Join(root, ".golib", "versions.env")
	mutationInventory := filepath.Join(root, ".golib", "mutation-zero-inventory.json")
	writeTestFile(t, versionsFile, "TOOL_VERSION=v1.0.0\n")
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
	benchmarkBefore := digest("benchmark")
	fuzzBefore := digest("fuzz")
	docsBefore := digest("docs")
	secretsBefore := digest("secrets")
	writeTestFile(t, workspace, "go 1.26.5\n\nuse ./pkg/unrelated\n")
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
	writeTestFile(t, workspace, "go 1.26.5\n")
	writeTestFile(t, mutationInventory, "{\"packages\":[\"unrelated\"]}\n")
	if current := digest("test"); current != testBefore {
		t.Fatalf(
			"mutation inventory changed test digest: %s != %s",
			current,
			testBefore,
		)
	}
	writeTestFile(t, versionsFile, "TOOL_VERSION=v1.0.1\n")
	if current := digest("test"); current == testBefore {
		t.Fatal("tool versions did not change test digest")
	}
	writeTestFile(t, versionsFile, "TOOL_VERSION=v1.0.0\n")
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
	writeTestFile(t, checkModuleScript, "revised check module\n")
	if current := digest("test"); current == testBefore {
		t.Fatal("shared gate tooling did not change test digest")
	}
	writeTestFile(t, checkModuleScript, "check module\n")
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
	writeFile(t, filepath.Join(repository, "scripts", "check-gates.txt"), "test\nmutation\n")
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

	writeEvidence := func(gate, logContents string) {
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
  "result": "passed",
  "exit_code": 0,
  "execution_revision": "proof",
  "input_digest": "current-digest",
  "completed_input_digest": "current-digest",
  "log_sha256": "%x",
  "completed_at": "2026-07-24T00:00:00Z"
}`, gate, logDigest),
		)
	}
	writeEvidence("test", "test passed\n")
	writeEvidence("mutation", "mutation passed\n")

	verify := exec.Command(
		filepath.Join(repository, "scripts", "verify-gate-evidence.sh"),
		"pkg/sample",
		"test",
	)
	verify.Dir = repository
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify current evidence: %v\n%s", err, output)
	}

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
	writeEvidence("test", "test passed\n")

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
		len(report.Goals) != 1 ||
		report.Goals[0].VerificationStatus != "verified" ||
		len(report.GateEvidence) != 2 {
		t.Fatalf("goal audit report = %+v", report)
	}
}

func TestRootDocumentationGateDoesNotDelegateToRootMakefile(t *testing.T) {
	root := testRepositoryRoot(t)
	bin := t.TempDir()
	makeMarker := filepath.Join(t.TempDir(), "make-called")
	makePath := filepath.Join(bin, "make")
	writeFile(t, makePath, "#!/bin/sh\nprintf called >\"$MAKE_MARKER\"\nexit 99\n")
	if err := os.Chmod(makePath, 0o700); err != nil {
		t.Fatal(err)
	}
	goPath := filepath.Join(bin, "go")
	writeFile(t, goPath, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(goPath, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(filepath.Join(root, "scripts", "check-module.sh"), ".", "docs")
	command.Dir = root
	command.Env = environmentWith("MAKE_MARKER", makeMarker)
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
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("root documentation gate: %v\n%s", err, output)
	}
	if _, err := os.Stat(makeMarker); !os.IsNotExist(err) {
		t.Fatalf("root documentation gate delegated to root Makefile")
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
		`make GOWORK="${root}/go.work" "${target}"`,
		`make "${target}"`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("module gate fail-closed dispatch lacks %q", required)
		}
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
	writeFile(t, filepath.Join(repository, ".gitignore"), "ignored.txt\n.artifacts/\n")
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
		"tracked.txt":   "working tree\n",
		"untracked.txt": "untracked\n",
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		filepath.Join(repository, "scripts", "run-modules.sh"),
		"check",
		"--modules", "pkg/example",
	)
	command.Dir = repository
	command.Env = environmentWithValues(
		environmentWithValues(os.Environ(), "PATH", repository+string(os.PathListSeparator)+os.Getenv("PATH")),
		"TEST_STATE", state,
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

	waitForFile(t, ctx, filepath.Join(state, "ready"))
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate module runner: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("terminated module runner exited successfully")
	} else if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 143 {
		t.Fatalf("terminated module runner exit = %v, want 143\n%s", err, output.String())
	}
	waitForFile(t, ctx, filepath.Join(state, "snapshot-state"))
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
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.5\n")
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
	writeFile(t, filepath.Join(repository, "go.mod"), "module example.test/evidence\n\ngo 1.26.5\n")
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
		if entry.Reason == "" || !strings.Contains(string(versions), "GREMLINS_VERSION="+entry.GremlinsVersion+"\n") {
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
		"module example.test/nested\n\ngo 1.26.5\n",
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
	writeFile(t, filepath.Join(directory, "go.mod"), "module example.test/fixture\n\ngo 1.26.5\n")
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
