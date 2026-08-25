package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteStandaloneContentsRewritesOwnedPathsAndVersions(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"github.com/faustbrian/golib/pkg/outbox": "github.com/faustbrian/go-transactional-outbox",
		"github.com/faustbrian/golib/pkg/queue":  "github.com/faustbrian/go-queue",
	}
	versions := map[string]string{
		"github.com/faustbrian/go-transactional-outbox": "v1.0.0",
		"github.com/faustbrian/go-queue":                "v1.0.1",
	}
	input := `module github.com/faustbrian/golib/pkg/outbox/adapters/queue

require (
	github.com/faustbrian/golib/pkg/outbox v0.0.0
	github.com/faustbrian/golib/pkg/queue v0.0.0
)
`

	got := rewriteStandaloneContents([]byte(input), paths, versions, true)
	want := `module github.com/faustbrian/go-transactional-outbox/adapters/queue

require (
	github.com/faustbrian/go-transactional-outbox v1.0.0
	github.com/faustbrian/go-queue v1.0.1
)
`
	if string(got) != want {
		t.Fatalf("rewriteStandaloneContents() =\n%s\nwant:\n%s", got, want)
	}
}

func TestRewriteStandaloneContentsUsesLongestModulePathFirst(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"github.com/faustbrian/golib/pkg/queue":              "github.com/faustbrian/go-queue",
		"github.com/faustbrian/golib/pkg/queue/queueservice": "github.com/faustbrian/go-queue/queueservice",
	}

	got := rewriteStandaloneContents(
		[]byte("github.com/faustbrian/golib/pkg/queue/queueservice/worker"),
		paths,
		nil,
		false,
	)
	if string(got) != "github.com/faustbrian/go-queue/queueservice/worker" {
		t.Fatalf("rewriteStandaloneContents() = %q", got)
	}
}

func TestStandaloneSupersededModulePathsRetiresPostgresqlIdentity(t *testing.T) {
	t.Parallel()

	paths := standaloneSupersededModulePaths()
	if got := paths["github.com/faustbrian/go-postgresql"]; got != "github.com/faustbrian/go-postgres" {
		t.Fatalf("postgres replacement = %q", got)
	}
}

func TestRewriteStandaloneContentsAdvancesSupersededCollisionVersion(t *testing.T) {
	t.Parallel()

	got := rewriteStandaloneContents(
		[]byte("require github.com/faustbrian/go-postgresql v1.0.0\n"),
		standaloneSupersededModulePaths(),
		map[string]string{"github.com/faustbrian/go-postgres": "v1.0.1"},
		true,
	)
	want := "require github.com/faustbrian/go-postgres v1.0.1\n"
	if string(got) != want {
		t.Fatalf("rewriteStandaloneContents() = %q, want %q", got, want)
	}
}

func TestRewriteStandaloneRepositoryPathsRequiresFamilyBoundary(t *testing.T) {
	t.Parallel()

	input := []byte("MODULES=pkg/log\npkg/log/docs pkg/logging\n")
	got := rewriteStandaloneRepositoryPaths(input, "log", "go-log")
	if string(got) != "MODULES=.\ndocs pkg/logging\n" {
		t.Fatalf("rewriteStandaloneRepositoryPaths() = %q", got)
	}
}

func TestRewriteStandaloneRepositoryPathsReplacesReferenceCheckout(t *testing.T) {
	t.Parallel()

	input := []byte("Reference `/Users/brian/Developer/cline/json-schema`.\n")
	got := rewriteStandaloneRepositoryPaths(input, "json-schema", "go-json-schema")
	want := "Reference `https://github.com/faustbrian/json-schema`.\n"
	if string(got) != want {
		t.Fatalf("rewriteStandaloneRepositoryPaths() = %q, want %q", got, want)
	}
}

func TestStandaloneMakefileUsesInstalledRepositoryTooling(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"./.golib/scripts/with-disposable-go-cache.sh",
		"./.golib/scripts/run-modules.sh",
		"./.golib/scripts/repository-check.sh",
	} {
		if !strings.Contains(standaloneMakefile, command) {
			t.Fatalf("standalone Makefile does not invoke %s", command)
		}
	}
	if strings.Contains(standaloneMakefile, "\n\t./scripts/") {
		t.Fatal("standalone Makefile invokes the package-owned scripts directory")
	}
}

