//go:build !windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProxyBuildsSelectedDependencyClosureDeterministically(t *testing.T) {
	root := testRepositoryRoot(t)
	script := filepath.Join(root, "scripts", "build-local-proxy.sh")
	first := t.TempDir()
	second := t.TempDir()

	for _, output := range []string{first, second} {
		command := exec.Command(
			script,
			output,
			"v0.1.0",
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
		relative := filepath.FromSlash(modulePath + "/@v/v0.1.0.zip")
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
	unselected := filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/jwt/@v/v0.1.0.mod",
		),
	)
	if _, err := os.Stat(unselected); !os.IsNotExist(err) {
		t.Fatalf("local proxy unexpectedly included unselected module: %v", err)
	}

	parentArchive := filepath.Join(
		first,
		filepath.FromSlash(
			"github.com/faustbrian/golib/pkg/authentication/@v/v0.1.0.zip",
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

func TestIsolatedGoUsesTemporarySumsForOwnedModules(t *testing.T) {
	root := testRepositoryRoot(t)
	module := t.TempDir()
	writeTestFile(t, filepath.Join(module, "go.mod"), `module example.test/consumer

go 1.26.5

require github.com/faustbrian/golib/pkg/dependency v0.1.0
`)
	sourceSum := filepath.Join(module, "go.sum")
	const staleSum = "github.com/faustbrian/golib/pkg/dependency v0.1.0 h1:stale=\n"
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
		'github.com/faustbrian/golib/pkg/dependency v0.1.0 h1:current=' \
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
	if strings.Contains(string(invocation), "-modfile=") ||
		strings.Contains(string(invocation), "-mod=readonly") {
		t.Fatalf("documentation inherited unsupported module flags: %s", invocation)
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
	for _, required := range []string{
		`build-local-proxy.sh`,
		`isolated-go.sh`,
		`GOLIB_ISOLATED_MODFILES_DIRECTORY`,
		`run_go_tool`,
		`exec-tool`,
		`file://${upstream_modcache}/cache/download`,
		`GOMODCACHE="${GOLIB_LOCAL_MODCACHE}"`,
		`golib-modcache.`,
		`chmod -R u+w "${GOLIB_LOCAL_MODCACHE}"`,
		`awk '$1 !~ /^github\.com\/faustbrian\/golib\// { print }'`,
		`test -s "${root}/LICENSE"`,
		`--ignore "github.com/faustbrian/golib"`,
		`--config "${root}/.gitleaks.toml"`,
		`check-api-baseline.sh`,
		`update-api-baseline.sh`,
		`-mod=readonly"`,
		`GOWORK=off "$@"`,
	} {
		if !strings.Contains(moduleContract, required) {
			t.Fatalf("isolated module contract lacks %q", required)
		}
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
	contract := string(mutationRunner) + string(mutationCommand)
	for _, required := range []string{
		`build-golib-gremlins.sh`,
		`mutation-coverage.sh`,
		`GOLIB_GREMLINS_COVERAGE_PROFILE`,
		`GOLIB_GREMLINS_COVERAGE_ELAPSED`,
		`GOFLAGS="-modfile=${modfile} -mod=mod"`,
		`go mod edit -modfile="${modfile}"`,
		`.module_path, .directory`,
		`.coverage_required == true`,
		`--exclude-files '^.+/'`,
		`--threshold-efficacy 100`,
		`--threshold-mcover 100`,
		`GOCACHE="${active_build_cache}"`,
		`trap cleanup EXIT INT TERM`,
		`find "${active_build_cache}" -depth -delete`,
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
		`historical_package_digest`,
		`historically identical mutation evidence`,
		`mutation-history-migrations.json`,
		`checkpoint_report_digest`,
		`current_gate_input_digest`,
		`.replacement_gate_input_digest //`,
		`GOLIB_MUTATION_DIGEST_RESOLUTION=caller`,
		`migrated caller-dependent mutation identity`,
		`migrated reset-safe mutation evidence`,
		`write_aggregate`,
		`mv "${checkpoint_tmp}" "${checkpoint}"`,
		`.complete == true`,
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("mutation evidence contract lacks %q", required)
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
		"scripts/package-source-digest.sh",
		"scripts/patches/gremlins-run-all-mutants.patch",
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
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 1 }\n")
	writeFile(t, dependencyTest, "package dependency\n\n// Dependency tests are not observers of another module's mutants.\n")
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
	consumerTest := filepath.Join(repository, "pkg", "example", "consumer", "consumer_test.go")
	writeFile(t, filepath.Join(repository, "pkg", "example", "consumer", "consumer.go"), `package consumer

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

	digest := func() string {
		t.Helper()
		command := exec.Command(
			filepath.Join(repository, "scripts", "gate-input-digest.sh"),
			"mutation",
			"pkg/example",
			".",
		)
		command.Dir = repository
		command.Env = directGoEnvironment(t)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("calculate mutation digest: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}

	initial := digest()
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
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 2 }\n")
	if current := digest(); current == initial {
		t.Fatal("dependency production source did not change mutation digest")
	}
	writeFile(t, dependencySource, "package dependency\n\nfunc Value() int { return 1 }\n")
	writeFile(t, sibling, "package sibling\n\nfunc Value() int { return 2 }\n")
	if current := digest(); current == initial {
		t.Fatal("integration-tested package did not change mutation digest")
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
	if current := digest(); current == initial {
		t.Fatal("reverse-dependent test did not change mutation digest")
	}
	writeFile(t, consumerTest, `package consumer

import "testing"

func TestValue(t *testing.T) {
	if Value() != 1 {
		t.Fatal("wrong value")
	}
}
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
	command.Env = environmentWithValues(command.Env, "PATH", bin+":"+os.Getenv("PATH"))
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
	if strings.Contains(string(runner), "mapfile") ||
		strings.Contains(string(runner), "readarray") {
		t.Error("module runner requires Bash features unavailable on macOS")
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

	// A failing test proves the patched binary does not recollect coverage.
	writeFile(t, testFile, `package fixture

import "testing"

func TestValue(t *testing.T) {
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
