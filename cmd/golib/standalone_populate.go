package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type standalonePackageCatalog struct {
	// SchemaVersion identifies the generated package-catalog contract.
	SchemaVersion int `json:"schema_version"`
	// Repository is the standalone root module path.
	Repository string `json:"repository"`
	// Packages inventories every package retained in the repository.
	Packages []packageInfo `json:"packages"`
}

func populateStandaloneRepositories(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-populate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
	family := flags.String("family", "", "populate only one package family")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	manifest := standaloneManifest{}
	if err := readStandaloneJSON(
		filepath.Join(root, "migration/standalone/repositories.json"),
		&manifest,
	); err != nil {
		return err
	}
	current := catalog{}
	if err := readStandaloneJSON(filepath.Join(root, "modules.json"), &current); err != nil {
		return err
	}
	paths := make(map[string]string, len(manifest.Modules))
	for _, item := range manifest.Modules {
		paths[item.PreviousPath] = item.Path
	}

	selected := 0
	for _, repository := range manifest.Repositories {
		if *family != "" && repository.Family != *family {
			continue
		}
		selected++
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		if err := populateStandaloneRepository(
			root,
			destination,
			repository,
			current,
			paths,
		); err != nil {
			return fmt.Errorf("%s: %w", repository.Family, err)
		}
	}
	if selected == 0 {
		return fmt.Errorf("no repository matched family %q", *family)
	}

	return nil
}

func populateStandaloneRepository(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	current catalog,
	paths map[string]string,
) error {
	if err := requireStandaloneDestination(destination, repository.Name); err != nil {
		return err
	}
	if err := restoreStandaloneTrackedFiles(destination); err != nil {
		return err
	}
	if err := removeLegacyRootTooling(sourceRoot, destination); err != nil {
		return err
	}
	if err := installStandaloneFoundation(
		sourceRoot,
		destination,
		repository,
		paths,
	); err != nil {
		return err
	}
	files, err := standaloneTrackedFiles(destination)
	if err != nil {
		return err
	}
	for _, relative := range files {
		filename := filepath.Join(destination, relative)
		info, err := os.Lstat(filename)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", relative, err)
		}
		if bytes.IndexByte(contents, 0) >= 0 {
			continue
		}
		rewritten := rewriteStandaloneContents(contents, paths, relative == "go.mod" || strings.HasSuffix(relative, "/go.mod"))
		rewritten = rewriteStandaloneRepositoryPaths(
			rewritten,
			repository.Family,
			repository.Name,
		)
		if bytes.Equal(contents, rewritten) {
			continue
		}
		if err := os.WriteFile(filename, rewritten, info.Mode().Perm()); err != nil {
			return fmt.Errorf("rewrite %s: %w", relative, err)
		}
	}

	repositoryCatalog, err := standaloneCatalog(
		current,
		repository.Family,
		repository.Name,
		paths,
	)
	if err != nil {
		return err
	}
	if err := writeStandaloneJSON(filepath.Join(destination, "modules.json"), repositoryCatalog); err != nil {
		return err
	}
	packages := make([]packageInfo, 0)
	for _, item := range repositoryCatalog.Modules {
		packages = append(packages, item.Packages...)
	}
	sort.Slice(packages, func(left, right int) bool {
		return packages[left].Import < packages[right].Import
	})
	if err := writeStandaloneJSON(filepath.Join(destination, "packages.json"), standalonePackageCatalog{
		SchemaVersion: 1,
		Repository:    repositoryCatalog.Repository,
		Packages:      packages,
	}); err != nil {
		return err
	}
	if len(repositoryCatalog.Modules) > 1 {
		if err := writeStandaloneWorkspace(destination, repositoryCatalog); err != nil {
			return err
		}
	}

	return nil
}

