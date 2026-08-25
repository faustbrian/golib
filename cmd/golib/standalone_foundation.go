package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type standaloneMutationHistory struct {
	SchemaVersion           int              `json:"schema_version"`
	Reason                  string           `json:"reason"`
	VerifierMigrationReview json.RawMessage  `json:"verifier_migration_review"`
	VerifierMigrations      []map[string]any `json:"verifier_migrations"`
	Entries                 []map[string]any `json:"entries"`
}

type standaloneMutationZeroInventory struct {
	SchemaVersion int              `json:"schema_version"`
	Packages      []map[string]any `json:"packages"`
}

var standaloneFoundationFiles = []string{
	".gitattributes",
	".gitignore",
	".gitleaks.toml",
	".go-version",
	"AGENTS.md",
	"CLAUDE.md",
	"CODE_OF_CONDUCT.md",
	"COMPATIBILITY.md",
	"CONTRIBUTING.md",
	"DEPRECATION.md",
	"SECURITY.md",
	"SUPPORT.md",
	"cspell.json",
	"package-lock.json",
	"package.json",
	".github/pull_request_template.md",
	".golib/documentation-tools.env",
	".golib/versions.env",
}

func installStandaloneFoundation(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	paths map[string]string,
) error {
	packageMakefile := filepath.Join(destination, "Makefile")
	preservedMakefile := filepath.Join(destination, ".golib/package.mk")
	if _, err := os.Stat(preservedMakefile); os.IsNotExist(err) {
		contents, readErr := os.ReadFile(packageMakefile)
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("read package Makefile: %w", readErr)
		}
		if readErr == nil {
			contents = rewriteStandaloneContents(contents, paths, false)
			contents = rewriteStandaloneRepositoryPaths(
				contents,
				repository.Family,
				repository.Name,
			)
			if err := os.MkdirAll(filepath.Join(destination, ".golib"), 0o755); err != nil {
				return fmt.Errorf("create tooling directory: %w", err)
			}
			if err := os.WriteFile(preservedMakefile, contents, 0o644); err != nil {
				return fmt.Errorf("preserve package Makefile: %w", err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("inspect preserved package Makefile: %w", err)
	}

	for _, relative := range standaloneFoundationFiles {
		if relative == "SECURITY.md" {
			if _, err := os.Stat(filepath.Join(destination, relative)); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect package security policy: %w", err)
			}
		}
		if err := copyStandaloneFoundationFile(
			sourceRoot,
			destination,
			relative,
			repository,
			paths,
		); err != nil {
			return err
		}
	}
	if err := copyStandaloneScripts(sourceRoot, destination, repository, paths); err != nil {
		return err
	}
	if err := writeStandaloneMutationPolicies(
		sourceRoot,
		destination,
		"pkg/"+repository.Family,
	); err != nil {
		return err
	}

	generated := map[string]string{
		"Makefile":                                   standaloneMakefile,
		".golangci.yml":                              standaloneGolangCILint,
		".github/dependabot.yml":                     standaloneDependabot,
		".github/workflows/ci.yml":                   standaloneCIWorkflow,
		".golib/scripts/run-modules.sh":              standaloneRunModules,
		".golib/scripts/check-go-safety.sh":          standaloneSafetyScript,
		".golib/scripts/repository-check.sh":         standaloneRepositoryCheck,
		".golib/scripts/release.sh":                  standaloneReleaseScript,
		".golib/scripts/with-disposable-go-cache.sh": standaloneDisposableCache,
	}
	for relative, template := range generated {
		contents := strings.ReplaceAll(template, "{{REPOSITORY}}", repository.Name)
		filename := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return fmt.Errorf("create %s parent: %w", relative, err)
		}
		mode := fs.FileMode(0o644)
		if strings.HasPrefix(relative, ".golib/scripts/") {
			mode = 0o755
		}
		if err := os.WriteFile(filename, []byte(contents), mode); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}

	return nil
}

