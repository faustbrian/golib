package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var standaloneSpellingIgnoreRegExpList = []string{
	"/```[\\s\\S]*?```/g",
	"/`[^`\\n]+`/g",
	`/https?:\/\/[^\s)]+/g`,
}

var standaloneSpellingWords = []string{
	"abortability",
	"acks",
	"AIMD",
	"ALPN",
	"antimeridian",
	"Arazzo",
	"arities",
	"autovacuum",
	"backpressured",
	"backpressures",
	"Banderwagon",
	"benchmem",
	"benchtime",
	"Besu",
	"bidi",
	"bignum",
	"bignums",
	"BIPM",
	"bodyclose",
	"bodyless",
	"boundedly",
	"Brakmo",
	"bulkheading",
	"bytea",
	"bytewise",
	"calendarclock",
	"calendarconfig",
	"calendartemporal",
	"calendartest",
	"calendarvalidation",
	"calendarwire",
	"cancelably",
	"canonicality",
	"canonicalizer",
	"Canonicalizers",
	"Cavage",
	"CEFACT",
	"cenkalti",
	"checkpointed",
	"checkpointing",
	"CLDR",
	"cooldowns",
	"copylocks",
	"dataref",
	"Datatypes",
	"dateperiod",
	"dateranges",
	"dedup",
	"Defaultable",
	"deferrability",
	"definedness",
	"depguard",
	"drainable",
	"ECMAREGEXP",
	"entrancy",
	"EPSG",
	"Erigon",
	"errgroup",
	"errorlint",
	"Eventsourcing",
	"Eventtest",
	"evictable",
	"EWKB",
	"EWKT",
	"Excelize",
	"Excelize's",
	"Exploitability",
	"failback",
	"fenceable",
	"finalizer",
	"finalizers",
	"fixarrays",
	"formedness",
	"fstatat",
	"fsyncs",
	"geohash",
	"geohashes",
	"Glippy",
	"gnark",
	"gnark's",
	"GOARCH",
	"gochecknoglobals",
	"Goexit",
	"gofmt",
	"gofumpt",
	"goimports",
	"goleak",
	"golines",
	"gomodguard",
	"golib",
	"goodput",
	"Graphviz",
	"Grule",
	"Grule's",
	"Hallgren",
	"HDFS",
	"healthhttp",
	"heartbeated",
	"hedgeable",
	"hexary",
	"HGETALL",
	"HLEN",
	"Hoeffding",
	"hostless",
	"hrefs",
	"HSTS",
	"HTTPCLIENT",
	"HTTPMIDDLEWARE",
	"HTTPWG",
	"Hyperledger",
	"hysteretic",
	"IDNA",
	"IFMA",
	"inflectors",
	"instrumenter",
	"interfacebloat",
	"interprocedural",
	"ireturn",
	"IUGG",
	"JAAS",
	"Jetify",
	"kadm",
	"Karney",
	"Keccak",
	"keepalive",
	"keylessly",
	"kubeconfig",
	"kubelet",
	"leaderlessly",
	"leaderlessness",
	"leakless",
	"lestrrat",
	"libphonenumber",
	"libpq",
	"linearizable",
	"llms",
	"locationless",
	"Logrus",
	"loopclosure",
	"lostcancel",
	"matoous",
	"maxmemory",
	"merkleization",
	"MERKLETREE",
	"metacharacters",
	"Methodless",
	"microbenchmark",
	"microbenchmarks",
	"Misordered",
	"misresolution",
	"MLSD",
	"MLST",
	"MTOM",
	"multichecker",
	"Multiproof",
	"multiranges",
	"multiset",
	"multisets",
	"multitenancy",
	"nacks",
	"Nethermind",
	"NFKC",
	"NFKD",
	"nilaway",
	"nilness",
	"noctx",
	"noeviction",
	"nonblank",
	"nonblocking",
	"noncanonical",
	"noncomment",
	"nondecreasing",
	"Noninteractive",
	"nonminimal",
	"nonportable",
	"nonpositive",
	"nontransactional",
	"nsqd",
	"O'Malley",
	"OAUTHBEARER",
	"obsoletion",
	"OCSP",
	"oklog",
	"OOXML",
	"ORPC",
	"openfeature",
	"Packagist",
	"parseability",
	"PASETO",
	"PCRE",
	"Pedersen",
	"Petstore",
	"pgxpool",
	"poolers",
	"postorder",
	"precomputation",
	"preemptible",
	"preflighted",
	"preflights",
	"prehash",
	"prehashed",
	"preimplement",
	"presigning",
	"prevalidated",
	"promlinter",
	"pseudonymization",
	"pseudonymize",
	"punycoded",
	"quickstarts",
	"quiesces",
	"qvalues",
	"ratelimithttp",
	"ratelimitlog",
	"ratelimitprincipal",
	"ratelimitqueue",
	"ratelimitrpc",
	"ratelimittelemetry",
	"ratelimittest",
	"readback",
	"reauthenticated",
	"reauthenticates",
	"reauthentication",
	"rebaseline",
	"rebaselining",
	"redispatch",
	"Redpanda",
	"redrive",
	"redriven",
	"redrives",
	"reentrancy",
	"reentrantly",
	"reindexing",
	"remappable",
	"remediations",
	"replayability",
	"repolled",
	"repolls",
	"Repr",
	"requiredness",
	"resampler",
	"reserialized",
	"retryer",
	"revalidations",
	"revisioned",
	"revisioning",
	"RSASSA",
	"RUSTSEC",
	"sampledrate",
	"Sarama",
	"Sarama's",
	"SARIF",
	"savepoints",
	"seccomp",
	"seekability",
	"Semian",
	"Semian's",
	"servicetest",
	"shamaton",
	"shipit",
	"Shopspring",
	"Sigstore",
	"simplefeatures",
	"singleflight",
	"slowloris",
	"sluggable",
	"sortation",
	"spatie",
	"Spatie's",
	"speedata",
	"SPKI",
	"sqlclosecheck",
	"SRID",
	"sslmode",
	"subcodes",
	"SUBPROOF",
	"subresource",
	"subschema",
	"subschemas",
	"subsecond",
	"Subtag",
	"subtries",
	"subvalues",
	"Symfony",
	"synctest",
	"Temurin",
	"terminality",
	"timeofday",
	"TOCTOU",
	"Toggl",
	"Toxiproxy",
	"Tracestate",
	"triggerable",
	"trimpath",
	"txaty",
	"tzdata",
	"Unacked",
	"unacquired",
	"unactivated",
	"unadvanced",
	"uncacheable",
	"uncancellable",
	"uncited",
	"unclaimable",
	"uncompiled",
	"unconfigured",
	"unencodable",
	"unguessability",
	"unioned",
	"unjittered",
	"unmarshal",
	"unmarshalling",
	"unpartitioned",
	"unranking",
	"unskewed",
	"unstarted",
	"upcaster",
	"upcasters",
	"upcasting",
	"urfave",
	"userinfo",
	"valkeygo",
	"variadics",
	"varint",
	"vettool",
	"voku",
	"worktrees",
	"writability",
	"xorshift",
	"XSTS",
	"Yugabyte",
	"Zstandard",
	"zxinggo",
}

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
	requiredServices []string,
) error {
	preservedMakefile := filepath.Join(destination, ".golib/package.mk")
	packageMakefile := filepath.Join(sourceRoot, repository.SourceDirectory, "Makefile")
	contents, readErr := os.ReadFile(packageMakefile)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("read package Makefile: %w", readErr)
	}
	if readErr == nil {
		contents = rewriteStandaloneContents(contents, paths, nil, false)
		contents = rewriteStandaloneRepositoryPaths(
			contents,
			repository.Family,
			repository.Name,
		)
		contents = rewriteStandalonePackageMakefile(contents)
		if err := os.MkdirAll(filepath.Join(destination, ".golib"), 0o755); err != nil {
			return fmt.Errorf("create tooling directory: %w", err)
		}
		if err := os.WriteFile(preservedMakefile, contents, 0o644); err != nil {
			return fmt.Errorf("preserve package Makefile: %w", err)
		}
	} else if err := os.Remove(preservedMakefile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove obsolete package Makefile: %w", err)
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
	if err := copyStandaloneServiceFixtures(
		sourceRoot,
		destination,
		repository,
		paths,
		requiredServices,
	); err != nil {
		return err
	}
	if err := writeStandaloneMutationPolicies(
		sourceRoot,
		destination,
		"pkg/"+repository.Family,
	); err != nil {
		return err
	}
	packageLintConfiguration, err := installStandaloneLintConfiguration(
		sourceRoot,
		destination,
		repository,
		paths,
	)
	if err != nil {
		return err
	}

	generated := map[string]string{
		"Makefile":                                   standaloneMakefile,
		".golangci.yml":                              standaloneGolangCILint,
		".github/dependabot.yml":                     standaloneDependabot,
		".github/workflows/ci.yml":                   standaloneCIWorkflow,
		".golib/scripts/run-modules.sh":              standaloneRunModules,
		".golib/scripts/check-go-safety.sh":          standaloneSafetyScript,
		".golib/scripts/check-go-safety.go":          standaloneSafetyProgram,
		".golib/scripts/codeql-build.sh":             standaloneCodeQLBuild,
		".golib/scripts/repository-check.sh":         standaloneRepositoryCheck,
		".golib/scripts/release.sh":                  standaloneReleaseScript,
		".golib/scripts/with-disposable-go-cache.sh": standaloneDisposableCache,
	}
	for relative, template := range generated {
		if relative == ".golangci.yml" && packageLintConfiguration {
			continue
		}
		contents := strings.ReplaceAll(template, "{{REPOSITORY}}", repository.Name)
		filename := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return fmt.Errorf("create %s parent: %w", relative, err)
		}
		mode := fs.FileMode(0o644)
		if strings.HasSuffix(relative, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filename, []byte(contents), mode); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}

	return nil
}

