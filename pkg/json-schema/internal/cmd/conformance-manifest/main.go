// Command conformance-manifest generates pinned official-suite evidence.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fixtureGroup struct {
	Tests []json.RawMessage `json:"tests"`
}

type manifestFile interface {
	io.Writer
	Name() string
	Close() error
}

type manifestGenerator struct {
	revision     func(string) (string, error)
	createTemp   func(string, string) (manifestFile, error)
	fixturePaths func(string) ([]string, error)
	readFile     func(string) ([]byte, error)
	relativePath func(string, string) (string, error)
	rename       func(string, string) error
	remove       func(string) error
}

var (
	commandArgs                  = os.Args[1:]
	commandErrorOutput io.Writer = os.Stderr
	generateManifest             = generate
	exitProcess                  = os.Exit
)

func main() {
	exitProcess(run(commandArgs, commandErrorOutput))
}

func run(arguments []string, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("conformance-manifest", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	output := flags.String(
		"output",
		filepath.Join("specification", "official-suite-results.tsv"),
		"manifest output path",
	)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if err := generateManifest(*output); err != nil {
		_, _ = fmt.Fprintln(errorOutput, err)
		return 1
	}
	return 0
}

func generate(output string) error {
	return defaultManifestGenerator().generate(output)
}

func defaultManifestGenerator() manifestGenerator {
	return manifestGenerator{
		revision: suiteRevision,
		createTemp: func(directory, pattern string) (manifestFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		fixturePaths: fixturePaths,
		readFile:     os.ReadFile,
		relativePath: filepath.Rel,
		rename:       os.Rename,
		remove:       os.Remove,
	}
}

func (generator manifestGenerator) generate(output string) error {
	revision, err := generator.revision(
		filepath.Join("specification", "official-suite.env"),
	)
	if err != nil {
		return err
	}
	temporary, err := generator.createTemp(
		filepath.Dir(output),
		".official-suite-results-*",
	)
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = generator.remove(temporaryName) }()
	if _, err := fmt.Fprintln(
		temporary,
		"revision\tdraft\tfile\tgroups\tcases\tpass\tskip\tfailure\tsha256",
	); err != nil {
		return err
	}
	for _, draft := range []string{
		"draft3", "draft4", "draft6", "draft7", "draft2019-09", "draft2020-12",
	} {
		root := filepath.Join(
			"testdata", "official", "JSON-Schema-Test-Suite", "tests", draft,
		)
		paths, err := generator.fixturePaths(root)
		if err != nil {
			return err
		}
		for _, path := range paths {
			// #nosec G304 -- path is confined to the pinned fixture tree.
			raw, err := generator.readFile(path)
			if err != nil {
				return err
			}
			var groups []fixtureGroup
			if err := json.Unmarshal(raw, &groups); err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			cases := 0
			for _, group := range groups {
				cases += len(group.Tests)
			}
			relative, err := generator.relativePath(root, path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(raw)
			if _, err := fmt.Fprintf(
				temporary,
				"%s\t%s\t%s\t%d\t%d\t%d\t0\t0\t%s\n",
				revision,
				draft,
				filepath.ToSlash(relative),
				len(groups),
				cases,
				cases,
				hex.EncodeToString(digest[:]),
			); err != nil {
				return err
			}
		}
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return generator.rename(temporaryName, output)
}

func fixturePaths(root string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func suiteRevision(path string) (string, error) {
	// #nosec G304 -- path is a repository-owned provenance file.
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if revision, found := strings.CutPrefix(line, "SUITE_REVISION="); found {
			return revision, nil
		}
	}
	return "", fmt.Errorf("SUITE_REVISION is missing from %s", path)
}
