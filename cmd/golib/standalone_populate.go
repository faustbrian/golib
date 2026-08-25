package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
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

func migrateStandaloneToolingReferences(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-tooling-references", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
	check := flags.Bool("check", false, "report stale references without changing them")
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
	changed := 0
	for _, repository := range manifest.Repositories {
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		modulePath := standaloneModulePrefix + repository.Name
		sharedTooling, err := standaloneSharedToolingPaths(destination)
		if err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
		files, err := standaloneTrackedFiles(destination)
		if err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
		for _, relative := range files {
			filename := filepath.Join(destination, relative)
			info, err := os.Lstat(filename)
			if err != nil {
				return fmt.Errorf("inspect %s/%s: %w", repository.Name, relative, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			contents, err := os.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("read %s/%s: %w", repository.Name, relative, err)
			}
			if bytes.IndexByte(contents, 0) >= 0 {
				continue
			}
			rewritten := contents
			if relative == ".gitignore" {
				rewritten = rewriteStandaloneGitignore(rewritten)
			}
			if relative == "CONTRIBUTING.md" {
				rewritten = rewriteStandaloneContributing(rewritten)
			}
			if relative == "CHANGELOG.md" {
				rewritten = rewriteStandaloneChangelog(rewritten)
			}
			if relative == "SECURITY.md" {
				rewritten = rewriteStandaloneSecurity(rewritten, repository.Name)
			}
			if relative == "cspell.json" {
				rewritten, err = rewriteStandaloneSpellingConfiguration(rewritten)
				if err != nil {
					return fmt.Errorf("rewrite %s/%s: %w", repository.Name, relative, err)
				}
			}
			if relative == ".golib/scripts/package-source-digest.sh" ||
				relative == ".golib/scripts/check-module.sh" ||
				relative == ".golib/scripts/check-documentation.sh" ||
				relative == ".golib/scripts/repository-check.sh" {
				rewritten = rewriteStandaloneTooling(rewritten, relative, modulePath)
			}
			rewritten = rewriteStandaloneSharedToolingReferences(
				rewritten,
				relative,
				destination,
				sharedTooling,
			)
			if bytes.Equal(contents, rewritten) {
				continue
			}
			changed++
			if *check {
				continue
			}
			if err := os.WriteFile(filename, rewritten, info.Mode().Perm()); err != nil {
				return fmt.Errorf("rewrite %s/%s: %w", repository.Name, relative, err)
			}
		}
	}
	if *check && changed != 0 {
		return fmt.Errorf("%d standalone files retain monorepo tooling references", changed)
	}
	if *check {
		fmt.Println("standalone tooling references are canonical")
	} else {
		fmt.Printf("rewrote standalone tooling references in %d files\n", changed)
	}

	return nil
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
	versions := make(map[string]string, len(manifest.Modules))
	for _, item := range manifest.Modules {
		paths[item.PreviousPath] = item.Path
		if item.Releasable {
			versions[item.Path] = item.ReleaseVersion
		}
	}
	for previous, replacement := range standaloneSupersededModulePaths() {
		paths[previous] = replacement
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
			versions,
		); err != nil {
			return fmt.Errorf("%s: %w", repository.Family, err)
		}
	}
	if selected == 0 {
		return fmt.Errorf("no repository matched family %q", *family)
	}

	return nil
}

func refreshStandaloneCI(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-ci-refresh", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
	family := flags.String("family", "", "refresh only one package family")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	manifest := standaloneManifest{}
	if err := readStandaloneJSON(
		filepath.Join(root, "migration/standalone/repositories.json"),
		&manifest,
	); err != nil {
		return err
	}
	selected := 0
	for _, repository := range manifest.Repositories {
		if *family != "" && repository.Family != *family {
			continue
		}
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		if err := requireStandaloneDestination(destination, repository.Name); err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
		if err := writeStandaloneCIContract(root, destination, repository); err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
		selected++
	}
	if selected == 0 {
		return fmt.Errorf("no repository matched family %q", *family)
	}
	fmt.Printf("refreshed standalone CI in %d repositories\n", selected)

	return nil
}