func rewriteStandalonePackageMakefile(contents []byte) []byte {
	return bytes.ReplaceAll(contents, []byte("../../scripts/"), []byte("./.golib/scripts/"))
}

func installStandaloneLintConfiguration(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	paths map[string]string,
) (bool, error) {
	sourceRelative := filepath.Join("pkg", repository.Family, ".golangci.yml")
	if _, err := os.Stat(filepath.Join(sourceRoot, sourceRelative)); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect package lint configuration: %w", err)
	}
	if err := copyStandaloneFoundationFileAs(
		sourceRoot,
		destination,
		sourceRelative,
		".golangci.yml",
		repository,
		paths,
	); err != nil {
		return false, err
	}
	return true, nil
}

func copyStandaloneServiceFixtures(
	sourceRoot string,
	destination string,
	repository standaloneRepository,
	paths map[string]string,
	requiredServices []string,
) error {
	fixtures := []struct {
		source      string
		destination string
		services    []string
	}{
		{
			source:      "pkg/rabbitstream/rabbitmq/integration",
			destination: ".golib/services/rabbitstream",
			services:    []string{"rabbitstream", "rabbitstream-standalone"},
		},
		{
			source:      "pkg/search/adapters/opensearch/scripts/opensearch-images.env",
			destination: ".golib/services/opensearch/opensearch-images.env",
			services:    []string{"opensearch"},
		},
	}
	for _, fixture := range fixtures {
		if !standaloneServicesIntersect(requiredServices, fixture.services) {
			continue
		}
		source := filepath.Join(sourceRoot, filepath.FromSlash(fixture.source))
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("inspect standalone service fixture %s: %w", fixture.source, err)
		}
		if !info.IsDir() {
			if err := copyStandaloneFoundationFileAs(
				sourceRoot,
				destination,
				fixture.source,
				fixture.destination,
				repository,
				paths,
			); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(source, func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(source, filename)
			if err != nil {
				return err
			}
			return copyStandaloneFoundationFileAs(
				sourceRoot,
				destination,
				filepath.ToSlash(filepath.Join(fixture.source, relative)),
				filepath.ToSlash(filepath.Join(fixture.destination, relative)),
				repository,
				paths,
			)
		}); err != nil {
			return err
		}
	}
	return nil
}

