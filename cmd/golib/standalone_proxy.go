package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const standaloneReleaseVersion = "v1.0.0"

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
	if err := os.MkdirAll(moduleDirectory, 0o755); err != nil {
		return fmt.Errorf("create proxy directory for %s: %w", item.Path, err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, standaloneReleaseVersion+".mod"),
		goMod,
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy go.mod for %s: %w", item.Path, err)
	}
	info, err := json.Marshal(struct {
		Version string    `json:"Version"`
		Time    time.Time `json:"Time"`
	}{standaloneReleaseVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		return fmt.Errorf("encode proxy info for %s: %w", item.Path, err)
	}
	info = append(info, '\n')
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, standaloneReleaseVersion+".info"),
		info,
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy info for %s: %w", item.Path, err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, "list"),
		[]byte(standaloneReleaseVersion+"\n"),
		0o644,
	); err != nil {
		return fmt.Errorf("write proxy list for %s: %w", item.Path, err)
	}

	archive, err := standaloneModuleArchive(moduleRoot, item, modules)
	if err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(moduleDirectory, standaloneReleaseVersion+".zip"),
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

	files := make([]string, 0)
	err := filepath.WalkDir(moduleRoot, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(moduleRoot, filename)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() {
			if relative == ".git" || relative == ".artifacts" || relative == ".golib" ||
				standalonePathWithin(relative, nested) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inventory module archive %s: %w", item.Path, err)
	}
	sort.Strings(files)

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	prefix := item.Path + "@" + standaloneReleaseVersion + "/"
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
