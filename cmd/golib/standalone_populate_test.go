package main

import (
	"encoding/json"
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
	input := `module github.com/faustbrian/golib/pkg/outbox/adapters/queue

require (
	github.com/faustbrian/golib/pkg/outbox v0.0.0
	github.com/faustbrian/golib/pkg/queue v0.0.0
)
`

	got := rewriteStandaloneContents([]byte(input), paths, true)
	want := `module github.com/faustbrian/go-transactional-outbox/adapters/queue

require (
	github.com/faustbrian/go-transactional-outbox v1.0.0
	github.com/faustbrian/go-queue v1.0.0
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
		false,
	)
	if string(got) != "github.com/faustbrian/go-queue/queueservice/worker" {
		t.Fatalf("rewriteStandaloneContents() = %q", got)
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

	got, err := standaloneCatalog(current, "outbox", "go-transactional-outbox", paths)
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
	}}}, "outbox", "go-transactional-outbox", nil)
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