func writeStandaloneCIContract(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
) error {
	workflow := strings.ReplaceAll(standaloneCIWorkflow, "{{REPOSITORY}}", repository.Name)
	workflowPath := filepath.Join(destination, ".github", "workflows", "ci.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		return fmt.Errorf("create workflow directory: %w", err)
	}
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		return fmt.Errorf("write workflow: %w", err)
	}

	stage, err := os.ReadFile(filepath.Join(sourceRoot, "scripts", "stage-ci-evidence.sh"))
	if err != nil {
		return fmt.Errorf("read evidence stage: %w", err)
	}
	stagePath := filepath.Join(destination, ".golib", "scripts", "stage-ci-evidence.sh")
	if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
		return fmt.Errorf("create evidence stage directory: %w", err)
	}
	if err := os.WriteFile(stagePath, stage, 0o755); err != nil {
		return fmt.Errorf("write evidence stage: %w", err)
	}
	if err := copyStandaloneFoundationFileAs(
		sourceRoot,
		destination,
		"scripts/build-local-proxy.sh",
		filepath.Join(".golib", "scripts", "build-local-proxy.sh"),
		repository,
		map[string]string{},
	); err != nil {
		return fmt.Errorf("write local proxy builder: %w", err)
	}
	changelogPath := filepath.Join(destination, "CHANGELOG.md")
	changelog, err := os.ReadFile(changelogPath)
	if err != nil {
		return fmt.Errorf("read standalone changelog: %w", err)
	}
	if err := os.WriteFile(
		changelogPath,
		rewriteStandaloneChangelog(changelog),
		0o644,
	); err != nil {
		return fmt.Errorf("write standalone changelog: %w", err)
	}

	return nil
}

func standaloneSupersededModulePaths() map[string]string {
	return map[string]string{
		standaloneModulePrefix + "go-postgresql": standaloneModulePrefix + "go-postgres",
	}
}

func populateStandaloneRepository(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	current catalog,
	paths map[string]string,
	versions map[string]string,
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
		standaloneRepositoryRequiredServices(current, repository.Family),
	); err != nil {
		return err
	}
	files, err := standaloneExistingTrackedFiles(destination)
	if err != nil {
		return err
	}
	sharedTooling, err := standaloneSharedToolingPaths(destination)
	if err != nil {
		return err
	}
	changedGoFiles := make([]string, 0)
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
		if relative == "go.sum" || strings.HasSuffix(relative, "/go.sum") {
			contents = removeStandaloneOwnedChecksums(contents, paths)
		}
		rewritten := rewriteStandaloneContents(
			contents,
			paths,
			versions,
			relative == "go.mod" || strings.HasSuffix(relative, "/go.mod"),
		)
		rewritten = rewriteStandaloneRepositoryPaths(
			rewritten,
			repository.Family,
			repository.Name,
		)
		rewritten = rewriteStandaloneSharedToolingReferences(
			rewritten,
			relative,
			destination,
			sharedTooling,
		)
		if relative == "README.md" {
			rewritten = addStandaloneReadmeBadges(rewritten, repository)
		}
		if bytes.Equal(contents, rewritten) {
			continue
		}
		if filepath.Ext(relative) == ".go" {
			changedGoFiles = append(changedGoFiles, relative)
		}
		if err := os.WriteFile(filename, rewritten, info.Mode().Perm()); err != nil {
			return fmt.Errorf("rewrite %s: %w", relative, err)
		}
	}
	if err := formatStandaloneGoSources(destination, changedGoFiles); err != nil {
		return err
	}

	repositoryCatalog, err := standaloneCatalog(
		current,
		repository.Family,
		repository.Name,
		paths,
		versions,
	)
	if err != nil {
		return err
	}
	if err := addStandaloneChangelogEntries(destination, repositoryCatalog); err != nil {
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

func formatStandaloneGoSources(destination string, files []string) error {
	for _, relative := range files {
		if filepath.Ext(relative) != ".go" {
			continue
		}
		filename := filepath.Join(destination, relative)
		info, err := os.Lstat(filename)
		if err != nil {
			return fmt.Errorf("inspect Go source %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read Go source %s: %w", relative, err)
		}
		formatted, err := format.Source(contents)
		if err != nil {
			return fmt.Errorf("format Go source %s: %w", relative, err)
		}
		if bytes.Equal(contents, formatted) {
			continue
		}
		if err := os.WriteFile(filename, formatted, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write formatted Go source %s: %w", relative, err)
		}
	}

	return nil
}

func standaloneSharedToolingPaths(destination string) ([]string, error) {
	root := filepath.Join(destination, ".golib", "scripts")
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(filepath.Join(destination, ".golib"), filename)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))

		return nil
	})
	sort.Strings(paths)

	return paths, err
}

