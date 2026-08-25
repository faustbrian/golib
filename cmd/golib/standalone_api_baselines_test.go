package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshStandaloneAPIBaselineRebuildsBinarySnapshotWithCanonicalTarget(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	moduleRoot := filepath.Join(destination, "go-example", "adapter")
	baseline := filepath.Join(destination, "go-example", "api", "stable.txt")
	if err := os.MkdirAll(filepath.Dir(baseline), 0o755); err != nil {
		t.Fatalf("create baseline directory: %v", err)
	}
	if err := os.MkdirAll(moduleRoot, 0o755); err != nil {
		t.Fatalf("create module directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(moduleRoot, "subpackage"), 0o755); err != nil {
		t.Fatalf("create package directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module github.com/faustbrian/go-example/adapter\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(
		baseline,
		[]byte("github.com/faustbrian/golib/pkg/example/adapter/subpackage\n\x00legacy"),
		0o640,
	); err != nil {
		t.Fatalf("write legacy baseline: %v", err)
	}

	called := false
	err := refreshStandaloneAPIBaseline(
		baseline,
		map[string]string{
			"github.com/faustbrian/golib/pkg/example":         "github.com/faustbrian/go-example",
			"github.com/faustbrian/golib/pkg/example/adapter": "github.com/faustbrian/go-example/adapter",
		},
		map[string]string{
			"github.com/faustbrian/go-example":         filepath.Join(destination, "go-example"),
			"github.com/faustbrian/go-example/adapter": moduleRoot,
		},
		func(gotRoot, target, output string) error {
			called = true
			if gotRoot != moduleRoot {
				t.Fatalf("module root = %q, want %q", gotRoot, moduleRoot)
			}
			want := "github.com/faustbrian/go-example/adapter/subpackage"
			if target != want {
				t.Fatalf("target = %q, want %q", target, want)
			}
			return os.WriteFile(output, append([]byte(target+"\n\x00"), []byte("canonical")...), 0o600)
		},
	)
	if err != nil {
		t.Fatalf("refreshStandaloneAPIBaseline() error = %v", err)
	}
	if !called {
		t.Fatal("baseline generator was not called")
	}
	contents, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read refreshed baseline: %v", err)
	}
	if bytes.Contains(contents, []byte("github.com/faustbrian/golib/pkg")) {
		t.Fatalf("refreshed baseline retains monorepo identity: %q", contents)
	}
	if info, err := os.Stat(baseline); err != nil {
		t.Fatalf("inspect refreshed baseline: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("refreshed baseline mode = %o, want 640", info.Mode().Perm())
	}
}

func TestStandaloneAPIBaselineTargetRejectsTextReferences(t *testing.T) {
	t.Parallel()

	contents := []byte("github.com/faustbrian/golib/pkg/example\nplain text")
	if target, ok := standaloneAPIBaselineTarget(contents); ok {
		t.Fatalf("standaloneAPIBaselineTarget() = %q, true", target)
	}
}

func TestStaleStandaloneAPIBaselinesFindsSnapshotOutsideAPIDirectory(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	baseline := filepath.Join(destination, "specification", "api-v0.txt")
	if err := os.MkdirAll(filepath.Dir(baseline), 0o755); err != nil {
		t.Fatalf("create specification directory: %v", err)
	}
	if err := os.WriteFile(
		baseline,
		[]byte("github.com/faustbrian/golib/pkg/example\n\x00snapshot"),
		0o644,
	); err != nil {
		t.Fatalf("write API snapshot: %v", err)
	}
	files, err := staleStandaloneAPIBaselines(destination)
	if err != nil {
		t.Fatalf("staleStandaloneAPIBaselines() error = %v", err)
	}
	if len(files) != 1 || files[0] != baseline {
		t.Fatalf("stale baselines = %v, want [%s]", files, baseline)
	}
}

func TestRewriteStandaloneAPITargetUsesLongestModulePrefix(t *testing.T) {
	t.Parallel()

	target, ok := rewriteStandaloneAPITarget(
		"github.com/faustbrian/golib/pkg/example/adapter/subpackage",
		map[string]string{
			"github.com/faustbrian/golib/pkg/example":         "github.com/faustbrian/go-example",
			"github.com/faustbrian/golib/pkg/example/adapter": "github.com/faustbrian/go-example/adapter",
		},
	)
	if !ok {
		t.Fatal("rewriteStandaloneAPITarget() did not match")
	}
	want := "github.com/faustbrian/go-example/adapter/subpackage"
	if target != want {
		t.Fatalf("rewriteStandaloneAPITarget() = %q, want %q", target, want)
	}
}

func TestResolveStandaloneAPIBaselineUsesPhysicalModuleForHistoricalPackageAlias(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	moduleRoot := filepath.Join(repository, "adapters", "math")
	baseline := filepath.Join(moduleRoot, "api", "baseline.txt")
	if err := os.MkdirAll(filepath.Dir(baseline), 0o755); err != nil {
		t.Fatalf("create baseline directory: %v", err)
	}
	target, root, ok := resolveStandaloneAPIBaseline(
		baseline,
		"github.com/faustbrian/go-rule-engine/adapters/gomath",
		map[string]string{
			"github.com/faustbrian/go-rule-engine":               repository,
			"github.com/faustbrian/go-rule-engine/adapters/math": moduleRoot,
		},
	)
	if !ok {
		t.Fatal("resolveStandaloneAPIBaseline() did not resolve")
	}
	if target != "github.com/faustbrian/go-rule-engine/adapters/math" {
		t.Fatalf("target = %q", target)
	}
	if root != moduleRoot {
		t.Fatalf("module root = %q, want %q", root, moduleRoot)
	}
}