func writeStandaloneMutationPolicies(sourceRoot string, destination string, prefix string) error {
	history := standaloneMutationHistory{}
	if err := readStandaloneJSON(
		filepath.Join(sourceRoot, ".golib/mutation-history-migrations.json"),
		&history,
	); err != nil {
		return err
	}
	history.VerifierMigrations = filterStandaloneMutationRecords(
		history.VerifierMigrations,
		prefix,
		"module",
	)
	history.Entries = filterStandaloneMutationRecords(history.Entries, prefix, "module")
	if err := writeStandaloneJSON(
		filepath.Join(destination, ".golib/mutation-history-migrations.json"),
		history,
	); err != nil {
		return err
	}

	zero := standaloneMutationZeroInventory{}
	if err := readStandaloneJSON(
		filepath.Join(sourceRoot, ".golib/mutation-zero-inventory.json"),
		&zero,
	); err != nil {
		return err
	}
	zero.Packages = filterStandaloneMutationRecords(
		zero.Packages,
		prefix,
		"module_directory",
	)
	return writeStandaloneJSON(
		filepath.Join(destination, ".golib/mutation-zero-inventory.json"),
		zero,
	)
}

func filterStandaloneMutationRecords(
	records []map[string]any,
	prefix string,
	field string,
) []map[string]any {
	result := make([]map[string]any, 0)
	for _, record := range records {
		value, ok := record[field].(string)
		if !ok || (value != prefix && !strings.HasPrefix(value, prefix+"/")) {
			continue
		}
		record[field] = rebaseStandalonePath(value, prefix)
		result = append(result, record)
	}
	return result
}

func copyStandaloneScripts(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	paths map[string]string,
) error {
	source := filepath.Join(sourceRoot, "scripts")
	return filepath.WalkDir(source, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		sourceRelative, err := filepath.Rel(sourceRoot, filename)
		if err != nil {
			return err
		}
		if sourceRelative == "scripts" {
			return nil
		}
		destinationRelative := filepath.Join(".golib", sourceRelative)
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(destination, destinationRelative), 0o755)
		}
		if sourceRelative == "scripts/capture-standalone-repository-audit.sh" ||
			sourceRelative == "scripts/extract-standalone-repository.sh" {
			return nil
		}
		return copyStandaloneFoundationFileAs(
			sourceRoot,
			destination,
			sourceRelative,
			destinationRelative,
			repository,
			paths,
		)
	})
}

func copyStandaloneFoundationFile(
	sourceRoot string,
	destination string,
	relative string,
	repository standaloneRepository,
	paths map[string]string,
) error {
	return copyStandaloneFoundationFileAs(
		sourceRoot,
		destination,
		relative,
		relative,
		repository,
		paths,
	)
}

func copyStandaloneFoundationFileAs(
	sourceRoot string,
	destination string,
	sourceRelative string,
	destinationRelative string,
	repository standaloneRepository,
	paths map[string]string,
) error {
	source := filepath.Join(sourceRoot, sourceRelative)
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect foundation file %s: %w", sourceRelative, err)
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read foundation file %s: %w", sourceRelative, err)
	}
	if bytesContainNUL(contents) {
		return fmt.Errorf("foundation file is unexpectedly binary: %s", sourceRelative)
	}
	contents = rewriteStandaloneContents(contents, paths, false)
	contents = rewriteStandaloneRepositoryPaths(
		contents,
		repository.Family,
		repository.Name,
	)
	contents = rewriteStandaloneTooling(
		contents,
		destinationRelative,
		standaloneModulePrefix+repository.Name,
	)
	if sourceRelative == "AGENTS.md" {
		contents = rewriteStandaloneAgentPolicy(contents)
	}
	filename := filepath.Join(destination, destinationRelative)
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("create foundation directory for %s: %w", destinationRelative, err)
	}
	if err := os.WriteFile(filename, contents, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write foundation file %s: %w", destinationRelative, err)
	}
	return nil
}

