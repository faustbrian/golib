package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const standaloneModulePrefix = "github.com/faustbrian/"

type standaloneManifest struct {
	// SchemaVersion identifies the migration manifest contract.
	SchemaVersion int `json:"schema_version"`
	// Source binds extraction to the canonical verified monorepo state.
	Source standaloneSource `json:"source"`
	// Counts makes scope drift visible before repositories are changed.
	Counts standaloneCounts `json:"counts"`
	// Repositories lists every independently maintained destination.
	Repositories []standaloneRepository `json:"repositories"`
	// Modules maps releasable modules and retained non-release harnesses.
	Modules []standaloneModulePlan `json:"modules"`
	// ReleaseWaves orders modules so dependencies are public first.
	ReleaseWaves [][]string `json:"release_waves"`
	// ExcludedFamilies records intentionally deferred package families.
	ExcludedFamilies []string `json:"excluded_families"`
	// Collisions records immutable public versions requiring new identities.
	Collisions []standaloneCollision `json:"public_version_collisions"`
}

type standaloneSource struct {
	// Repository is the canonical implementation source.
	Repository string `json:"repository"`
	// Commit is the exact source tree selected for extraction.
	Commit string `json:"commit"`
	// CIRun is the successful workflow run proving the source tree.
	CIRun int64 `json:"ci_run"`
}

type standaloneCounts struct {
	// Repositories is the required number of standalone destinations.
	Repositories int `json:"repositories"`
	// Modules includes releasable modules and repository-local harness modules.
	Modules int `json:"modules"`
	// ReleasableModules is the number of modules requiring initial stable tags.
	ReleasableModules int `json:"releasable_modules"`
}

type standaloneRepository struct {
	// Family is the former top-level pkg directory name.
	Family string `json:"family"`
	// Name is the destination GitHub repository name.
	Name string `json:"name"`
	// LegacyName identifies the disposable repository being replaced.
	LegacyName string `json:"legacy_repository"`
	// ModulePath is the root public Go module path.
	ModulePath string `json:"module_path"`
	// SourceDirectory is the directory extracted from the monorepo.
	SourceDirectory string `json:"source_directory"`
	// DestinationDirectory is the local checkout name under the migration root.
	DestinationDirectory string `json:"destination_directory"`
	// Modules lists all module identities retained in this repository.
	Modules []string `json:"modules"`
}

type standaloneModulePlan struct {
	// SourceDirectory is the module's former monorepo location.
	SourceDirectory string `json:"source_directory"`
	// Directory is the module's repository-relative standalone location.
	Directory string `json:"directory"`
	// PreviousPath is the module identity before extraction.
	PreviousPath string `json:"previous_path"`
	// Path is the standalone module identity.
	Path string `json:"module_path"`
	// Repository names the owning standalone destination.
	Repository string `json:"repository"`
	// Releasable distinguishes public modules from fixtures and harnesses.
	Releasable bool `json:"releasable"`
	// ReleaseVersion is the immutable public semantic version to publish.
	ReleaseVersion string `json:"release_version,omitempty"`
	// ReleaseTag is the root or directory-prefixed stable release tag.
	ReleaseTag string `json:"release_tag,omitempty"`
	// OwnedDependencies contains rewritten standalone module dependencies.
	OwnedDependencies []string `json:"owned_dependencies"`
}

type standaloneCollision struct {
	// PreviousPath is the immutable public module identity that cannot be reused.
	PreviousPath string `json:"previous_path"`
	// Version is the cached semantic version whose content cannot be replaced.
	Version string `json:"version"`
	// Replacement is the module identity selected for the canonical code.
	Replacement string `json:"replacement_path"`
	// ReplacementVersion is the first version available for canonical content.
	ReplacementVersion string `json:"replacement_version"`
	// Reason explains why the cached version cannot contain canonical content.
	Reason string `json:"reason"`
}