func TestRewriteStandalonePackageMakefileUsesInstalledRepositoryTooling(t *testing.T) {
	t.Parallel()

	input := []byte("./scripts/with-gocache.sh ../../scripts/check-api-baseline.sh .\n")
	want := "./scripts/with-gocache.sh ./.golib/scripts/check-api-baseline.sh .\n"
	if got := string(rewriteStandalonePackageMakefile(input)); got != want {
		t.Fatalf("rewriteStandalonePackageMakefile() = %q, want %q", got, want)
	}
}

func TestRewriteStandaloneSharedToolingReferencesRedirectsSelfWrapper(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	if err := os.MkdirAll(filepath.Join(destination, "scripts"), 0o755); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "scripts", "check-mutation.sh"),
		[]byte("wrapper"),
		0o755,
	); err != nil {
		t.Fatalf("write package wrapper: %v", err)
	}
	input := []byte("exec \"${root}/scripts/check-mutation.sh\" .\n")
	want := "exec \"${root}/.golib/scripts/check-mutation.sh\" .\n"
	got := rewriteStandaloneSharedToolingReferences(
		input,
		"scripts/check-mutation.sh",
		destination,
		[]string{"scripts/check-mutation.sh"},
	)
	if string(got) != want {
		t.Fatalf("rewriteStandaloneSharedToolingReferences() = %q, want %q", got, want)
	}
}

func TestRewriteStandaloneSharedToolingReferencesPreservesPackageTool(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	if err := os.MkdirAll(filepath.Join(destination, "scripts"), 0o755); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "scripts", "release.sh"),
		[]byte("package release"),
		0o755,
	); err != nil {
		t.Fatalf("write package tool: %v", err)
	}
	input := []byte("\"$root/scripts/release.sh\" v1.0.0\n")
	got := rewriteStandaloneSharedToolingReferences(
		input,
		"scripts/verify-release.sh",
		destination,
		[]string{"scripts/release.sh"},
	)
	if string(got) != string(input) {
		t.Fatalf("rewriteStandaloneSharedToolingReferences() = %q, want %q", got, input)
	}
}

func TestRemoveLegacyRootToolingDeletesMigrationOnlyInstalledScripts(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	destination := t.TempDir()
	for _, filename := range []string{
		filepath.Join(source, "scripts", "tidy-standalone-modules.sh"),
		filepath.Join(destination, ".golib", "scripts", "tidy-standalone-modules.sh"),
	} {
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatalf("create script directory: %v", err)
		}
		if err := os.WriteFile(filename, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}
	}

	if err := removeLegacyRootTooling(source, destination); err != nil {
		t.Fatalf("removeLegacyRootTooling() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(
		destination,
		".golib",
		"scripts",
		"tidy-standalone-modules.sh",
	)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration-only installed script remains: %v", err)
	}
}

func TestCopyStandaloneScriptsRemovesMigrationOnlyScripts(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	destination := t.TempDir()
	for _, relative := range standaloneMigrationOnlyScripts {
		for _, root := range []string{source, filepath.Join(destination, ".golib")} {
			filename := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
				t.Fatalf("create script directory: %v", err)
			}
			if err := os.WriteFile(filename, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatalf("write script: %v", err)
			}
		}
	}
	if err := copyStandaloneScripts(
		source,
		destination,
		standaloneRepository{Name: "go-example"},
		map[string]string{},
	); err != nil {
		t.Fatalf("copyStandaloneScripts() error = %v", err)
	}
	for _, relative := range standaloneMigrationOnlyScripts {
		if _, err := os.Stat(filepath.Join(destination, ".golib", relative)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("migration-only installed script remains: %s: %v", relative, err)
		}
	}
}

func TestStandaloneWorkflowRetainsStrictCIContracts(t *testing.T) {
	t.Parallel()

	for _, required := range []string{
		"name: Required",
		"name: Quality / ${{ matrix.directory }}",
		"GOLIB_BOOTSTRAP_PROXY_URL",
		"GOLIB_BOOTSTRAP_PROXY_SHA256",
		"restore-ci-mutation-evidence.sh",
		"stage-ci-evidence.sh",
		"actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f",
		"RIPGREP_SHA256",
		"denoland/setup-deno@22d081ff2d3a40755e97629de92e3bcbfa7cf2ed",
		"ZSH_DEB_SHA256",
		"codeql-build.sh",
		"security-events: write",
	} {
		if !strings.Contains(standaloneCIWorkflow, required) {
			t.Fatalf("standalone workflow does not contain %q", required)
		}
	}
	if strings.Contains(standaloneCIWorkflow, "apt-get install") {
		t.Fatal("standalone workflow installs an unpinned package-manager tool")
	}
}