func rewriteStandaloneTooling(contents []byte, relative string, modulePath string) []byte {
	toolingRelative := strings.TrimPrefix(relative, ".golib/")
	if !strings.HasPrefix(toolingRelative, "scripts/") {
		return contents
	}
	type replacement struct{ previous, next string }
	replacements := make([]replacement, 0, 7)
	if toolingRelative == "scripts/check-module.sh" {
		replacements = append(replacements,
			replacement{
				`"${GOLIB_LOCAL_PROXY}" v0.0.0`,
				`"${GOLIB_LOCAL_PROXY}" v1.0.0`,
			},
			replacement{
				`if [[ "${module_path}" != "github.com/faustbrian/golib" &&
                "${module_path}" != github.com/faustbrian/golib/* ]]; then`,
				`if [[ "${module_path}" != "` + modulePath + `" &&
                "${module_path}" != ` + modulePath + `/* ]]; then`,
			},
			replacement{
				`--ignore "github.com/faustbrian/golib"`,
				`--ignore "` + modulePath + `"`,
			},
			replacement{
				`make_has_target() {
    [[ -f Makefile ]] && grep -Eq "^$1([[:space:]]+[^:]*)?:" Makefile
}

find_make_target() {
    local target
    if [[ "${module}" == "." ]]; then
        return 1
    fi`,
				`package_makefile="Makefile"
if [[ "${module}" == "." && -f "${root}/.golib/package.mk" ]]; then
    package_makefile="${root}/.golib/package.mk"
fi

package_make() {
    make -f "${package_makefile}" "$@"
}

make_has_target() {
    [[ -f "${package_makefile}" ]] &&
        grep -Eq "^$1([[:space:]]+[^:]*)?:" "${package_makefile}"
}

find_make_target() {
    local target`,
			},
			replacement{
				`if [[ "${module}" == "." ]]; then
                GOWORK=off "${root}/scripts/check-documentation.sh"
            elif target="$(find_make_target docs documentation)"; then
                make "${target}"`,
				`if target="$(find_make_target docs documentation)"; then
                make "${target}"
            elif [[ "${module}" == "." ]]; then
                GOWORK=off "${root}/scripts/check-documentation.sh"`,
			},
		)
	}
	if toolingRelative == "scripts/build-local-proxy.sh" {
		replacements = append(replacements, replacement{
			`version="${2:-v0.0.0}"`,
			`version="${2:-v1.0.0}"`,
		})
	}
	replacements = append(replacements,
		replacement{
			`${root}/scripts/`,
			`${root}/.golib/scripts/`,
		},
		replacement{
			"github.com/faustbrian/golib/*",
			"github.com/faustbrian/go-*",
		},
		replacement{
			`github\.com/faustbrian/golib/pkg/`,
			`github\.com/faustbrian/go-`,
		},
	)
	for _, item := range replacements {
		contents = []byte(strings.ReplaceAll(string(contents), item.previous, item.next))
	}
	if toolingRelative == "scripts/check-module.sh" {
		contents = []byte(strings.ReplaceAll(
			string(contents),
			`make GOWORK=off "${target}"`,
			`package_make GOWORK=off "${target}"`,
		))
		contents = []byte(strings.ReplaceAll(
			string(contents),
			`make "${target}"`,
			`package_make "${target}"`,
		))
		contents = []byte(strings.ReplaceAll(
			string(contents),
			"                make \\\n",
			"                package_make \\\n",
		))
	}
	if toolingRelative == "scripts/check-documentation.sh" {
		contents = []byte(strings.ReplaceAll(
			string(contents),
			"go run ./cmd/golib documentation\n",
			"test -s README.md\n",
		))
	}
	return contents
}

func bytesContainNUL(contents []byte) bool {
	for _, value := range contents {
		if value == 0 {
			return true
		}
	}
	return false
}

func rewriteStandaloneAgentPolicy(contents []byte) []byte {
	replacements := map[string]string{
		"- Public modules MUST live under `pkg/<library>`.\n":                                                   "- The public root module MUST live at the repository root.\n- Intentional optional or test modules MAY live in explicit nested directories.\n",
		"- Public module paths MUST match their directory exactly beneath\n  `github.com/faustbrian/golib/`.\n": "- Public module paths MUST match their repository-relative directories beneath\n  the module path declared by the root `go.mod`.\n",
		"- `make check MODULES=pkg/<library>` runs the exact module contract.\n":                                "- `make check` runs the exact contract for every repository module.\n",
		"- `make ci-changed BASE=<revision>` includes changed modules and reverse\n  dependants.\n":             "",
	}
	text := string(contents)
	for previous, next := range replacements {
		text = strings.ReplaceAll(text, previous, next)
	}
	return []byte(text)
}