func rewriteStandaloneSharedToolingReferences(
	contents []byte,
	currentRelative string,
	destination string,
	sharedTooling []string,
) []byte {
	currentRelative = filepath.ToSlash(currentRelative)
	for _, sharedRelative := range sharedTooling {
		packageTool := filepath.Join(destination, filepath.FromSlash(sharedRelative))
		_, packageErr := os.Stat(packageTool)
		if packageErr == nil && currentRelative != sharedRelative {
			continue
		}
		if packageErr != nil && !os.IsNotExist(packageErr) {
			continue
		}
		installedRelative := ".golib/" + sharedRelative
		for _, prefix := range []string{
			"${root}/",
			"$root/",
			"$$(git rev-parse --show-toplevel)/",
			"$(git rev-parse --show-toplevel)/",
		} {
			contents = bytes.ReplaceAll(
				contents,
				[]byte(prefix+sharedRelative),
				[]byte(prefix+installedRelative),
			)
		}
	}

	return contents
}

func standaloneRepositoryRequiredServices(current catalog, family string) []string {
	services := make([]string, 0)
	for _, item := range current.Modules {
		if item.Family != family {
			continue
		}
		for _, service := range item.RequiredServices {
			if !slices.Contains(services, service) {
				services = append(services, service)
			}
		}
	}
	sort.Strings(services)
	return services
}

func addStandaloneChangelogEntries(destination string, current catalog) error {
	for _, item := range current.Modules {
		if !item.Releasable {
			continue
		}
		root := destination
		if item.Directory != "." {
			root = filepath.Join(destination, filepath.FromSlash(item.Directory))
		}
		filename := filepath.Join(root, "CHANGELOG.md")
		contents, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read changelog for %s: %w", item.Path, err)
		}
		rewritten, err := addStandaloneChangelogEntry(contents, item.Path)
		if err != nil {
			return fmt.Errorf("update changelog for %s: %w", item.Path, err)
		}
		if bytes.Equal(contents, rewritten) {
			continue
		}
		if err := os.WriteFile(filename, rewritten, 0o644); err != nil {
			return fmt.Errorf("write changelog for %s: %w", item.Path, err)
		}
	}
	return nil
}

func standaloneChangelogEntry(modulePath string) string {
	return "- Publish the module from its standalone `" + modulePath +
		"` identity while preserving its documented API and behavior."
}

func addStandaloneChangelogEntry(contents []byte, modulePath string) ([]byte, error) {
	entry := standaloneChangelogEntry(modulePath)
	if bytes.Contains(contents, []byte(entry)) {
		return contents, nil
	}

	text := strings.TrimRight(string(contents), "\n")
	unreleased := regexp.MustCompile(`(?m)^## (?:\[Unreleased\]|Unreleased)[ \t]*$`)
	heading := unreleased.FindStringIndex(text)
	if heading == nil {
		return nil, errors.New("missing Unreleased section")
	}
	sectionEnd := len(text)
	if next := strings.Index(text[heading[1]:], "\n## "); next >= 0 {
		sectionEnd = heading[1] + next
	}
	section := text[heading[1]:sectionEnd]
	changed := regexp.MustCompile(`(?m)^### Changed[ \t]*$`).FindStringIndex(section)

	var rewritten string
	if changed != nil {
		insertAt := heading[1] + changed[1]
		remainder := text[insertAt:]
		if strings.HasPrefix(remainder, "\n\n") &&
			!strings.HasPrefix(remainder, "\n\n#") {
			remainder = remainder[1:]
		}
		rewritten = text[:insertAt] + "\n\n" + entry + remainder
	} else {
		rewritten = text[:heading[1]] + "\n\n### Changed\n\n" + entry +
			text[heading[1]:]
	}
	return []byte(rewritten + "\n"), nil
}

