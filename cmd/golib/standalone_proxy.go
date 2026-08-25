package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func buildStandaloneProxy(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-proxy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing standalone repositories",
	)
	output := flags.String("output", "", "empty output directory for the file proxy")
	throughWave := flags.Int(
		"through-wave",
		-1,
		"include release waves through this one-based wave number; -1 includes all waves",
	)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	if err := requireEmptyStandaloneDirectory(*output); err != nil {
		return err
	}

	manifest := standaloneManifest{}
	if err := readStandaloneJSON(
		filepath.Join(root, "migration/standalone/repositories.json"),
		&manifest,
	); err != nil {
		return err
	}
	repositories := make(map[string]standaloneRepository, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		repositories[repository.Name] = repository
	}

	modules, err := standaloneProxyModules(manifest, *throughWave)
	if err != nil {
		return err
	}
	for _, item := range modules {
		repository, ok := repositories[item.Repository]
		if !ok {
			return fmt.Errorf("module %s references unknown repository %s", item.Path, item.Repository)
		}
		repositoryRoot := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		if err := writeStandaloneProxyModule(
			*output,
			repositoryRoot,
			item,
			manifest.Modules,
		); err != nil {
			return err
		}
	}

	return nil
}

func standaloneProxyModules(
	manifest standaloneManifest,
	throughWave int,
) ([]standaloneModulePlan, error) {
	if throughWave < -1 || throughWave > len(manifest.ReleaseWaves) {
		return nil, fmt.Errorf(
			"--through-wave must be between 0 and %d, or -1 for all waves",
			len(manifest.ReleaseWaves),
		)
	}

	included := make(map[string]struct{})
	if throughWave == -1 {
		for _, item := range manifest.Modules {
			if item.Releasable {
				included[item.Path] = struct{}{}
			}
		}
	} else {
		for _, wave := range manifest.ReleaseWaves[:throughWave] {
			for _, modulePath := range wave {
				included[modulePath] = struct{}{}
			}
		}
	}

	modules := make([]standaloneModulePlan, 0, len(included))
	for _, item := range manifest.Modules {
		if _, ok := included[item.Path]; ok {
			modules = append(modules, item)
		}
	}

	return modules, nil
}

func requireEmptyStandaloneDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create proxy output: %w", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read proxy output: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("proxy output must be empty: %s", directory)
	}
	return nil
}

func writeStandaloneProxyModule(
	output string,
	repositoryRoot string,
	item standaloneModulePlan,
	modules []standaloneModulePlan,
) error {
	moduleRoot := repositoryRoot
	if item.Directory != "." {
		moduleRoot = filepath.Join(repositoryRoot, filepath.FromSlash(item.Directory))
	}
	goMod, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return fmt.Errorf("read %s go.mod: %w", item.Path, err)
	}
	moduleDirectory := filepath.Join(output, filepath.FromSlash(item.Path), "@v")
	releaseVersion := item.ReleaseVersion
	if releaseVersion == "" {
		releaseVersion = "v1.0.0"
	}
	if err := os.MkdirAll(moduleDirectory, 0o755); err != nil {
		return fmt.Errorf("create proxy directory for %s: %w", item.Path, err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, releaseVersion+".mod"),
		goMod,
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy go.mod for %s: %w", item.Path, err)
	}
	info, err := json.Marshal(struct {
		Version string    `json:"Version"`
		Time    time.Time `json:"Time"`
	}{releaseVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		return fmt.Errorf("encode proxy info for %s: %w", item.Path, err)
	}
	info = append(info, '\n')
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, releaseVersion+".info"),
		info,
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy info for %s: %w", item.Path, err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, "list"),
		[]byte(releaseVersion+"\n"),
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy list for %s: %w", item.Path, err)
	}

	archive, err := standaloneModuleArchive(moduleRoot, item, modules)
	if err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, releaseVersion+".zip"),
		archive,
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy archive for %s: %w", item.Path, err)
	}
	return nil
}

func standaloneModuleArchive(
	moduleRoot string,
	item standaloneModulePlan,
	modules []standaloneModulePlan,
) ([]byte, error) {
	nested := make([]string, 0)
	for _, candidate := range modules {
		if candidate.Repository != item.Repository || candidate.Directory == item.Directory {
			continue
		}
		if item.Directory == "." {
			nested = append(nested, filepath.FromSlash(candidate.Directory))
		} else if strings.HasPrefix(candidate.Directory, item.Directory+"/") {
			nested = append(
				nested,
				filepath.FromSlash(strings.TrimPrefix(candidate.Directory, item.Directory+"/")),
			)
		}
	}
	sort.Strings(nested)

	command := exec.Command("git", "ls-files", "-z", "--cached", "--", ".")
	command.Dir = moduleRoot
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inventory tracked module files %s: %w", item.Path, err)
	}
	files := make([]string, 0)
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		relative := filepath.Clean(string(raw))
		if relative == ".golib" || strings.HasPrefix(relative, ".golib"+string(filepath.Separator)) ||
			relative == ".artifacts" ||
			strings.HasPrefix(relative, ".artifacts"+string(filepath.Separator)) ||
			standalonePathWithin(relative, nested) {
			continue
		}
		info, err := os.Lstat(filepath.Join(moduleRoot, relative))
		if err != nil {
			return nil, fmt.Errorf("inspect tracked archive file %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, relative)
	}
	sort.Strings(files)

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	releaseVersion := item.ReleaseVersion
	if releaseVersion == "" {
		releaseVersion = "v1.0.0"
	}
	prefix := item.Path + "@" + releaseVersion + "/"
	for _, relative := range files {
		contents, err := os.ReadFile(filepath.Join(moduleRoot, relative))
		if err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("read archive file %s: %w", relative, err)
		}
		header := &zip.FileHeader{
			Name:   prefix + filepath.ToSlash(relative),
			Method: zip.Deflate,
		}
		header.SetModTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0o644)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("create archive entry %s: %w", relative, err)
		}
		if _, err := writer.Write(contents); err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("write archive entry %s: %w", relative, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close module archive %s: %w", item.Path, err)
	}
	return buffer.Bytes(), nil
}

func standalonePathWithin(relative string, directories []string) bool {
	for _, directory := range directories {
		if relative == directory || strings.HasPrefix(relative, directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