const standaloneMakefile = `SHELL := /usr/bin/env bash

.PHONY: check ci inventory repository-check

check:
	./scripts/with-disposable-go-cache.sh ./scripts/run-modules.sh check --all

ci: repository-check check

inventory repository-check:
	./scripts/repository-check.sh
`

const standaloneGolangCILint = `version: "2"

run:
  timeout: 10m

linters:
  default: standard
  enable:
    - bodyclose
    - errorlint
    - exhaustive
    - gocritic
    - gosec
    - nilerr
    - noctx
    - prealloc
    - revive
    - rowserrcheck
    - sqlclosecheck
    - testifylint
    - unconvert
    - unparam
    - wastedassign

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
`

const standaloneDependabot = `version: 2
updates:
  - package-ecosystem: gomod
    directories:
      - "/"
      - "/**"
    schedule:
      interval: weekly
    groups:
      go-dependencies:
        group-by: dependency-name
  - package-ecosystem: github-actions
    directory: "/"
    schedule:
      interval: weekly
    groups:
      github-actions:
        patterns:
          - "*"
`

const standaloneRunModules = `#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
    printf 'usage: %s <gate> <--all|--modules LIST>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
gate="$1"
shift
case "$1" in
    --all)
        selection="$(jq -r '.modules[].directory' "${root}/modules.json")"
        ;;
    --modules)
        [[ $# -eq 2 ]] || exit 2
        selection="${2//,/\\n}"
        ;;
    *)
        printf 'unknown module selection: %s\n' "$1" >&2
        exit 2
        ;;
esac

while IFS= read -r module; do
    [[ -n "${module}" ]] || continue
    (
        task="$(mktemp -d "${TMPDIR:-/tmp}/golib-services.XXXXXX")"
        environment="${task}/environment"
        state="${task}/state"
        # shellcheck disable=SC2329 # Invoked by the subshell EXIT trap.
        cleanup() {
            "${root}/.golib/scripts/stop-services.sh" "${state}" || true
            find "${task}" -depth -delete 2>/dev/null || true
        }
        trap cleanup EXIT HUP INT TERM
        "${root}/.golib/scripts/start-services.sh" "${module}" "${environment}" "${state}"
        set -a
        # shellcheck source=/dev/null
        source "${environment}"
        set +a
        "${root}/.golib/scripts/check-module.sh" "${module}" "${gate}"
    )
done <<<"${selection}"
`

const standaloneSafetyScript = `#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
directory="${root}/${module}"

if rg -n --glob '*.go' --glob '!**/*_test.go' \
    '(^|[^[:alnum:]_])(unsafe\.|os\.Exit\(|log\.Fatal|http\.DefaultClient)' \
    "${directory}"; then
    printf 'forbidden unsafe or process-global production API detected\n' >&2
    exit 1
fi
printf 'standalone safety policy passed for %s\n' "${module}"
`

const standaloneRepositoryCheck = `#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
repository="github.com/faustbrian/{{REPOSITORY}}"

jq -e --arg repository "${repository}" '
    .repository == $repository and
    (.modules | length > 0) and
    all(.modules[];
        (.directory == "." or (.directory | startswith("/") | not)) and
        (
            .releasable == false or
            .module_path == $repository or
            (.module_path | startswith($repository + "/"))
        )
    )
' "${root}/modules.json" >/dev/null

while IFS= read -r module; do
    directory="$(jq -r --arg module "${module}" \
        '.modules[] | select(.module_path == $module) | .directory' \
        "${root}/modules.json")"
    [[ "$(sed -n 's/^module[[:space:]]\+//p' "${root}/${directory}/go.mod")" == "${module}" ]]
    if grep -Eq '^[[:space:]]*replace([[:space:]]|$)' "${root}/${directory}/go.mod"; then
        printf 'committed replace directive in %s\n' "${directory}/go.mod" >&2
        exit 1
    fi
done < <(jq -r '.modules[].module_path' "${root}/modules.json")

if rg -n 'github\.com/faustbrian/golib/pkg|/Users/[^/]+/Developer|\.\./go-' \
    --glob '!go.sum' --glob '!CHANGELOG.md' \
    --glob '!.golib/scripts/repository-check.sh' "${root}"; then
    printf 'monorepo or sibling-checkout reference remains\n' >&2
    exit 1
fi

git diff --check
printf 'standalone repository contract passed\n'
`