func addStandaloneReadmeBadges(
	contents []byte,
	repository standaloneRepository,
) []byte {
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	heading := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "# ") {
			heading = index
			break
		}
	}
	if heading == -1 {
		return contents
	}

	body := heading + 1
	for body < len(lines) && strings.TrimSpace(lines[body]) == "" {
		body++
	}
	for body < len(lines) && strings.HasPrefix(lines[body], "[![") {
		body++
	}
	for body < len(lines) && strings.TrimSpace(lines[body]) == "" {
		body++
	}

	workflow := "https://github.com/faustbrian/" + repository.Name +
		"/actions/workflows/ci.yml"
	badges := []string{
		"[![CI](" + workflow + "/badge.svg?branch=main)](" + workflow + ")",
		"[![CodeQL](https://img.shields.io/badge/CodeQL-required-blue)](" + workflow + ")",
		"[![Coverage](https://img.shields.io/badge/coverage-100%25_required-blue)](CONTRIBUTING.md#verification)",
		"[![Mutation](https://img.shields.io/badge/mutation-100%25_required-blue)](CONTRIBUTING.md#verification)",
		"[![Documentation](https://img.shields.io/badge/docs-checked_in_CI-blue)](docs/)",
		"[![Go Reference](https://pkg.go.dev/badge/" + repository.ModulePath + ".svg)](https://pkg.go.dev/" + repository.ModulePath + ")",
		"[![Release](https://img.shields.io/github/v/release/faustbrian/" + repository.Name + "?sort=semver)](https://github.com/faustbrian/" + repository.Name + "/releases)",
		"[![Go](https://img.shields.io/badge/go-1.26.6-00ADD8?logo=go)](https://go.dev/)",
		"[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)",
	}

	result := make([]string, 0, len(lines)+len(badges)+2)
	result = append(result, lines[:heading+1]...)
	result = append(result, "")
	result = append(result, badges...)
	if body < len(lines) {
		result = append(result, "")
		result = append(result, lines[body:]...)
	}
	return []byte(strings.Join(result, "\n") + "\n")
}

func removeStandaloneOwnedChecksums(contents []byte, paths map[string]string) []byte {
	owned := make([]string, 0, len(paths)*2)
	for previous, current := range paths {
		owned = append(owned, previous, current)
	}
	lines := bytes.Split(contents, []byte{'\n'})
	kept := make([][]byte, 0, len(lines))
	for _, line := range lines {
		fields := bytes.Fields(line)
		remove := false
		if len(fields) > 0 {
			modulePath := strings.SplitN(string(fields[0]), "@", 2)[0]
			for _, candidate := range owned {
				if modulePath == candidate {
					remove = true
					break
				}
			}
		}
		if !remove {
			kept = append(kept, line)
		}
	}
	return bytes.Join(kept, []byte{'\n'})
}

func cleanStandaloneChecksums(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-clean-sums", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
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
	for _, repository := range manifest.Repositories {
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		if err := requireStandaloneDestination(destination, repository.Name); err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
	}

	return cleanStandaloneChecksumsFromManifest(*destinationRoot, manifest)
}

