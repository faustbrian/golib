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

func TestCopyStandaloneFoundationTracksDocumentationLockfile(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(source, ".gitignore"),
		[]byte(".artifacts/\npackage-lock.json\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := copyStandaloneFoundationFile(
		source,
		destination,
		".gitignore",
		standaloneRepository{Family: "fixture", Name: "go-fixture"},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(destination, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "!package-lock.json\n") {
		t.Fatalf("standalone ignore rules do not track package-lock.json:\n%s", contents)
	}
}

func TestRewriteStandaloneRepositoryCheckRequiresTrackedDocumentationLockfile(t *testing.T) {
	t.Parallel()

	input := []byte(`#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
repository="github.com/faustbrian/go-fixture"

git diff --check
`)
	got := rewriteStandaloneTooling(
		input,
		".golib/scripts/repository-check.sh",
		"github.com/faustbrian/go-fixture",
	)
	if !strings.Contains(
		string(got),
		`git -C "${root}" ls-files --error-unmatch package-lock.json`,
	) {
		t.Fatalf("standalone repository check permits an untracked lockfile:\n%s", got)
	}
	second := rewriteStandaloneTooling(
		got,
		".golib/scripts/repository-check.sh",
		"github.com/faustbrian/go-fixture",
	)
	if string(second) != string(got) {
		t.Fatalf("standalone repository-check rewrite is not idempotent:\n%s", second)
	}
}

func TestRewriteStandaloneContributingUsesStandaloneContracts(t *testing.T) {
	t.Parallel()

	input := []byte(`New direct dependencies must follow the
[dependency governance policy](docs/dependency-governance.md).
Specification-backed changes must follow the
[specification governance contract](docs/specification-governance.md).

Run during development:

\x60\x60\x60bash
make inventory
make specification-decisions
make check MODULES=pkg/<library>
\x60\x60\x60

Before submitting a repository-wide change:

\x60\x60\x60bash
make ci-changed BASE=origin/main
\x60\x60\x60

Follow [module lifecycle procedures](docs/module-lifecycle.md).

Do not add package-local workflows, permanent replacements, machine-specific
paths, bypass flags, broad mutation exclusions, or aggregate quality metrics.
`)
	got := string(rewriteStandaloneContributing(input))
	for _, forbidden := range []string{
		"docs/dependency-governance.md",
		"docs/specification-governance.md",
		"docs/module-lifecycle.md",
		"make check MODULES=pkg/<library>",
		"make ci-changed",
		"make specification-decisions",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("standalone contributor guide retains %q:\n%s", forbidden, got)
		}
	}
	for _, wanted := range []string{
		"AGENTS.md#dependencies-and-supply-chain",
		"AGENTS.md#design",
		"AGENTS.md#repository-structure",
		"make check",
		"make ci",
		"zero surviving viable mutants",
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("standalone contributor guide does not contain %q:\n%s", wanted, got)
		}
	}
	second := rewriteStandaloneContributing([]byte(got))
	if string(second) != got {
		t.Fatalf("standalone contributor guide rewrite is not idempotent:\n%s", second)
	}
}

func TestRewriteStandaloneSpellingConfigurationAddsStandaloneMarkdownPolicy(t *testing.T) {
	t.Parallel()

	input := []byte(`{
  "version": "0.2",
  "words": ["existing", "golib"],
  "ignoreRegExpList": ["/existing/g"]
}
`)
	got, err := rewriteStandaloneSpellingConfiguration(input)
	if err != nil {
		t.Fatalf("rewriteStandaloneSpellingConfiguration() error = %v", err)
	}
	for _, wanted := range []string{
		`"version": "0.2"`,
		`"existing"`,
		`"golib"`,
		"/```[\\\\s\\\\S]*?```/g",
		"/`[^`\\\\n]+`/g",
		`/https?:\\/\\/[^\\s)]+/g`,
	} {
		if !strings.Contains(string(got), wanted) {
			t.Errorf("standalone spelling configuration does not contain %q:\n%s", wanted, got)
		}
	}
	second, err := rewriteStandaloneSpellingConfiguration(got)
	if err != nil {
		t.Fatalf("second rewriteStandaloneSpellingConfiguration() error = %v", err)
	}
	if string(second) != string(got) {
		t.Fatalf("standalone spelling configuration is not idempotent:\n%s", second)
	}
}

func TestRewriteStandaloneChangelogRecordsDocumentationPolicy(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## [Unreleased]\n\n## [1.0.0]\n")
	got := rewriteStandaloneChangelog(input)
	if !strings.Contains(string(got), "Harden standalone documentation validation") {
		t.Fatalf("standalone changelog does not record documentation policy:\n%s", got)
	}
	if !strings.Contains(string(got), "Reconcile standalone dependency checksums") {
		t.Fatalf("standalone changelog does not record dependency checksums:\n%s", got)
	}
	if !strings.Contains(string(got), "Track the pinned documentation-tool lockfile") {
		t.Fatalf("standalone changelog does not record the tracked lockfile:\n%s", got)
	}
	second := rewriteStandaloneChangelog(got)
	if string(second) != string(got) {
		t.Fatalf("standalone changelog rewrite is not idempotent:\n%s", second)
	}

	unbracketed := rewriteStandaloneChangelog([]byte("# Changelog\n\n## Unreleased\n"))
	if !strings.Contains(string(unbracketed), "Harden standalone documentation validation") {
		t.Fatalf("unbracketed standalone changelog was not updated:\n%s", unbracketed)
	}
}

func TestRewriteStandaloneSecurityUsesRepositoryOwnedPolicies(t *testing.T) {
	t.Parallel()

	input := []byte("Report through `faustbrian/golib`.\n\n" +
		"Until modules reach `v1`, only the latest released minor line receives security\n" +
		"fixes. After `v1`, support windows are documented per module and in\n" +
		"[`COMPATIBILITY.md`](COMPATIBILITY.md).\n\n" +
		"The repository-wide [threat model](docs/security/threat-model.md),\n" +
		"[security matrix](docs/security/security-matrix.md), and\n" +
		"[residual-risk register](docs/security/residual-risks.md) define shared trust\n" +
		"boundaries and open release risks. Package-specific threat models refine those\n" +
		"rules for their owned boundary; they do not replace the repository model.\n")
	got := string(rewriteStandaloneSecurity(input, "go-validation"))
	for _, forbidden := range []string{
		"faustbrian/golib",
		"Until modules reach",
		"docs/security/threat-model.md",
		"docs/security/security-matrix.md",
		"docs/security/residual-risks.md",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("standalone security policy retains %q:\n%s", forbidden, got)
		}
	}
	for _, wanted := range []string{
		"faustbrian/go-validation",
		"latest stable `v1` release line",
		"AGENTS.md#safety-and-concurrency",
		"AGENTS.md#dependencies-and-supply-chain",
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("standalone security policy does not contain %q:\n%s", wanted, got)
		}
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