func TestStandaloneRepositoryCheckUsesRunnerProvidedSearchTooling(t *testing.T) {
	t.Parallel()

	if strings.Contains(standaloneRepositoryCheck, "if rg ") {
		t.Fatal("standalone repository contract requires uninstalled ripgrep")
	}
	if !strings.Contains(standaloneRepositoryCheck, "grep -REn") {
		t.Fatal("standalone repository contract does not use standard grep")
	}
}

func TestAddStandaloneReadmeBadgesReplacesLegacyWorkflowLinks(t *testing.T) {
	t.Parallel()

	repository := standaloneRepository{
		Name:       "go-router",
		ModulePath: "github.com/faustbrian/go-router",
	}
	input := []byte("# router\n\n" +
		"[![CI](https://github.com/faustbrian/golib/actions/workflows/ci.yml/badge.svg)]" +
		"(https://github.com/faustbrian/golib/actions/workflows/ci.yml)\n\n" +
		"Explicit routing.\n")
	got := addStandaloneReadmeBadges(input, repository)
	text := string(got)
	for _, required := range []string{
		"github.com/faustbrian/go-router/actions/workflows/ci.yml",
		"pkg.go.dev/github.com/faustbrian/go-router",
		"coverage-100%25_required",
		"mutation-100%25_required",
		"Explicit routing.",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("standalone README does not contain %q:\n%s", required, text)
		}
	}
	if strings.Contains(text, "github.com/faustbrian/golib/actions") {
		t.Fatalf("standalone README retains the legacy workflow URL:\n%s", text)
	}
	if second := addStandaloneReadmeBadges(got, repository); string(second) != text {
		t.Fatalf("standalone README badges are not idempotent:\n%s", second)
	}
}

func TestAddStandaloneChangelogEntryUsesExistingChangedSection(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## [Unreleased]\n\n### Changed\n\n- Existing change.\n")
	got, err := addStandaloneChangelogEntry(
		input,
		"github.com/faustbrian/go-router",
	)
	if err != nil {
		t.Fatalf("addStandaloneChangelogEntry() error = %v", err)
	}
	want := "### Changed\n\n" + standaloneChangelogEntry(
		"github.com/faustbrian/go-router",
	) + "\n- Existing change."
	if !strings.Contains(string(got), want) {
		t.Fatalf("standalone changelog entry was not inserted in Changed:\n%s", got)
	}
	second, err := addStandaloneChangelogEntry(got, "github.com/faustbrian/go-router")
	if err != nil {
		t.Fatalf("second addStandaloneChangelogEntry() error = %v", err)
	}
	if string(second) != string(got) {
		t.Fatalf("standalone changelog insertion is not idempotent:\n%s", second)
	}
}

func TestAddStandaloneChangelogEntryCreatesChangedSection(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## Unreleased\n\n### Added\n\n- Initial API.\n")
	got, err := addStandaloneChangelogEntry(
		input,
		"github.com/faustbrian/go-router/adapters/otel",
	)
	if err != nil {
		t.Fatalf("addStandaloneChangelogEntry() error = %v", err)
	}
	want := "## Unreleased\n\n### Changed\n\n" + standaloneChangelogEntry(
		"github.com/faustbrian/go-router/adapters/otel",
	) + "\n\n### Added"
	if !strings.Contains(string(got), want) {
		t.Fatalf("standalone Changed section was not created:\n%s", got)
	}
}

func TestAddStandaloneChangelogEntryPreservesSpaceBeforeNextHeading(t *testing.T) {
	t.Parallel()

	input := []byte("# Changelog\n\n## Unreleased\n\n### Changed\n\n### Fixed\n\n- Existing fix.\n")
	got, err := addStandaloneChangelogEntry(
		input,
		"github.com/faustbrian/go-router",
	)
	if err != nil {
		t.Fatalf("addStandaloneChangelogEntry() error = %v", err)
	}
	want := standaloneChangelogEntry("github.com/faustbrian/go-router") +
		"\n\n### Fixed"
	if !strings.Contains(string(got), want) {
		t.Fatalf("standalone changelog heading spacing was not preserved:\n%s", got)
	}
}