func removeLegacyRootTooling(sourceRoot string, destination string) error {
	sourceScripts := filepath.Join(sourceRoot, "scripts")
	err := filepath.WalkDir(sourceScripts, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, filename)
		if err != nil {
			return err
		}
		candidate := filepath.Join(destination, relative)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return fmt.Errorf("inspect legacy root tooling %s: %w", relative, err)
		}
		tracked := exec.Command("git", "-C", destination, "ls-files", "--error-unmatch", relative)
		if tracked.Run() == nil {
			return nil
		}
		if err := os.Remove(candidate); err != nil {
			return fmt.Errorf("remove legacy generated tooling %s: %w", relative, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, relative := range []string{
		"scripts/repository-check.sh",
		"scripts/with-disposable-go-cache.sh",
	} {
		candidate := filepath.Join(destination, relative)
		tracked := exec.Command("git", "-C", destination, "ls-files", "--error-unmatch", relative)
		if tracked.Run() == nil {
			continue
		}
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove legacy generated tooling %s: %w", relative, err)
		}
	}
	return nil
}

func restoreStandaloneTrackedFiles(destination string) error {
	files, err := standaloneTrackedFiles(destination)
	if err != nil {
		return err
	}
	for _, relative := range files {
		filename := filepath.Join(destination, relative)
		info, err := os.Lstat(filename)
		if err != nil {
			return fmt.Errorf("inspect tracked file %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		command := exec.Command("git", "-C", destination, "show", "HEAD:"+relative)
		contents, err := command.Output()
		if err != nil {
			return fmt.Errorf("restore tracked file %s from extracted HEAD: %w", relative, err)
		}
		if err := os.WriteFile(filename, contents, info.Mode().Perm()); err != nil {
			return fmt.Errorf("restore tracked file %s: %w", relative, err)
		}
	}
	return nil
}

func requireStandaloneDestination(destination string, repository string) error {
	if _, err := os.Stat(filepath.Join(destination, ".git")); err != nil {
		return fmt.Errorf("destination is not an initialized repository: %w", err)
	}
	command := exec.Command("git", "-C", destination, "remote", "get-url", "origin")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read origin: %w", err)
	}
	want := "git@github.com:faustbrian/" + repository + ".git"
	if strings.TrimSpace(string(output)) != want {
		return fmt.Errorf("origin = %q, want %q", strings.TrimSpace(string(output)), want)
	}
	return nil
}

func standaloneTrackedFiles(destination string) ([]string, error) {
	command := exec.Command("git", "-C", destination, "ls-files", "-z")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}
	parts := bytes.Split(output, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result, nil
}

func rewriteStandaloneRepositoryPaths(contents []byte, family string, repository string) []byte {
	prefix := "pkg/" + family
	replacements := []struct{ previous, next string }{
		{
			"https://github.com/faustbrian/golib/blob/main/" + prefix + "/",
			"https://github.com/faustbrian/" + repository + "/blob/main/",
		},
		{
			"https://github.com/faustbrian/golib/commits/main/" + prefix,
			"https://github.com/faustbrian/" + repository + "/commits/main",
		},
		{prefix + "/", ""},
	}
	for _, replacement := range replacements {
		contents = bytes.ReplaceAll(
			contents,
			[]byte(replacement.previous),
			[]byte(replacement.next),
		)
	}
	exactPath := regexp.MustCompile(
		`(^|[^A-Za-z0-9_/-])` + regexp.QuoteMeta(prefix) + `([^A-Za-z0-9_/-]|$)`,
	)
	contents = exactPath.ReplaceAll(contents, []byte(`${1}.${2}`))
	return contents
}

func writeStandaloneWorkspace(destination string, current catalog) error {
	var contents strings.Builder
	contents.WriteString("go ")
	contents.WriteString(current.GoVersion)
	contents.WriteString("\n\nuse (\n")
	for _, item := range current.Modules {
		contents.WriteString("\t")
		if item.Directory == "." {
			contents.WriteString(".")
		} else {
			contents.WriteString("./")
			contents.WriteString(item.Directory)
		}
		contents.WriteString("\n")
	}
	contents.WriteString(")\n")
	return os.WriteFile(filepath.Join(destination, "go.work"), []byte(contents.String()), 0o644)
}

func readStandaloneJSON(filename string, target any) error {
	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		return fmt.Errorf("decode %s: %w", filename, err)
	}
	return nil
}