func standaloneServicesIntersect(required []string, candidates []string) bool {
	for _, service := range required {
		for _, candidate := range candidates {
			if service == candidate {
				return true
			}
		}
	}
	return false
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
	err := filepath.WalkDir(source, func(filename string, entry fs.DirEntry, walkErr error) error {
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
		if standaloneMigrationOnlyScript(sourceRelative) {
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
	if err != nil {
		return err
	}
	for _, relative := range standaloneMigrationOnlyScripts {
		filename := filepath.Join(destination, ".golib", relative)
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove migration-only standalone script %s: %w", relative, err)
		}
	}
	return nil
}

var standaloneMigrationOnlyScripts = []string{
	"scripts/capture-standalone-repository-audit.sh",
	"scripts/extract-standalone-repository.sh",
	"scripts/migrate-standalone-mutation-evidence.sh",
	"scripts/tidy-standalone-modules.sh",
}

func standaloneMigrationOnlyScript(relative string) bool {
	for _, candidate := range standaloneMigrationOnlyScripts {
		if relative == candidate {
			return true
		}
	}
	return false
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
	contents = rewriteStandaloneContents(contents, paths, nil, false)
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
	if sourceRelative == ".gitignore" {
		contents = rewriteStandaloneGitignore(contents)
	}
	if sourceRelative == "AGENTS.md" {
		contents = rewriteStandaloneAgentPolicy(contents)
	}
	if sourceRelative == "CONTRIBUTING.md" {
		contents = rewriteStandaloneContributing(contents)
	}
	if sourceRelative == "SECURITY.md" {
		contents = rewriteStandaloneSecurity(contents, repository.Name)
	}
	if sourceRelative == "cspell.json" {
		contents, err = rewriteStandaloneSpellingConfiguration(contents)
		if err != nil {
			return fmt.Errorf("rewrite standalone spelling configuration: %w", err)
		}
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

func rewriteStandaloneGitignore(contents []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	found := false
	for index, line := range lines {
		if line != "package-lock.json" && line != "!package-lock.json" {
			continue
		}
		lines[index] = "!package-lock.json"
		found = true
	}
	if !found {
		lines = append(lines, "!package-lock.json")
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func rewriteStandaloneTooling(contents []byte, relative string, modulePath string) []byte {
	toolingRelative := strings.TrimPrefix(relative, ".golib/")
	if !strings.HasPrefix(toolingRelative, "scripts/") {
		return contents
	}
	type replacement struct{ previous, next string }
	replacements := make([]replacement, 0, 7)
	releaseVersion := standaloneReleaseVersionForPath(modulePath)
	if toolingRelative == "scripts/repository-check.sh" &&
		!strings.Contains(string(contents), "ls-files --error-unmatch package-lock.json") {
		contents = bytes.Replace(
			contents,
			[]byte("\ngit diff --check"),
			[]byte("\ngit -C \"${root}\" ls-files --error-unmatch package-lock.json >/dev/null\n\ngit diff --check"),
			1,
		)
	}
	if toolingRelative == "scripts/check-module.sh" {
		replacements = append(replacements,
			replacement{
				`"${GOLIB_LOCAL_PROXY}" v0.0.0`,
				`"${GOLIB_LOCAL_PROXY}" ` + releaseVersion,
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
		)
	}
	if toolingRelative == "scripts/build-local-proxy.sh" {
		replacements = append(replacements, replacement{
			`version="${2:-v0.0.0}"`,
			`version="${2:-` + releaseVersion + `}"`,
		})
	}
	if toolingRelative == "scripts/mutation-verifier-identity.sh" {
		replacements = append(replacements,
			replacement{
				`[[ -f "${root}/${input}" ]]`,
				`[[ -f "${root}/.golib/${input}" ]]`,
			},
			replacement{
				`shasum -a 256 "${root}/${input}"`,
				`shasum -a 256 "${root}/.golib/${input}"`,
			},
		)
	}
	if toolingRelative == "scripts/package-source-digest.sh" {
		replacements = append(replacements, replacement{
			`package_directory="$1"
case "${package_directory}" in
    pkg/*) ;;
    *)
        printf 'package directory must be beneath pkg/: %s\n' \
            "${package_directory}" >&2
        exit 2
        ;;
esac`,
			`package_directory="$1"
package_directory="${package_directory#./}"
case "${package_directory}" in
    ""|/*|../*|*/../*|*/..)
        printf 'package directory must be repository-relative: %s\n' \
            "${package_directory}" >&2
        exit 2
        ;;
esac`,
		})
	}
	replacements = append(replacements,
		replacement{
			`${root}/pkg/rabbitstream/rabbitmq/integration`,
			`${root}/.golib/services/rabbitstream`,
		},
		replacement{
			`${root}/pkg/search/adapters/opensearch/scripts/opensearch-images.env`,
			`${root}/.golib/services/opensearch/opensearch-images.env`,
		},
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
		canonicalDocs := "if [[ \"${module}\" == \".\" ]]; then\n" +
			"                GOWORK=off \"${root}/.golib/scripts/check-documentation.sh\"\n" +
			"            fi\n" +
			"            if target=\"$(find_make_target docs documentation)\"; then\n" +
			"                make \"${target}\"\n" +
			"            elif [[ \"${module}\" != \".\" ]]; then\n" +
			"                GOWORK=off go test ./... -run '^Example' -count=1\n" +
			"            fi"
		legacyDocs := []string{
			"if [[ \"${module}\" == \".\" ]]; then\n" +
				"                GOWORK=off \"${root}/.golib/scripts/check-documentation.sh\"\n" +
				"            elif target=\"$(find_make_target docs documentation)\"; then\n" +
				"                make \"${target}\"\n" +
				"            else\n" +
				"                GOWORK=off go test ./... -run '^Example' -count=1\n" +
				"            fi",
			"if target=\"$(find_make_target docs documentation)\"; then\n" +
				"                package_make \"${target}\"\n" +
				"            elif [[ \"${module}\" == \".\" ]]; then\n" +
				"                GOWORK=off \"${root}/.golib/scripts/check-documentation.sh\"\n" +
				"            else\n" +
				"                GOWORK=off go test ./... -run '^Example' -count=1\n" +
				"            fi",
		}
		for _, legacy := range legacyDocs {
			contents = []byte(strings.ReplaceAll(string(contents), legacy, canonicalDocs))
		}
		contents = []byte(strings.ReplaceAll(
			string(contents),
			`                make GOWORK=off "${target}"`,
			`                package_make GOWORK=off "${target}"`,
		))
		contents = []byte(strings.ReplaceAll(
			string(contents),
			`                make "${target}"`,
			`                package_make "${target}"`,
		))
		contents = []byte(strings.ReplaceAll(
			string(contents),
			"                make \\\n",
			"                package_make \\\n",
		))
		for strings.Contains(string(contents), "package_package_make") {
			contents = []byte(strings.ReplaceAll(
				string(contents),
				"package_package_make",
				"package_make",
			))
		}
	}
	if toolingRelative == "scripts/package-source-digest.sh" &&
		!strings.Contains(string(contents), `package_directory="$1"`) {
		contents = []byte(strings.Replace(
			string(contents),
			`package_directory="${package_directory#./}"`,
			"package_directory=\"$1\"\n"+`package_directory="${package_directory#./}"`,
			1,
		))
	}
	if toolingRelative == "scripts/check-documentation.sh" {
		contents = []byte(strings.ReplaceAll(
			string(contents),
			"go run ./cmd/golib documentation\n",
			"test -s README.md\n",
		))
		if !strings.Contains(string(contents), "golib-unpublished-pkg-go-dev") {
			contents = []byte(strings.Replace(
				string(contents),
				`"${lychee}" \
    --cache=false \
`,
				`# golib-unpublished-pkg-go-dev: public-proxy checks own publication readiness.
"${lychee}" \
    --cache=false \
    --exclude '^https://pkg\.go\.dev/(badge/)?github\.com/faustbrian/go-' \
    --exclude '^https://doi\.org/10\.1145/190314\.190317$' \
    --exclude '^https://service\.unece\.org/trade/' \
    --exclude '^https://www\.iso\.org/standard/' \
`,
				1,
			))
		}
		for _, exclusion := range []string{
			`^https://pkg\.go\.dev/(badge/)?github\.com/faustbrian/go-`,
			`^https://doi\.org/10\.1145/190314\.190317$`,
			`^https://service\.unece\.org/trade/`,
			`^https://www\.iso\.org/standard/`,
		} {
			argument := "    --exclude '" + exclusion + "' \\\n"
			if strings.Contains(string(contents), argument) {
				continue
			}
			contents = []byte(strings.Replace(
				string(contents),
				"    --max-concurrency 16 \\\n",
				argument+"    --max-concurrency 16 \\\n",
				1,
			))
		}
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

func rewriteStandaloneContributing(contents []byte) []byte {
	replacements := []struct{ previous, next string }{
		{
			"[dependency governance policy](docs/dependency-governance.md)",
			"[dependency governance policy](AGENTS.md#dependencies-and-supply-chain)",
		},
		{
			"[specification governance contract](docs/specification-governance.md)",
			"[specification governance contract](AGENTS.md#design)",
		},
		{
			"make specification-decisions\n",
			"",
		},
		{
			"make check MODULES=pkg/<library>",
			"make check",
		},
		{
			"make ci-changed BASE=origin/main",
			"make ci",
		},
		{
			"[module lifecycle procedures](docs/module-lifecycle.md)",
			"[repository structure policy](AGENTS.md#repository-structure)",
		},
	}
	text := string(contents)
	for _, replacement := range replacements {
		text = strings.ReplaceAll(text, replacement.previous, replacement.next)
	}
	mutationRequirement := "Required mutation gates must finish with zero surviving viable mutants."
	for strings.Contains(text, mutationRequirement+"\n\n"+mutationRequirement) {
		text = strings.ReplaceAll(
			text,
			mutationRequirement+"\n\n"+mutationRequirement,
			mutationRequirement,
		)
	}
	if !strings.Contains(text, mutationRequirement) {
		text = strings.ReplaceAll(
			text,
			"Do not add package-local workflows, permanent replacements,",
			mutationRequirement+"\n\nDo not add package-local workflows, permanent replacements,",
		)
	}
	return []byte(text)
}

func rewriteStandaloneChangelog(contents []byte) []byte {
	entries := []struct {
		flat    string
		wrapped string
	}{
		{
			flat: "- Harden standalone documentation validation with deterministic spelling and link checks, package-specific documentation gates, and repository-local contributor guidance.",
			wrapped: "- Harden standalone documentation validation with deterministic spelling and\n" +
				"  link checks, package-specific documentation gates, and repository-local\n" +
				"  contributor guidance.",
		},
		{
			flat: "- Reconcile standalone dependency checksums against deterministic current module archives so CI, local verification, and release consumers resolve identical content.",
			wrapped: "- Reconcile standalone dependency checksums against deterministic current\n" +
				"  module archives so CI, local verification, and release consumers resolve\n" +
				"  identical content.",
		},
		{
			flat: "- Track the pinned documentation-tool lockfile so clean CI checkouts install the exact validated cspell dependency.",
			wrapped: "- Track the pinned documentation-tool lockfile so clean CI checkouts install\n" +
				"  the exact validated cspell dependency.",
		},
	}
	text := string(contents)
	for _, entry := range entries {
		text = strings.ReplaceAll(text, entry.flat, entry.wrapped)
	}
	header := "## [Unreleased]\n"
	if !strings.Contains(text, header) {
		header = "## Unreleased\n"
	}
	headerIndex := strings.Index(text, header)
	if headerIndex < 0 {
		return []byte(text)
	}
	bodyStart := headerIndex + len(header)
	bodyEnd := len(text)
	if next := strings.Index(text[bodyStart:], "\n## "); next >= 0 {
		bodyEnd = bodyStart + next
	}
	section := text[bodyStart:bodyEnd]
	missing := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.Contains(section, entry.wrapped) {
			missing = append(missing, entry.wrapped)
		}
	}
	if len(missing) == 0 {
		return []byte(text)
	}

	addition := strings.Join(missing, "\n") + "\n\n"
	const changedHeading = "\n### Changed\n"
	if changedIndex := strings.Index(section, changedHeading); changedIndex >= 0 {
		insertion := bodyStart + changedIndex + len(changedHeading)
		if insertion < len(text) && text[insertion] == '\n' {
			insertion++
		}
		return []byte(text[:insertion] + addition + text[insertion:])
	}
	return []byte(text[:bodyStart] + "\n### Changed\n\n" + addition + text[bodyStart:])
}

func rewriteStandaloneSecurity(contents []byte, repositoryName string) []byte {
	text := strings.ReplaceAll(
		string(contents),
		"`faustbrian/golib`",
		"`faustbrian/"+repositoryName+"`",
	)
	text = strings.ReplaceAll(
		text,
		"Until modules reach `v1`, only the latest released minor line receives security\nfixes. After `v1`, support windows are documented per module and in",
		"The latest stable `v1` release line receives security fixes. Support windows\nare documented per module and in",
	)
	text = strings.ReplaceAll(
		text,
		"The repository-wide [threat model](docs/security/threat-model.md),\n[security matrix](docs/security/security-matrix.md), and\n[residual-risk register](docs/security/residual-risks.md) define shared trust\nboundaries and open release risks. Package-specific threat models refine those\nrules for their owned boundary; they do not replace the repository model.",
		"The repository [safety and concurrency policy](AGENTS.md#safety-and-concurrency)\nand [supply-chain policy](AGENTS.md#dependencies-and-supply-chain) define shared\ntrust boundaries and release requirements. Package-specific security guidance\nrefines those rules for its owned boundary.",
	)
	return []byte(text)
}

func rewriteStandaloneSpellingConfiguration(contents []byte) ([]byte, error) {
	configuration := map[string]json.RawMessage{}
	if err := json.Unmarshal(contents, &configuration); err != nil {
		return nil, err
	}

	words := make([]string, 0)
	if raw, ok := configuration["words"]; ok {
		if err := json.Unmarshal(raw, &words); err != nil {
			return nil, fmt.Errorf("decode words: %w", err)
		}
	}
	words = append(words, standaloneSpellingWords...)
	configuration["words"] = mustStandaloneJSON(sortedUniqueStrings(words))

	ignoreRegExpList := make([]string, 0)
	if raw, ok := configuration["ignoreRegExpList"]; ok {
		if err := json.Unmarshal(raw, &ignoreRegExpList); err != nil {
			return nil, fmt.Errorf("decode ignoreRegExpList: %w", err)
		}
	}
	ignoreRegExpList = append(ignoreRegExpList, standaloneSpellingIgnoreRegExpList...)
	configuration["ignoreRegExpList"] = mustStandaloneJSON(sortedUniqueStrings(ignoreRegExpList))

	rewritten, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rewritten, '\n'), nil
}

func sortedUniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func mustStandaloneJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

const standaloneMakefile = `SHELL := /usr/bin/env bash

.PHONY: check ci inventory repository-check

check:
	./.golib/scripts/with-disposable-go-cache.sh ./.golib/scripts/run-modules.sh check --all

ci: repository-check check

inventory repository-check:
	./.golib/scripts/repository-check.sh
`

const standaloneGolangCILint = `version: "2"

run:
  timeout: 10m

linters:
  default: standard

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

go run "${root}/.golib/scripts/check-go-safety.go" "${directory}"
printf 'standalone safety policy passed for %s\n' "${module}"
`

const standaloneSafetyProgram = `package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-go-safety <directory>")
		os.Exit(2)
	}
	violations, err := scan(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, violation := range violations {
		fmt.Fprintln(os.Stderr, violation)
	}
	if len(violations) != 0 {
		os.Exit(1)
	}
}

func scan(directory string) ([]string, error) {
	violations := make([]string, 0)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != directory && excludedDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("parse import in %s: %w", path, err)
			}
			if name == "unsafe" || name == "C" {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: forbidden import %q",
					path,
					fileSet.Position(imported.Pos()).Line,
					name,
				))
			}
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(strings.TrimSpace(comment.Text), "//go:linkname") {
					violations = append(violations, fmt.Sprintf(
						"%s:%d: forbidden go:linkname directive",
						path,
						fileSet.Position(comment.Pos()).Line,
					))
				}
			}
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}

func excludedDirectory(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")
}
`

const standaloneCodeQLBuild = `#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
task="$(mktemp -d "${TMPDIR:-/tmp}/golib-codeql-build.XXXXXX")"
cleanup() {
    find "${task}" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

while IFS= read -r module; do
    [[ -n "${module}" ]] || continue
    module_root="${root}"
    if [[ "${module}" != "." ]]; then
        module_root="${root}/${module}"
    fi
    while IFS= read -r package; do
        [[ -n "${package}" ]] || continue
        package_tags="$(
            jq -r \
                --arg module "${module}" \
                --arg package "${package}" '
                    .modules[]
                    | select(.directory == $module)
                    | .packages[]
                    | select(.import_path == $package)
                    | .build_tags[]?
                ' "${root}/modules.json"
        )"
        slug="$(printf '%s' "${package}" | tr '/.' '--')"
        if [[ "${module}" == "benchmarks/platform" && -n "${package_tags}" ]]; then
            package_tags=benchmark_disabled
        fi
        if [[ -z "${package_tags}" ]]; then
            (cd "${module_root}" && GOWORK=off go build -o "${task}/${slug}" "${package}")
            continue
        fi
        variant=0
        while IFS= read -r tag; do
            [[ -n "${tag}" ]] || continue
            (cd "${module_root}" && GOWORK=off go build \
                -tags="${tag}" -o "${task}/${slug}-${variant}" "${package}")
            variant=$((variant + 1))
        done <<<"${package_tags}"
    done < <(
        jq -r --arg module "${module}" '
            .modules[]
            | select(.directory == $module)
            | .packages[]
            | select(.build_required == true)
            | .import_path
        ' "${root}/modules.json"
    )
done < <(jq -r '.modules[].directory' "${root}/modules.json")
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

if grep -REnI \
    --exclude-dir='.git' \
    --exclude-dir='.artifacts' \
    --exclude='go.sum' \
    --exclude='CHANGELOG.md' \
    --exclude='repository-check.sh' \
    'github\.com/faustbrian/golib/pkg|/Users/[^/]+/Developer|\.\./go-' \
    "${root}"; then
    printf 'monorepo or sibling-checkout reference remains\n' >&2
    exit 1
fi

git -C "${root}" ls-files --error-unmatch package-lock.json >/dev/null

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
version="v$(jq -r '.version' <<<"${record}")"
tag="${tag_prefix}${version#v}"
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
        go list -m "${module_path}@${version}" >/dev/null
else
    proxy="${task}/proxy"
    mkdir "${proxy}"
    "${root}/.golib/scripts/build-local-proxy.sh" "${proxy}" "${version}"
    GOPROXY="file://${proxy},https://proxy.golang.org,direct" \
        GONOSUMDB="github.com/faustbrian/go-*" GOWORK=off \
        go list -m "${module_path}@${version}" >/dev/null
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
    inputs:
      release_dry_run:
        description: Run the stable v1 dry-run for every releasable module
        required: false
        default: false
        type: boolean

permissions:
  actions: read
  contents: read

concurrency:
  group: {{REPOSITORY}}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}

jobs:
  prepare:
    name: Select modules
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    outputs:
      matrix: ${{ steps.selection.outputs.matrix }}
    steps:
      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
      - name: Select repository modules
        id: selection
        env:
          RELEASE_DRY_RUN: ${{ inputs.release_dry_run }}
        run: |
          set -euo pipefail
          filter='.'
          if [[ "${RELEASE_DRY_RUN}" == true ]]; then
            filter='map(select(.releasable == true))'
          fi
          matrix="$(
            jq -c \
              --arg filter "${filter}" '
                .modules
                | if $filter == "." then . else map(select(.releasable == true)) end
                | map({
                    directory,
                    artifact: (
                      if .directory == "." then "root"
                      else (.directory | gsub("/"; "-"))
                      end
                    )
                  })
              ' modules.json
          )"
          [[ "$(jq 'length' <<<"${matrix}")" -gt 0 ]]
          echo "matrix=${matrix}" >>"${GITHUB_OUTPUT}"

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
    name: Quality / ${{ matrix.directory }}
    needs: prepare
    runs-on: ubuntu-24.04
    timeout-minutes: 360
    strategy:
      fail-fast: false
      max-parallel: 8
      matrix:
        include: ${{ fromJSON(needs.prepare.outputs.matrix) }}
    steps:
      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: .go-version
          cache: false
      - name: Set up pinned ripgrep
        env:
          RIPGREP_VERSION: 15.2.0
          RIPGREP_SHA256: 33e15bcf1624b25cdd2a55813a47a2f95dbe126268203e76aa6a585d1e7b149c
        run: |
          set -euo pipefail
          archive="${RUNNER_TEMP}/ripgrep.tar.gz"
          root="${RUNNER_TEMP}/ripgrep"
          curl --fail --silent --show-error --location \
            --retry 5 --retry-delay 2 --retry-all-errors \
            "https://github.com/BurntSushi/ripgrep/releases/download/${RIPGREP_VERSION}/ripgrep-${RIPGREP_VERSION}-x86_64-unknown-linux-musl.tar.gz" \
            --output "${archive}"
          printf '%s  %s\n' "${RIPGREP_SHA256}" "${archive}" | sha256sum --check -
          mkdir -p "${root}"
          tar --extract --gzip --file "${archive}" --directory "${root}" \
            --strip-components 1
          echo "${root}" >>"${GITHUB_PATH}"
          "${root}/rg" --version | grep -Eq "^ripgrep ${RIPGREP_VERSION}( |$)"
      - name: Set up Node
        if: github.repository == 'faustbrian/go-ecma-regexp' || github.repository == 'faustbrian/go-queue-control-plane'
        uses: actions/setup-node@249970729cb0ef3589644e2896645e5dc5ba9c38 # v6
        with:
          node-version: '24.4.1'
          package-manager-cache: false
      - name: Set up Deno
        if: github.repository == 'faustbrian/go-ecma-regexp'
        uses: denoland/setup-deno@22d081ff2d3a40755e97629de92e3bcbfa7cf2ed # v2.0.5
        with:
          deno-version: '2.9.4'
          cache: false
      - name: Set up CLI shell runtime
        if: github.repository == 'faustbrian/go-cli'
        env:
          ZSH_DEB_SHA256: bd5cc8dd3a01a6db38c0a815d75202c356a9c7f378674ba7bed9bc86dcba8af0
        run: |
          set -euo pipefail
          archive="${RUNNER_TEMP}/zsh.deb"
          root="${RUNNER_TEMP}/zsh-runtime"
          curl --fail --silent --show-error --location \
            --retry 5 --retry-delay 2 --retry-all-errors \
            'https://archive.ubuntu.com/ubuntu/pool/main/z/zsh/zsh_5.9-6ubuntu2_amd64.deb' \
            --output "${archive}"
          printf '%s  %s\n' "${ZSH_DEB_SHA256}" "${archive}" | sha256sum --check -
          mkdir -p "${root}"
          dpkg-deb --extract "${archive}" "${root}"
          echo "${root}/bin" >>"${GITHUB_PATH}"
          "${root}/bin/zsh" --version | grep -Eq '^zsh 5\.9 '
      - name: Set up unpublished module proxy
        if: vars.GOLIB_BOOTSTRAP_PROXY_URL != ''
        env:
          PROXY_URL: ${{ vars.GOLIB_BOOTSTRAP_PROXY_URL }}
          PROXY_SHA256: ${{ vars.GOLIB_BOOTSTRAP_PROXY_SHA256 }}
        run: |
          set -euo pipefail
          [[ "${PROXY_SHA256}" =~ ^[0-9a-f]{64}$ ]]
          archive="${RUNNER_TEMP}/golib-proxy.tar.gz"
          proxy="${RUNNER_TEMP}/golib-proxy"
          curl --fail --silent --show-error --location \
            --retry 5 --retry-delay 2 --retry-all-errors \
            "${PROXY_URL}" --output "${archive}"
          printf '%s  %s\n' "${PROXY_SHA256}" "${archive}" | sha256sum --check -
          mkdir -p "${proxy}"
          tar --extract --gzip --file "${archive}" --directory "${proxy}"
          echo "GOPROXY=file://${proxy},https://proxy.golang.org,direct" >>"${GITHUB_ENV}"
          echo 'GONOSUMDB=github.com/faustbrian/go-*' >>"${GITHUB_ENV}"
      - name: Restore content-addressed mutation evidence
        if: inputs.release_dry_run != true
        env:
          GH_TOKEN: ${{ github.token }}
          GITHUB_REPOSITORY_ID: ${{ github.repository_id }}
        run: |
          set -euo pipefail
          seed='.golib/mutation-bootstrap/${{ matrix.artifact }}.zip'
          if [[ -s "${seed}" ]]; then
            ./.golib/scripts/restore-ci-mutation-evidence.sh \
              '${{ matrix.directory }}' "${seed}"
          else
            ./.golib/scripts/restore-ci-mutation-evidence.sh \
              '${{ matrix.directory }}'
          fi
      - name: Run strict module contract
        id: strict_contract
        if: inputs.release_dry_run != true
        run: ./.golib/scripts/with-disposable-go-cache.sh ./.golib/scripts/run-modules.sh check --modules '${{ matrix.directory }}'
      - name: Run release dry-run
        id: release_dry_run
        if: inputs.release_dry_run == true
        env:
          GOLIB_VERIFICATION_SNAPSHOT: '1'
        run: |
          set -euo pipefail
          output='.artifacts/release-dry-run.log'
          if [[ '${{ matrix.directory }}' != '.' ]]; then
            output='.artifacts/${{ matrix.directory }}/release-dry-run.log'
          fi
          mkdir -p "$(dirname "${output}")"
          ./.golib/scripts/with-disposable-go-cache.sh \
            ./.golib/scripts/run-modules.sh release-dry-run \
            --modules '${{ matrix.directory }}' 2>&1 | tee "${output}"
      - name: Stage attributable evidence
        if: always()
        env:
          CONTRACT_OUTCOME: ${{ inputs.release_dry_run == true && steps.release_dry_run.outcome || steps.strict_contract.outcome }}
        run: >-
          ./.golib/scripts/stage-ci-evidence.sh '${{ matrix.directory }}'
          '${{ format('{0}/golib-evidence-{1}', runner.temp, matrix.artifact) }}'
          "${CONTRACT_OUTCOME}"
      - name: Upload attributable evidence
        if: always()
        uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f # v6
        with:
          name: evidence-${{ matrix.artifact }}
          path: ${{ format('{0}/golib-evidence-{1}', runner.temp, matrix.artifact) }}
          if-no-files-found: error
          include-hidden-files: true
          retention-days: 30

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
      - name: Set up unpublished module proxy
        if: vars.GOLIB_BOOTSTRAP_PROXY_URL != ''
        env:
          PROXY_URL: ${{ vars.GOLIB_BOOTSTRAP_PROXY_URL }}
          PROXY_SHA256: ${{ vars.GOLIB_BOOTSTRAP_PROXY_SHA256 }}
        run: |
          set -euo pipefail
          [[ "${PROXY_SHA256}" =~ ^[0-9a-f]{64}$ ]]
          archive="${RUNNER_TEMP}/golib-proxy.tar.gz"
          proxy="${RUNNER_TEMP}/golib-proxy"
          curl --fail --silent --show-error --location \
            --retry 5 --retry-delay 2 --retry-all-errors \
            "${PROXY_URL}" --output "${archive}"
          printf '%s  %s\n' "${PROXY_SHA256}" "${archive}" | sha256sum --check -
          mkdir -p "${proxy}"
          tar --extract --gzip --file "${archive}" --directory "${proxy}"
          echo "GOPROXY=file://${proxy},https://proxy.golang.org,direct" >>"${GITHUB_ENV}"
          echo 'GONOSUMDB=github.com/faustbrian/go-*' >>"${GITHUB_ENV}"
      - uses: github/codeql-action/init@e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81 # v4
        with:
          languages: go
          queries: security-extended,security-and-quality
      - run: ./.golib/scripts/with-disposable-go-cache.sh ./.golib/scripts/codeql-build.sh
      - uses: github/codeql-action/analyze@e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81 # v4

  required:
    name: Required
    if: always()
    needs: [prepare, repository-contract, quality, codeql]
    runs-on: ubuntu-24.04
    timeout-minutes: 5
    steps:
      - name: Require every repository result
        env:
          PREPARE_RESULT: ${{ needs.prepare.result }}
          CONTRACT_RESULT: ${{ needs.repository-contract.result }}
          QUALITY_RESULT: ${{ needs.quality.result }}
          CODEQL_RESULT: ${{ needs.codeql.result }}
        run: |
          set -euo pipefail
          for result in \
            "${PREPARE_RESULT}" \
            "${CONTRACT_RESULT}" \
            "${QUALITY_RESULT}" \
            "${CODEQL_RESULT}"; do
            [[ "${result}" == success ]]
          done
`
