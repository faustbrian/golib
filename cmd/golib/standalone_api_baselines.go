package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const standaloneLegacyModulePrefix = "github.com/faustbrian/golib/pkg/"

type standaloneAPIBaselineGenerator func(moduleRoot, target, output string) error

func refreshStandaloneAPIBaselines(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-api-baselines", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
	apidiff := flags.String("apidiff", "", "path to the pinned apidiff executable")
	check := flags.Bool("check", false, "report stale baselines without changing them")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if !*check && *apidiff == "" {
		return fmt.Errorf("--apidiff is required unless --check is used")
	}

	manifest := standaloneManifest{}
	if err := readStandaloneJSON(
		filepath.Join(root, "migration/standalone/repositories.json"),
		&manifest,
	); err != nil {
		return err
	}
	paths := make(map[string]string, len(manifest.Modules))
	moduleRoots := make(map[string]string, len(manifest.Modules))
	repositoryDirectories := make(map[string]string, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		repositoryDirectories[repository.Name] = repository.DestinationDirectory
	}
	for _, item := range manifest.Modules {
		paths[item.PreviousPath] = item.Path
		moduleRoots[item.Path] = filepath.Join(
			*destinationRoot,
			repositoryDirectories[item.Repository],
			item.Directory,
		)
	}

	stale := make([]string, 0)
	for _, repository := range manifest.Repositories {
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		files, err := staleStandaloneAPIBaselines(destination)
		if err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
		stale = append(stale, files...)
	}
	sort.Strings(stale)
	if *check {
		if len(stale) != 0 {
			return fmt.Errorf("%d standalone API baselines retain monorepo identities", len(stale))
		}
		fmt.Println("standalone API baselines are canonical")
		return nil
	}

	generator := commandStandaloneAPIBaselineGenerator(*apidiff)
	for _, filename := range stale {
		if err := refreshStandaloneAPIBaseline(
			filename,
			paths,
			moduleRoots,
			generator,
		); err != nil {
			return err
		}
	}
	fmt.Printf("refreshed %d standalone API baselines\n", len(stale))

	return nil
}

func staleStandaloneAPIBaselines(destination string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(destination, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if filename != destination && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		contents, readErr := io.ReadAll(io.LimitReader(file, 4096))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if _, ok := standaloneAPIBaselineTarget(contents); ok {
			files = append(files, filename)
		}
		return nil
	})

	return files, err
}

func standaloneAPIBaselineTarget(contents []byte) (string, bool) {
	newline := bytes.IndexByte(contents, '\n')
	if newline < 1 || bytes.IndexByte(contents, 0) < 0 {
		return "", false
	}
	target := string(contents[:newline])
	if !strings.HasPrefix(target, standaloneLegacyModulePrefix) {
		return "", false
	}

	return target, true
}

func refreshStandaloneAPIBaseline(
	filename string,
	paths map[string]string,
	moduleRoots map[string]string,
	generate standaloneAPIBaselineGenerator,
) error {
	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read API baseline %s: %w", filename, err)
	}
	previous, ok := standaloneAPIBaselineTarget(contents)
	if !ok {
		return nil
	}
	target, ok := rewriteStandaloneAPITarget(previous, paths)
	if !ok {
		return fmt.Errorf("API baseline %s has no canonical path for %s", filename, previous)
	}
	target, moduleRoot, ok := resolveStandaloneAPIBaseline(filename, target, moduleRoots)
	if !ok {
		return fmt.Errorf("API baseline %s has no owning module for %s", filename, target)
	}
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("inspect API baseline %s: %w", filename, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".golib-api-baseline-*")
	if err != nil {
		return fmt.Errorf("create temporary API baseline for %s: %w", filename, err)
	}
	temporaryName := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return fmt.Errorf("close temporary API baseline for %s: %w", filename, err)
	}
	defer os.Remove(temporaryName)
	if err := generate(moduleRoot, target, temporaryName); err != nil {
		return fmt.Errorf("generate API baseline %s: %w", filename, err)
	}
	generated, err := os.ReadFile(temporaryName)
	if err != nil {
		return fmt.Errorf("read generated API baseline %s: %w", filename, err)
	}
	generatedTarget, valid := binaryAPIBaselineTarget(generated)
	if !valid || generatedTarget != target {
		return fmt.Errorf(
			"generated API baseline %s has target %q, want %q",
			filename,
			generatedTarget,
			target,
		)
	}
	if err := os.Chmod(temporaryName, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set API baseline mode %s: %w", filename, err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace API baseline %s: %w", filename, err)
	}

	return nil
}

func rewriteStandaloneAPITarget(target string, paths map[string]string) (string, bool) {
	previousPaths := make([]string, 0, len(paths))
	for previous := range paths {
		previousPaths = append(previousPaths, previous)
	}
	sort.Slice(previousPaths, func(left, right int) bool {
		return len(previousPaths[left]) > len(previousPaths[right])
	})
	for _, previous := range previousPaths {
		if target == previous {
			return paths[previous], true
		}
		if strings.HasPrefix(target, previous+"/") {
			return paths[previous] + strings.TrimPrefix(target, previous), true
		}
	}

	return "", false
}

func resolveStandaloneAPIBaseline(
	filename string,
	target string,
	moduleRoots map[string]string,
) (string, string, bool) {
	modulePaths := make([]string, 0, len(moduleRoots))
	for modulePath := range moduleRoots {
		modulePaths = append(modulePaths, modulePath)
	}
	sort.Slice(modulePaths, func(left, right int) bool {
		return len(modulePaths[left]) > len(modulePaths[right])
	})
	for _, modulePath := range modulePaths {
		if target == modulePath {
			return target, moduleRoots[modulePath], true
		}
		if strings.HasPrefix(target, modulePath+"/") {
			relative := strings.TrimPrefix(target, modulePath+"/")
			if info, err := os.Stat(filepath.Join(moduleRoots[modulePath], filepath.FromSlash(relative))); err == nil && info.IsDir() {
				return target, moduleRoots[modulePath], true
			}
		}
	}

	physicalModules := append([]string(nil), modulePaths...)
	sort.Slice(physicalModules, func(left, right int) bool {
		return len(moduleRoots[physicalModules[left]]) > len(moduleRoots[physicalModules[right]])
	})
	for _, modulePath := range physicalModules {
		relative, err := filepath.Rel(moduleRoots[modulePath], filename)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return modulePath, moduleRoots[modulePath], true
		}
	}

	return "", "", false
}

func binaryAPIBaselineTarget(contents []byte) (string, bool) {
	newline := bytes.IndexByte(contents, '\n')
	if newline < 1 || bytes.IndexByte(contents, 0) < 0 {
		return "", false
	}

	return string(contents[:newline]), true
}

func commandStandaloneAPIBaselineGenerator(apidiff string) standaloneAPIBaselineGenerator {
	return func(moduleRoot, target, output string) error {
		command := exec.Command(apidiff, "-m", "-w", output, target)
		command.Dir = moduleRoot
		command.Env = append(os.Environ(), "GOWORK=off")
		combined, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("apidiff %s: %w: %s", target, err, strings.TrimSpace(string(combined)))
		}

		return nil
	}
}