const standaloneDisposableCache = `#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
    printf 'usage: %s <command> [arguments...]\n' "$0" >&2
    exit 2
fi

gocache="$(mktemp -d "${TMPDIR:-/tmp}/golib-gocache.XXXXXX")"
gomodcache="$(mktemp -d "${TMPDIR:-/tmp}/golib-modcache.XXXXXX")"
cleanup() {
    chmod -R u+w "${gocache}" "${gomodcache}" 2>/dev/null || true
    find "${gocache}" -depth -delete 2>/dev/null || true
    find "${gomodcache}" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

GOCACHE="${gocache}" GOMODCACHE="${gomodcache}" "$@"
`

const standaloneReleaseScript = `#!/usr/bin/env bash
set -euo pipefail

dry_run=0
public=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) dry_run=1; shift ;;
        --public) public=1; shift ;;
        *) break ;;
    esac
done
if [[ "${dry_run}" -ne 1 || $# -ne 1 ]]; then
    printf 'usage: %s --dry-run [--public] <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
record="$(jq -ce --arg directory "${module}" \
    '.modules[] | select(.directory == $directory and .releasable == true)' \
    "${root}/modules.json")"
module_path="$(jq -r '.module_path' <<<"${record}")"
tag_prefix="$(jq -r '.tag_prefix' <<<"${record}")"
tag="${tag_prefix}1.0.0"
directory="${root}/${module}"

[[ "$(sed -n 's/^module[[:space:]]\+//p' "${directory}/go.mod")" == "${module_path}" ]]
if grep -Eq '^[[:space:]]*replace([[:space:]]|$)' "${directory}/go.mod"; then
    printf 'release module contains a replace directive: %s\n' "${module}" >&2
    exit 1
fi
if git -C "${root}" show-ref --verify --quiet "refs/tags/${tag}"; then
    printf 'release tag already exists: %s\n' "${tag}" >&2
    exit 1
fi

task="$(mktemp -d "${TMPDIR:-/tmp}/golib-release.XXXXXX")"
# shellcheck disable=SC2329 # Invoked by the release EXIT trap.
cleanup() {
    chmod -R u+w "${task}" 2>/dev/null || true
    find "${task}" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

if [[ "${public}" -eq 1 ]]; then
    GOPROXY="https://proxy.golang.org,direct" GOWORK=off \
        go list -m "${module_path}@v1.0.0" >/dev/null
else
    proxy="${task}/proxy"
    mkdir "${proxy}"
    "${root}/.golib/scripts/build-local-proxy.sh" "${proxy}" v1.0.0
    GOPROXY="file://${proxy},https://proxy.golang.org,direct" \
        GONOSUMDB="github.com/faustbrian/go-*" GOWORK=off \
        go list -m "${module_path}@v1.0.0" >/dev/null
fi

printf 'release dry-run passed: %s %s\n' "${module_path}" "${tag}"
`

const standaloneCIWorkflow = `name: CI

on:
  pull_request:
  push:
    branches: [main]
  schedule:
    - cron: '17 3 * * *'
  workflow_dispatch:

permissions:
  actions: read
  contents: read

concurrency:
  group: {{REPOSITORY}}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}

jobs:
  repository-contract:
    name: Repository contract
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
        with:
          fetch-depth: 0
      - run: ./.golib/scripts/repository-check.sh

  quality:
    name: Quality
    runs-on: ubuntu-24.04
    timeout-minutes: 360
    steps:
      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: .go-version
          cache: false
      - name: Install required system tools
        run: sudo apt-get update && sudo apt-get install --yes ripgrep
      - run: ./.golib/scripts/with-disposable-go-cache.sh ./.golib/scripts/run-modules.sh check --all

  codeql:
    name: CodeQL
    runs-on: ubuntu-24.04
    timeout-minutes: 120
    permissions:
      contents: read
      packages: read
      security-events: write
    steps:
      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: .go-version
          cache: false
      - uses: github/codeql-action/init@e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81 # v4
        with:
          languages: go
          queries: security-extended,security-and-quality
      - run: ./.golib/scripts/with-disposable-go-cache.sh ./.golib/scripts/run-modules.sh test --all
      - uses: github/codeql-action/analyze@e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81 # v4
`