func writeStandaloneManifest(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-manifest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sourceCommit := flags.String("source-commit", "", "verified canonical source commit")
	ciRun := flags.Int64("ci-run", 0, "successful canonical CI run")
	output := flags.String(
		"output",
		"migration/standalone/repositories.json",
		"repository-relative output path",
	)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *sourceCommit == "" {
		return fmt.Errorf("--source-commit is required")
	}
	if *ciRun <= 0 {
		return fmt.Errorf("--ci-run must be positive")
	}
	if filepath.IsAbs(*output) {
		return fmt.Errorf("--output must be repository-relative")
	}
	cleanOutput := filepath.Clean(*output)
	if cleanOutput == ".." || strings.HasPrefix(cleanOutput, ".."+string(filepath.Separator)) {
		return fmt.Errorf("--output must stay inside the repository")
	}

	current := catalog{}
	contents, err := os.ReadFile(filepath.Join(root, "modules.json"))
	if err != nil {
		return fmt.Errorf("read modules.json: %w", err)
	}
	if err := json.Unmarshal(contents, &current); err != nil {
		return fmt.Errorf("decode modules.json: %w", err)
	}
	manifest, err := buildStandaloneManifest(current, *sourceCommit, *ciRun)
	if err != nil {
		return err
	}
	if manifest.Counts.Repositories != 82 || manifest.Counts.ReleasableModules != 110 {
		return fmt.Errorf(
			"unexpected migration scope: got %d repositories and %d releasable modules",
			manifest.Counts.Repositories,
			manifest.Counts.ReleasableModules,
		)
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	outputPath := filepath.Join(root, cleanOutput)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".repositories-*.json")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}

	return nil
}

func buildStandaloneManifest(current catalog, sourceCommit string, ciRun int64) (standaloneManifest, error) {
	manifest := standaloneManifest{
		SchemaVersion: 1,
		Source: standaloneSource{
			Repository: canonicalRoot,
			Commit:     sourceCommit,
			CIRun:      ciRun,
		},
		ExcludedFamilies: []string{"secret-store"},
		Collisions: []standaloneCollision{
			{
				PreviousPath:       standaloneModulePrefix + "go-outbox",
				Version:            "v1.0.0",
				Replacement:        standaloneModulePrefix + "go-transactional-outbox",
				ReplacementVersion: "v1.0.0",
				Reason:             "the previous v1.0.0 is immutable in the public Go proxy and checksum database",
			},
			{
				PreviousPath:       standaloneModulePrefix + "go-postgres",
				Version:            "v1.0.0",
				Replacement:        standaloneModulePrefix + "go-postgres",
				ReplacementVersion: "v1.0.1",
				Reason:             "the previous v1.0.0 is immutable; canonical content starts at v1.0.1",
			},
		},
	}

	pathMap := make(map[string]string)
	for _, item := range current.Modules {
		if item.Directory == "." || !strings.HasPrefix(item.Directory, "pkg/") {
			continue
		}
		family := strings.Split(strings.TrimPrefix(item.Directory, "pkg/"), "/")[0]
		if family == "secret-store" {
			continue
		}
		newPath, err := standalonePath(item.Path)
		if err != nil {
			return standaloneManifest{}, err
		}
		pathMap[item.Path] = newPath
	}

	repositories := make(map[string]*standaloneRepository)
	for _, item := range current.Modules {
		if item.Directory == "." || !strings.HasPrefix(item.Directory, "pkg/") {
			continue
		}
		relative := strings.TrimPrefix(item.Directory, "pkg/")
		family, suffix, _ := strings.Cut(relative, "/")
		if family == "secret-store" {
			continue
		}

		newPath := pathMap[item.Path]
		repositoryName := standaloneRepositoryName(family)
		repository, ok := repositories[family]
		if !ok {
			repository = &standaloneRepository{
				Family:               family,
				Name:                 repositoryName,
				LegacyName:           legacyStandaloneRepositoryName(family),
				ModulePath:           standaloneModulePrefix + repositoryName,
				SourceDirectory:      "pkg/" + family,
				DestinationDirectory: repositoryName,
			}
			repositories[family] = repository
		}
		repository.Modules = append(repository.Modules, newPath)

		directory := "."
		if suffix != "" {
			directory = suffix
		}
		dependencies := make([]string, 0, len(item.OwnedDependencies))
		for _, dependency := range item.OwnedDependencies {
			mapped, exists := pathMap[dependency]
			if !exists {
				return standaloneManifest{}, fmt.Errorf("module %s has unknown owned dependency %s", item.Path, dependency)
			}
			dependencies = append(dependencies, mapped)
		}
		sort.Strings(dependencies)

		releaseTag := ""
		releaseVersion := ""
		if item.Releasable {
			releaseVersion = standaloneReleaseVersionForPath(newPath)
			releaseTag = releaseVersion
			if directory != "." {
				releaseTag = path.Clean(directory) + "/" + releaseVersion
			}
		}
		manifest.Modules = append(manifest.Modules, standaloneModulePlan{
			SourceDirectory:   item.Directory,
			Directory:         directory,
			PreviousPath:      item.Path,
			Path:              newPath,
			Repository:        repositoryName,
			Releasable:        item.Releasable,
			ReleaseVersion:    releaseVersion,
			ReleaseTag:        releaseTag,
			OwnedDependencies: dependencies,
		})
	}

	for _, repository := range repositories {
		sort.Strings(repository.Modules)
		manifest.Repositories = append(manifest.Repositories, *repository)
	}
	sort.Slice(manifest.Repositories, func(left, right int) bool {
		return manifest.Repositories[left].Name < manifest.Repositories[right].Name
	})
	sort.Slice(manifest.Modules, func(left, right int) bool {
		return manifest.Modules[left].Path < manifest.Modules[right].Path
	})

	waves, err := standaloneReleaseWaves(manifest.Modules)
	if err != nil {
		return standaloneManifest{}, err
	}
	manifest.ReleaseWaves = waves
	manifest.Counts.Repositories = len(manifest.Repositories)
	manifest.Counts.Modules = len(manifest.Modules)
	for _, item := range manifest.Modules {
		if item.Releasable {
			manifest.Counts.ReleasableModules++
		}
	}

	return manifest, nil
}