func writeStandaloneJSON(filename string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filename, err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(filename, contents, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}

func rewriteStandaloneContents(contents []byte, paths map[string]string, goMod bool) []byte {
	keys := make([]string, 0, len(paths))
	for previous := range paths {
		keys = append(keys, previous)
	}
	sort.Slice(keys, func(left, right int) bool {
		return len(keys[left]) > len(keys[right])
	})

	rewritten := slices.Clone(contents)
	for _, previous := range keys {
		rewritten = bytes.ReplaceAll(rewritten, []byte(previous), []byte(paths[previous]))
	}
	if !goMod {
		return rewritten
	}

	for _, modulePath := range paths {
		pattern := regexp.MustCompile(
			`(` + regexp.QuoteMeta(modulePath) + `\s+)v0\.0\.0(\s|$)`,
		)
		rewritten = pattern.ReplaceAll(rewritten, []byte(`${1}v1.0.0${2}`))
	}

	return rewritten
}

func standaloneCatalog(
	current catalog,
	family string,
	repository string,
	paths map[string]string,
) (catalog, error) {
	prefix := "pkg/" + family
	result := catalog{
		SchemaVersion: current.SchemaVersion,
		Repository:    standaloneModulePrefix + repository,
		GoVersion:     current.GoVersion,
	}
	for _, item := range current.Modules {
		if item.Directory != prefix && !strings.HasPrefix(item.Directory, prefix+"/") {
			continue
		}
		item.Directory = rebaseStandalonePath(item.Directory, prefix)
		item.Path = rewriteStandalonePath(item.Path, paths)
		item.Purpose = string(rewriteStandaloneRepositoryPaths(
			rewriteStandaloneContents([]byte(item.Purpose), paths, false),
			family,
			repository,
		))
		item.TagPrefix = "v"
		if item.Directory != "." {
			item.TagPrefix = item.Directory + "/v"
		}
		item.OwnedDependencies = rewriteStandalonePaths(item.OwnedDependencies, paths)
		item.ReverseDependencies = rewriteStandalonePaths(item.ReverseDependencies, paths)
		item.Goals = rebaseStandalonePaths(item.Goals, prefix)
		item.Specifications = rebaseStandalonePaths(item.Specifications, prefix)
		item.ConformanceCorpora = rebaseStandalonePaths(item.ConformanceCorpora, prefix)
		item.Provenance = rebaseStandalonePaths(item.Provenance, prefix)
		for index := range item.GoalEvidence {
			item.GoalEvidence[index].File = rebaseStandalonePath(
				item.GoalEvidence[index].File,
				prefix,
			)
			item.GoalEvidence[index].ImplementationEvidence = rebaseStandalonePaths(
				item.GoalEvidence[index].ImplementationEvidence,
				prefix,
			)
		}
		for index := range item.Packages {
			item.Packages[index].ModuleDirectory = rebaseStandalonePath(
				item.Packages[index].ModuleDirectory,
				prefix,
			)
			item.Packages[index].Import = rewriteStandalonePath(
				item.Packages[index].Import,
				paths,
			)
		}
		result.Modules = append(result.Modules, item)
	}
	if len(result.Modules) == 0 {
		return catalog{}, fmt.Errorf("catalog contains no modules for family %s", family)
	}

	return result, nil
}

func rewriteStandalonePaths(values []string, paths map[string]string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = rewriteStandalonePath(value, paths)
	}
	return result
}

func rewriteStandalonePath(value string, paths map[string]string) string {
	keys := make([]string, 0, len(paths))
	for previous := range paths {
		keys = append(keys, previous)
	}
	sort.Slice(keys, func(left, right int) bool {
		return len(keys[left]) > len(keys[right])
	})
	for _, previous := range keys {
		if value == previous {
			return paths[previous]
		}
		if strings.HasPrefix(value, previous+"/") {
			return paths[previous] + strings.TrimPrefix(value, previous)
		}
	}
	return value
}

func rebaseStandalonePaths(values []string, prefix string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = rebaseStandalonePath(value, prefix)
	}
	return result
}

func rebaseStandalonePath(value string, prefix string) string {
	if value == prefix {
		return "."
	}
	if strings.HasPrefix(value, prefix+"/") {
		return strings.TrimPrefix(value, prefix+"/")
	}
	return value
}
