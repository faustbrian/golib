package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const standaloneInitialVersion = "v1.0.0"

var standaloneUnreleasedHeading = regexp.MustCompile(
	`(?m)^## (\[Unreleased\]|Unreleased)[ \t]*$`,
)

func prepareStandaloneReleases(root string, arguments []string) error {
	flags := flag.NewFlagSet("standalone-release-prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destinationRoot := flags.String(
		"destination-root",
		"/Users/brian/Developer/golib",
		"directory containing the standalone repositories",
	)
	releaseDate := flags.String("date", "", "release date in YYYY-MM-DD format")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if _, err := time.Parse("2006-01-02", *releaseDate); err != nil {
		return fmt.Errorf("--date must use YYYY-MM-DD: %w", err)
	}

	manifest := standaloneManifest{}
	if err := readStandaloneJSON(
		filepath.Join(root, "migration/standalone/repositories.json"),
		&manifest,
	); err != nil {
		return err
	}
	plans := make(map[string]standaloneModulePlan, len(manifest.Modules))
	for _, item := range manifest.Modules {
		plans[item.Path] = item
	}

	prepared := 0
	for _, repository := range manifest.Repositories {
		destination := filepath.Join(*destinationRoot, repository.DestinationDirectory)
		if err := requireStandaloneDestination(destination, repository.Name); err != nil {
			return err
		}
		current := catalog{}
		if err := readStandaloneJSON(filepath.Join(destination, "modules.json"), &current); err != nil {
			return err
		}
		for index := range current.Modules {
			item := &current.Modules[index]
			plan, exists := plans[item.Path]
			if !exists {
				return fmt.Errorf("%s: module is absent from migration manifest", item.Path)
			}
			if !item.Releasable {
				continue
			}
			if !plan.Releasable || plan.ReleaseTag == "" {
				return fmt.Errorf("%s: releasable module has no release tag", item.Path)
			}
			if item.Lifecycle != "pre-v1" && item.Lifecycle != "stable" {
				return fmt.Errorf("%s: unsupported lifecycle %q", item.Path, item.Lifecycle)
			}
			if item.Version != "unreleased" && item.Version != standaloneInitialVersion {
				return fmt.Errorf("%s: unsupported current version %q", item.Path, item.Version)
			}
			if err := prepareStandaloneModuleChangelog(
				destination,
				repository.Name,
				*item,
				plan.ReleaseTag,
				*releaseDate,
			); err != nil {
				return err
			}
			item.Lifecycle = "stable"
			item.Version = standaloneInitialVersion
			item.Purpose = standaloneStablePurpose(item.Path, item.Purpose)
			prepared++
		}
		if err := writeStandaloneJSON(filepath.Join(destination, "modules.json"), current); err != nil {
			return err
		}
	}
	if prepared != manifest.Counts.ReleasableModules {
		return fmt.Errorf(
			"prepared %d releasable modules, want %d",
			prepared,
			manifest.Counts.ReleasableModules,
		)
	}

	return nil
}

func standaloneStablePurpose(modulePath string, purpose string) string {
	switch modulePath {
	case "github.com/faustbrian/go-kafka":
		return "`kafka` is the stable v1 bounded first-party Apache Kafka " +
			"client policy for Go services."
	case "github.com/faustbrian/go-merkle-tree":
		return strings.Replace(purpose, "current pre-v1 surface", "stable v1 surface", 1)
	default:
		return purpose
	}
}

func prepareStandaloneModuleChangelog(
	destination string,
	repository string,
	item module,
	releaseTag string,
	releaseDate string,
) error {
	moduleRoot := destination
	if item.Directory != "." {
		moduleRoot = filepath.Join(destination, filepath.FromSlash(item.Directory))
	}
	filename := filepath.Join(moduleRoot, "CHANGELOG.md")
	contents, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read changelog for %s: %w", item.Path, err)
	}
	rewritten, err := prepareStandaloneChangelog(
		contents,
		repository,
		releaseTag,
		releaseDate,
	)
	if err != nil {
		return fmt.Errorf("prepare changelog for %s: %w", item.Path, err)
	}
	if bytes.Equal(contents, rewritten) {
		return nil
	}
	if err := os.WriteFile(filename, rewritten, 0o644); err != nil {
		return fmt.Errorf("write changelog for %s: %w", item.Path, err)
	}
	return nil
}

func prepareStandaloneChangelog(
	contents []byte,
	repository string,
	releaseTag string,
	releaseDate string,
) ([]byte, error) {
	text := strings.TrimRight(string(contents), "\n")
	match := standaloneUnreleasedHeading.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, errors.New("missing Unreleased section")
	}
	bracketed := text[match[2]:match[3]] == "[Unreleased]"
	releaseVersion := strings.TrimPrefix(standaloneInitialVersion, "v")
	releaseHeading := "## " + releaseVersion + " - " + releaseDate
	if bracketed {
		releaseHeading = "## [" + releaseVersion + "] - " + releaseDate
	}
	if existing := regexp.MustCompile(
		`(?m)^## \[?1\.0\.0\]? - ([0-9]{4}-[0-9]{2}-[0-9]{2})[ \t]*$`,
	).FindStringSubmatch(text); existing != nil {
		if existing[1] != releaseDate {
			return nil, fmt.Errorf(
				"existing v1.0.0 release date %s differs from %s",
				existing[1],
				releaseDate,
			)
		}
		return updateStandaloneChangelogLinks(
			[]byte(text+"\n"),
			repository,
			releaseTag,
			bracketed,
		), nil
	}

	insertAt := match[1]
	text = text[:insertAt] + "\n\n" + releaseHeading + text[insertAt:]
	return updateStandaloneChangelogLinks(
		[]byte(text+"\n"),
		repository,
		releaseTag,
		bracketed,
	), nil
}

func updateStandaloneChangelogLinks(
	contents []byte,
	repository string,
	releaseTag string,
	bracketed bool,
) []byte {
	if !bracketed {
		return contents
	}
	base := "https://github.com/faustbrian/" + repository
	text := strings.TrimRight(string(contents), "\n")
	unreleased := "[Unreleased]: " + base + "/compare/" +
		url.PathEscape(releaseTag) + "...HEAD"
	unreleasedLink := regexp.MustCompile(`(?m)^\[Unreleased\]:[^\n]*$`)
	if unreleasedLink.MatchString(text) {
		text = unreleasedLink.ReplaceAllString(text, unreleased)
	} else {
		text += "\n\n" + unreleased
	}
	releaseLink := "[1.0.0]: " + base + "/releases/tag/" +
		url.PathEscape(releaseTag)
	versionLink := regexp.MustCompile(`(?m)^\[1\.0\.0\]:[^\n]*$`)
	if versionLink.MatchString(text) {
		text = versionLink.ReplaceAllString(text, releaseLink)
	} else {
		text += "\n" + releaseLink
	}
	return []byte(text + "\n")
}