func TestAddStandaloneChangelogEntryRejectsMissingUnreleasedSection(t *testing.T) {
	t.Parallel()

	_, err := addStandaloneChangelogEntry(
		[]byte("# Changelog\n"),
		"github.com/faustbrian/go-router",
	)
	if err == nil {
		t.Fatal("addStandaloneChangelogEntry() error = nil")
	}
}

func TestRemoveStandaloneOwnedChecksumsKeepsExternalModules(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"github.com/faustbrian/golib/pkg/queue": "github.com/faustbrian/go-queue",
	}
	input := []byte("github.com/faustbrian/golib/pkg/queue v0.0.0 h1:old\n" +
		"github.com/faustbrian/go-queue v1.0.0 h1:rewritten-old\n" +
		"github.com/stretchr/testify v1.11.1 h1:external\n")
	got := removeStandaloneOwnedChecksums(input, paths)
	want := "github.com/stretchr/testify v1.11.1 h1:external\n"
	if string(got) != want {
		t.Fatalf("removeStandaloneOwnedChecksums() = %q, want %q", got, want)
	}
}

func TestCleanStandaloneChecksumsFromManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repositoryRoot := filepath.Join(root, "go-queue")
	if err := os.MkdirAll(filepath.Join(repositoryRoot, "redis"), 0o755); err != nil {
		t.Fatalf("create module directories: %v", err)
	}
	rootSum := "github.com/faustbrian/golib/pkg/queue v0.0.0 h1:old\n" +
		"github.com/stretchr/testify v1.11.1 h1:external\n"
	if err := os.WriteFile(filepath.Join(repositoryRoot, "go.sum"), []byte(rootSum), 0o644); err != nil {
		t.Fatalf("write root go.sum: %v", err)
	}
	nestedSum := "github.com/faustbrian/go-queue v1.0.0 h1:stale\n"
	if err := os.WriteFile(filepath.Join(repositoryRoot, "redis", "go.sum"), []byte(nestedSum), 0o644); err != nil {
		t.Fatalf("write nested go.sum: %v", err)
	}
	manifest := standaloneManifest{
		Repositories: []standaloneRepository{{
			Name:                 "go-queue",
			DestinationDirectory: "go-queue",
		}},
		Modules: []standaloneModulePlan{
			{
				Directory:    ".",
				PreviousPath: "github.com/faustbrian/golib/pkg/queue",
				Path:         "github.com/faustbrian/go-queue",
				Repository:   "go-queue",
			},
			{
				Directory:    "redis",
				PreviousPath: "github.com/faustbrian/golib/pkg/queue/redis",
				Path:         "github.com/faustbrian/go-queue/redis",
				Repository:   "go-queue",
			},
		},
	}

	if err := cleanStandaloneChecksumsFromManifest(root, manifest); err != nil {
		t.Fatalf("cleanStandaloneChecksumsFromManifest() error = %v", err)
	}
	gotRoot, err := os.ReadFile(filepath.Join(repositoryRoot, "go.sum"))
	if err != nil {
		t.Fatalf("read cleaned root go.sum: %v", err)
	}
	if string(gotRoot) != "github.com/stretchr/testify v1.11.1 h1:external\n" {
		t.Fatalf("cleaned root go.sum = %q", gotRoot)
	}
	gotNested, err := os.ReadFile(filepath.Join(repositoryRoot, "redis", "go.sum"))
	if err != nil {
		t.Fatalf("read cleaned nested go.sum: %v", err)
	}
	if len(gotNested) != 0 {
		t.Fatalf("cleaned nested go.sum = %q", gotNested)
	}
}

func TestCleanStandaloneChecksumsRejectsUnexpectedDestinationOrigin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	destinationRoot := t.TempDir()
	repositoryRoot := filepath.Join(destinationRoot, "go-queue")
	if err := os.MkdirAll(filepath.Join(root, "migration", "standalone"), 0o755); err != nil {
		t.Fatalf("create manifest directory: %v", err)
	}
	manifest := standaloneManifest{Repositories: []standaloneRepository{{
		Name:                 "go-queue",
		DestinationDirectory: "go-queue",
	}}}
	contents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "migration", "standalone", "repositories.json"),
		contents,
		0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(repositoryRoot, 0o755); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	for _, arguments := range [][]string{
		{"init", "-q", repositoryRoot},
		{"-C", repositoryRoot, "remote", "add", "origin", "git@example.test/wrong.git"},
	} {
		if output, err := exec.Command("git", arguments...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}

	err = cleanStandaloneChecksums(root, []string{"--destination-root", destinationRoot})
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("cleanStandaloneChecksums() error = %v", err)
	}
}