func cleanStandaloneChecksumsFromManifest(
	destinationRoot string,
	manifest standaloneManifest,
) error {
	repositories := make(map[string]standaloneRepository, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		repositories[repository.Name] = repository
	}
	paths := make(map[string]string, len(manifest.Modules))
	for _, item := range manifest.Modules {
		paths[item.PreviousPath] = item.Path
	}

	for _, item := range manifest.Modules {
		repository, ok := repositories[item.Repository]
		if !ok {
			return fmt.Errorf("module %s references unknown repository %s", item.Path, item.Repository)
		}
		moduleRoot := filepath.Join(destinationRoot, repository.DestinationDirectory)
		if item.Directory != "." {
			moduleRoot = filepath.Join(moduleRoot, filepath.FromSlash(item.Directory))
		}
		filename := filepath.Join(moduleRoot, "go.sum")
		contents, err := os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}
		cleaned := removeStandaloneOwnedChecksums(contents, paths)
		if bytes.Equal(cleaned, contents) {
			continue
		}
		if err := os.WriteFile(filename, cleaned, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
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
		".golib/scripts/capture-standalone-repository-audit.sh",
		".golib/scripts/extract-standalone-repository.sh",
		".golib/scripts/tidy-standalone-modules.sh",
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
		mode, err := standaloneTrackedFileMode(destination, relative)
		if err != nil {
			return err
		}
		info, err := os.Lstat(filename)
		if err == nil {
			if !info.Mode().IsRegular() {
				continue
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect tracked file %s: %w", relative, err)
		}
		command := exec.Command("git", "-C", destination, "show", "HEAD:"+relative)
		contents, err := command.Output()
		if err != nil {
			return fmt.Errorf("restore tracked file %s from extracted HEAD: %w", relative, err)
		}
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return fmt.Errorf("create tracked file directory %s: %w", relative, err)
		}
		if err := os.WriteFile(filename, contents, mode); err != nil {
			return fmt.Errorf("restore tracked file %s: %w", relative, err)
		}
		if err := os.Chmod(filename, mode); err != nil {
			return fmt.Errorf("restore tracked file mode %s: %w", relative, err)
		}
	}
	return nil
}

func standaloneTrackedFileMode(destination string, relative string) (os.FileMode, error) {
	command := exec.Command("git", "-C", destination, "ls-tree", "HEAD", "--", relative)
	output, err := command.Output()
	if err != nil {
		return 0, fmt.Errorf("inspect tracked file mode %s: %w", relative, err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 3 {
		return 0, fmt.Errorf("tracked file mode is unavailable for %s", relative)
	}
	switch fields[0] {
	case "100644":
		return 0o644, nil
	case "100755":
		return 0o755, nil
	default:
		return 0, fmt.Errorf("unsupported tracked file mode %s for %s", fields[0], relative)
	}
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

func standaloneExistingTrackedFiles(destination string) ([]string, error) {
	tracked, err := standaloneTrackedFiles(destination)
	if err != nil {
		return nil, err
	}
	existing := make([]string, 0, len(tracked))
	for _, relative := range tracked {
		if _, err := os.Lstat(filepath.Join(destination, relative)); err == nil {
			existing = append(existing, relative)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect tracked file %s: %w", relative, err)
		}
	}

	return existing, nil
}

func rewriteStandaloneRepositoryPaths(contents []byte, family string, repository string) []byte {
	prefix := "pkg/" + family
	replacements := []struct{ previous, next string }{
		{
			"/Users/brian/Developer/cline/json-schema",
			"https://github.com/faustbrian/json-schema",
		},
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

func rewriteStandaloneContents(
	contents []byte,
	paths map[string]string,
	versions map[string]string,
	goMod bool,
) []byte {
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
		version := versions[modulePath]
		if version == "" {
			version = "v1.0.0"
		}
		versionPattern := `v0\.0\.0`
		if version != "v1.0.0" {
			versionPattern = `(?:v0\.0\.0|v1\.0\.0)`
		}
		pattern := regexp.MustCompile(
			`(` + regexp.QuoteMeta(modulePath) + `\s+)` + versionPattern + `(\s|$)`,
		)
		rewritten = pattern.ReplaceAll(rewritten, []byte(`${1}`+version+`${2}`))
	}

	return rewritten
}

func standaloneCatalog(
	current catalog,
	family string,
	repository string,
	paths map[string]string,
	versions map[string]string,
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
		if item.Releasable {
			version := versions[item.Path]
			if version == "" {
				return catalog{}, fmt.Errorf("module %s has no release version", item.Path)
			}
			item.Version = strings.TrimPrefix(version, "v")
		}
		item.Purpose = string(rewriteStandaloneRepositoryPaths(
			rewriteStandaloneContents([]byte(item.Purpose), paths, versions, false),
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
