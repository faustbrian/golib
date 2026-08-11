package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCohesionContract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCohesionModuleDocs(t, root, "pkg/clock")
	current := catalog{Modules: []module{
		{Directory: ".", Releasable: false},
		{
			Directory:         "pkg/clock",
			Releasable:        true,
			Family:            "foundations",
			FamilyLabel:       "Foundations",
			FamilyDescription: "Shared immutable values.",
			FamilyOrder:       1,
			Packages:          []packageInfo{{Directory: ".", Name: "clock", Kind: "public", Production: true}},
		},
	}}
	if err := validateCohesionContract(root, current); err != nil {
		t.Fatalf("validateCohesionContract() error = %v", err)
	}
}

func TestValidateCohesionContractRejectsIncompleteMetadataAndEntryPoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		module module
		setup  func(t *testing.T, root, directory string)
		want   string
	}{
		{
			name: "family",
			module: module{
				Directory:  "pkg/clock",
				Releasable: true,
				Packages:   []packageInfo{{Directory: ".", Name: "clock", Kind: "public", Production: true}},
			},
			setup: writeCohesionModuleDocs,
			want:  "family metadata",
		},
		{
			name:   "readme",
			module: completeCohesionTestModule(),
			setup: func(t *testing.T, root, directory string) {
				mustWriteFile(t, filepath.Join(root, directory, "CHANGELOG.md"), "# Changelog\n")
				mustWriteFile(t, filepath.Join(root, directory, "LICENSE"), "MIT\n")
			},
			want: "README.md",
		},
		{
			name:   "public entry point",
			module: completeCohesionTestModule(),
			setup:  writeCohesionModuleDocs,
			want:   "public production package",
		},
	}
	tests[2].module.Packages = nil

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			test.setup(t, root, "pkg/clock")
			current := catalog{Modules: []module{test.module}}
			err := validateCohesionContract(root, current)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateCohesionContract() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateCohesionContractRejectsMetadataOnInternalModule(t *testing.T) {
	t.Parallel()
	current := catalog{Modules: []module{{
		Directory:   ".",
		Releasable:  false,
		Family:      "tooling",
		FamilyOrder: 1,
	}}}
	err := validateCohesionContract(t.TempDir(), current)
	if err == nil || !strings.Contains(err.Error(), "non-releasable") {
		t.Fatalf("validateCohesionContract() error = %v", err)
	}
}

func TestValidateCohesionContractRejectsConflictingFamilyDefinitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		second module
		want   string
	}{
		{
			name: "order",
			second: module{
				Directory:         "pkg/config",
				Releasable:        true,
				Family:            "service-edge",
				FamilyLabel:       "Service edge",
				FamilyDescription: "HTTP service boundaries.",
				FamilyOrder:       1,
				Packages:          []packageInfo{{Directory: ".", Name: "config", Kind: "public", Production: true}},
			},
			want: "family order 1",
		},
		{
			name: "description",
			second: module{
				Directory:         "pkg/config",
				Releasable:        true,
				Family:            "foundations",
				FamilyLabel:       "Foundations",
				FamilyDescription: "A conflicting description.",
				FamilyOrder:       1,
				Packages:          []packageInfo{{Directory: ".", Name: "config", Kind: "public", Production: true}},
			},
			want: "conflicting labels or descriptions",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeCohesionModuleDocs(t, root, "pkg/clock")
			writeCohesionModuleDocs(t, root, "pkg/config")
			current := catalog{Modules: []module{completeCohesionTestModule(), test.second}}
			err := validateCohesionContract(root, current)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateCohesionContract() error = %v, want %q", err, test.want)
			}
		})
	}
}

func completeCohesionTestModule() module {
	return module{
		Directory:         "pkg/clock",
		Releasable:        true,
		Family:            "foundations",
		FamilyLabel:       "Foundations",
		FamilyDescription: "Shared immutable values.",
		FamilyOrder:       1,
		Packages:          []packageInfo{{Directory: ".", Name: "clock", Kind: "public", Production: true}},
	}
}

func writeCohesionModuleDocs(t *testing.T, root, directory string) {
	t.Helper()
	for path, contents := range map[string]string{
		"README.md":    "# Clock\n",
		"CHANGELOG.md": "# Changelog\n",
		"LICENSE":      "MIT\n",
	} {
		mustWriteFile(t, filepath.Join(root, directory, path), contents)
	}
}

func TestCohesionEntryPointRejectsEmptyFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := validateCohesionEntryPoint(path); err == nil {
		t.Fatal("validateCohesionEntryPoint() error = nil")
	}
}

func TestCohesionEntryPointRejectsDirectories(t *testing.T) {
	t.Parallel()
	if err := validateCohesionEntryPoint(t.TempDir()); err == nil {
		t.Fatal("validateCohesionEntryPoint() error = nil")
	}
}