func TestStandaloneCatalogRebasesRepositoryPaths(t *testing.T) {
	t.Parallel()

	current := catalog{
		SchemaVersion: 1,
		Repository:    canonicalRoot,
		GoVersion:     requiredGo,
		Modules: []module{{
			Directory: "pkg/outbox/adapters/queue",
			Path:      "github.com/faustbrian/golib/pkg/outbox/adapters/queue",
			Packages: []packageInfo{{
				ModuleDirectory: "pkg/outbox/adapters/queue",
				Directory:       ".",
				Import:          "github.com/faustbrian/golib/pkg/outbox/adapters/queue",
			}},
			OwnedDependencies: []string{
				"github.com/faustbrian/golib/pkg/outbox",
				"github.com/faustbrian/golib/pkg/queue",
			},
			Goals: []string{"pkg/outbox/.ai/GOAL.md"},
			GoalEvidence: []goalEvidence{{
				File:                   "pkg/outbox/.ai/GOAL.md",
				ImplementationEvidence: []string{"pkg/outbox/README.md"},
			}},
		}},
	}
	paths := map[string]string{
		"github.com/faustbrian/golib/pkg/outbox": "github.com/faustbrian/go-transactional-outbox",
		"github.com/faustbrian/golib/pkg/queue":  "github.com/faustbrian/go-queue",
	}

	got, err := standaloneCatalog(
		current,
		"outbox",
		"go-transactional-outbox",
		paths,
		map[string]string{
			"github.com/faustbrian/go-transactional-outbox/adapters/queue": "v1.0.0",
		},
	)
	if err != nil {
		t.Fatalf("standaloneCatalog() error = %v", err)
	}
	if got.Repository != "github.com/faustbrian/go-transactional-outbox" {
		t.Fatalf("repository = %q", got.Repository)
	}
	if len(got.Modules) != 1 {
		t.Fatalf("module count = %d", len(got.Modules))
	}
	item := got.Modules[0]
	if item.Directory != "adapters/queue" {
		t.Fatalf("directory = %q", item.Directory)
	}
	if item.Path != "github.com/faustbrian/go-transactional-outbox/adapters/queue" {
		t.Fatalf("module path = %q", item.Path)
	}
	if strings.Join(item.OwnedDependencies, ",") !=
		"github.com/faustbrian/go-transactional-outbox,github.com/faustbrian/go-queue" {
		t.Fatalf("owned dependencies = %v", item.OwnedDependencies)
	}
	if item.Goals[0] != ".ai/GOAL.md" ||
		item.GoalEvidence[0].ImplementationEvidence[0] != "README.md" {
		t.Fatalf("repository paths were not rebased: %+v", item)
	}
}

func TestStandaloneCatalogRejectsModulesOutsideFamily(t *testing.T) {
	t.Parallel()

	_, err := standaloneCatalog(catalog{Modules: []module{{
		Directory: "pkg/queue",
	}}}, "outbox", "go-transactional-outbox", nil, nil)
	if err == nil {
		t.Fatal("standaloneCatalog() error = nil")
	}
}

func TestStandaloneCatalogRejectsMissingReleaseVersion(t *testing.T) {
	t.Parallel()

	_, err := standaloneCatalog(catalog{Modules: []module{{
		Directory:  "pkg/outbox",
		Path:       "github.com/faustbrian/golib/pkg/outbox",
		Releasable: true,
	}}}, "outbox", "go-transactional-outbox", map[string]string{
		"github.com/faustbrian/golib/pkg/outbox": "github.com/faustbrian/go-transactional-outbox",
	}, nil)
	if err == nil {
		t.Fatal("standaloneCatalog() error = nil")
	}
}

