package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestStandaloneModuleArchiveExcludesNestedModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeProxyTestFile(t, root, "go.mod", "module github.com/faustbrian/go-queue\n")
	writeProxyTestFile(t, root, "queue.go", "package queue\n")
	writeProxyTestFile(t, root, "queueservice/go.mod", "module github.com/faustbrian/go-queue/queueservice\n")
	writeProxyTestFile(t, root, "queueservice/service.go", "package queueservice\n")
	item := standaloneModulePlan{
		Directory:  ".",
		Path:       "github.com/faustbrian/go-queue",
		Repository: "go-queue",
	}
	modules := []standaloneModulePlan{
		item,
		{
			Directory:  "queueservice",
			Path:       "github.com/faustbrian/go-queue/queueservice",
			Repository: "go-queue",
		},
	}

	archive, err := standaloneModuleArchive(root, item, modules)
	if err != nil {
		t.Fatalf("standaloneModuleArchive() error = %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("open module archive: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	want := []string{
		"github.com/faustbrian/go-queue@v1.0.0/go.mod",
		"github.com/faustbrian/go-queue@v1.0.0/queue.go",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("archive entries = %v, want %v", names, want)
	}
}

func TestStandaloneModuleArchiveIsDeterministic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeProxyTestFile(t, root, "go.mod", "module github.com/faustbrian/go-clock\n")
	item := standaloneModulePlan{
		Directory:  ".",
		Path:       "github.com/faustbrian/go-clock",
		Repository: "go-clock",
	}
	first, err := standaloneModuleArchive(root, item, []standaloneModulePlan{item})
	if err != nil {
		t.Fatalf("first standaloneModuleArchive() error = %v", err)
	}
	second, err := standaloneModuleArchive(root, item, []standaloneModulePlan{item})
	if err != nil {
		t.Fatalf("second standaloneModuleArchive() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("standalone module archives are not deterministic")
	}
}

func TestStandaloneProxyModulesSelectsCompleteReleaseWaves(t *testing.T) {
	t.Parallel()

	manifest := standaloneManifest{
		Modules: []standaloneModulePlan{
			{Path: "example.test/fixture"},
			{Path: "github.com/faustbrian/go-first", Releasable: true},
			{Path: "github.com/faustbrian/go-second", Releasable: true},
		},
		ReleaseWaves: [][]string{
			{"github.com/faustbrian/go-first"},
			{"github.com/faustbrian/go-second"},
		},
	}

	firstWave, err := standaloneProxyModules(manifest, 1)
	if err != nil {
		t.Fatalf("standaloneProxyModules() error = %v", err)
	}
	if len(firstWave) != 1 || firstWave[0].Path != "github.com/faustbrian/go-first" {
		t.Fatalf("first wave modules = %#v", firstWave)
	}
	empty, err := standaloneProxyModules(manifest, 0)
	if err != nil {
		t.Fatalf("standaloneProxyModules(empty) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty proxy modules = %#v", empty)
	}

	all, err := standaloneProxyModules(manifest, -1)
	if err != nil {
		t.Fatalf("standaloneProxyModules(all) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all releasable modules = %#v", all)
	}

	if _, err := standaloneProxyModules(manifest, 3); err == nil {
		t.Fatal("standaloneProxyModules() accepted an unavailable release wave")
	}
}

func writeProxyTestFile(t *testing.T, root string, relative string, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