func standaloneRepositoryName(family string) string {
	switch family {
	case "outbox":
		return "go-transactional-outbox"
	case "rabbitstream":
		return "go-rabbitmq-streams"
	default:
		return "go-" + family
	}
}

func legacyStandaloneRepositoryName(family string) string {
	switch family {
	case "outbox":
		return "go-outbox"
	case "postgres":
		return "go-postgresql"
	default:
		return standaloneRepositoryName(family)
	}
}

func standaloneReleaseVersionForPath(modulePath string) string {
	if modulePath == standaloneModulePrefix+"go-postgres" {
		return "v1.0.1"
	}
	return "v1.0.0"
}

func standalonePath(modulePath string) (string, error) {
	relative, ok := strings.CutPrefix(modulePath, canonicalRoot+"/pkg/")
	if !ok {
		return modulePath, nil
	}
	family, suffix, _ := strings.Cut(relative, "/")
	result := standaloneModulePrefix + standaloneRepositoryName(family)
	if suffix != "" {
		result += "/" + suffix
	}

	return result, nil
}

func standaloneReleaseWaves(modules []standaloneModulePlan) ([][]string, error) {
	remaining := make(map[string][]string)
	for _, item := range modules {
		if item.Releasable {
			remaining[item.Path] = slices.Clone(item.OwnedDependencies)
		}
	}

	waves := make([][]string, 0)
	for len(remaining) > 0 {
		wave := make([]string, 0)
		for modulePath, dependencies := range remaining {
			ready := true
			for _, dependency := range dependencies {
				if _, pending := remaining[dependency]; pending {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, modulePath)
			}
		}
		if len(wave) == 0 {
			return nil, fmt.Errorf("standalone module dependency graph contains a cycle")
		}
		sort.Strings(wave)
		waves = append(waves, wave)
		for _, modulePath := range wave {
			delete(remaining, modulePath)
		}
	}

	return waves, nil
}