func TestRewriteStandaloneToolingScopesLicensesAndProxyVersions(t *testing.T) {
	t.Parallel()

	input := []byte(`"${GOLIB_LOCAL_PROXY}" v0.0.0
if [[ "${module_path}" != "github.com/faustbrian/golib" &&
                "${module_path}" != github.com/faustbrian/golib/* ]]; then
--ignore "github.com/faustbrian/golib"
`)
	got := string(rewriteStandaloneTooling(
		input,
		"scripts/check-module.sh",
		"github.com/faustbrian/go-queue",
	))
	for _, wanted := range []string{
		`"${GOLIB_LOCAL_PROXY}" v1.0.0`,
		`"${module_path}" != "github.com/faustbrian/go-queue"`,
		`--ignore "github.com/faustbrian/go-queue"`,
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("rewritten tooling does not contain %q:\n%s", wanted, got)
		}
	}
	if strings.Contains(got, "github.com/faustbrian/golib") {
		t.Fatalf("rewritten tooling retains monorepo path:\n%s", got)
	}
}

func TestRewriteStandaloneToolingUsesPostgresCollisionSuccessorVersion(t *testing.T) {
	t.Parallel()

	got := string(rewriteStandaloneTooling(
		[]byte(`"${GOLIB_LOCAL_PROXY}" v0.0.0`),
		"scripts/check-module.sh",
		"github.com/faustbrian/go-postgres",
	))
	if got != `"${GOLIB_LOCAL_PROXY}" v1.0.1` {
		t.Fatalf("rewritten tooling = %q", got)
	}
}

func TestRewriteStandaloneToolingRelocatesVerifierInputsWithoutChangingLabels(t *testing.T) {
	t.Parallel()

	input := []byte(`[[ -f "${root}/${input}" ]]
printf 'file\t%s\t%s\n' "${input}" "$(
    shasum -a 256 "${root}/${input}" | awk '{print $1}'
)"
`)
	got := string(rewriteStandaloneTooling(
		input,
		".golib/scripts/mutation-verifier-identity.sh",
		"github.com/faustbrian/go-queue",
	))
	for _, wanted := range []string{
		`[[ -f "${root}/.golib/${input}" ]]`,
		`shasum -a 256 "${root}/.golib/${input}"`,
		`printf 'file\t%s\t%s\n' "${input}"`,
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("rewritten verifier does not contain %q:\n%s", wanted, got)
		}
	}
}

func TestRewriteStandaloneToolingRelocatesSharedServiceFixtures(t *testing.T) {
	t.Parallel()

	input := []byte(`integration="${root}/pkg/rabbitstream/rabbitmq/integration"
source "${root}/pkg/search/adapters/opensearch/scripts/opensearch-images.env"
`)
	got := string(rewriteStandaloneTooling(
		input,
		".golib/scripts/start-services.sh",
		"github.com/faustbrian/go-queue",
	))
	for _, wanted := range []string{
		`${root}/.golib/services/rabbitstream`,
		`${root}/.golib/services/opensearch/opensearch-images.env`,
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("rewritten service tooling does not contain %q:\n%s", wanted, got)
		}
	}
	if strings.Contains(got, `${root}/pkg/`) {
		t.Fatalf("rewritten service tooling retains monorepo fixture path:\n%s", got)
	}
}

func TestWriteStandaloneWorkspaceIncludesEveryRepositoryModule(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	current := catalog{
		GoVersion: requiredGo,
		Modules: []module{
			{Directory: "."},
			{Directory: "adapters/queue"},
		},
	}
	if err := writeStandaloneWorkspace(directory, current); err != nil {
		t.Fatalf("writeStandaloneWorkspace() error = %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(directory, "go.work"))
	if err != nil {
		t.Fatalf("read go.work: %v", err)
	}
	want := "go " + requiredGo + "\n\nuse (\n\t.\n\t./adapters/queue\n)\n"
	if string(contents) != want {
		t.Fatalf("go.work =\n%s\nwant:\n%s", contents, want)
	}
}

func TestFilterStandaloneMutationRecordsRetainsOnlyOwningFamily(t *testing.T) {
	t.Parallel()

	records := []map[string]any{
		{"module": "pkg/queue", "package": "."},
		{"module": "pkg/queue/queueservice", "package": "."},
		{"module": "pkg/kafka", "package": "."},
	}
	got := filterStandaloneMutationRecords(records, "pkg/queue", "module")
	if len(got) != 2 || got[0]["module"] != "." || got[1]["module"] != "queueservice" {
		t.Fatalf("filterStandaloneMutationRecords() = %#v", got)
	}
}
