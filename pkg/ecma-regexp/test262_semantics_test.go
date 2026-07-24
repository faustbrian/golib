package ecmascript_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

const test262SemanticFileCount = 606
const test262MatcherFileCount = 506
const test262BuiltInFileCount = 1868
const test262BuiltInNegativeParseCount = 192
const test262BuiltInSelectedFileCount = 554
const test262BuiltInMatcherFileCount = 494

var test262RegExpFeatureCounts = map[string]int{
	"RegExp.escape":                   20,
	"Reflect":                         1,
	"Reflect.construct":               12,
	"String.fromCodePoint":            12,
	"Symbol":                          29,
	"Symbol.iterator":                 1,
	"Symbol.match":                    64,
	"Symbol.matchAll":                 26,
	"Symbol.replace":                  70,
	"Symbol.search":                   23,
	"Symbol.species":                  32,
	"Symbol.split":                    44,
	"Symbol.toPrimitive":              1,
	"arrow-function":                  8,
	"cross-realm":                     12,
	"exponentiation":                  1,
	"numeric-separator-literal":       1,
	"regexp-dotall":                   16,
	"regexp-duplicate-named-groups":   13,
	"regexp-lookbehind":               18,
	"regexp-match-indices":            30,
	"regexp-modifiers":                230,
	"regexp-named-groups":             98,
	"regexp-unicode-property-escapes": 669,
	"regexp-v-flag":                   181,
	"u180e":                           8,
}

func TestTest262RegExpFeatureAccounting(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directories := []string{
		filepath.Join(root, "test", "built-ins", "RegExp"),
		filepath.Join(root, "test", "language", "literals", "regexp"),
	}
	counts := map[string]int{}
	for _, directory := range directories {
		for _, path := range test262JavaScriptPaths(t, directory) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", path, err)
			}
			headerEnd := strings.Index(string(source), "---*/")
			if headerEnd < 0 {
				t.Fatalf("%s: missing Test262 metadata header", path)
			}
			for _, line := range strings.Split(string(source[:headerEnd]), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "features:") {
					continue
				}
				features := strings.TrimSpace(strings.TrimPrefix(line, "features:"))
				features = strings.Trim(features, "[]")
				for _, feature := range strings.Split(features, ",") {
					feature = strings.TrimSpace(feature)
					if feature != "" {
						counts[feature]++
					}
				}
			}
		}
	}
	if len(counts) != len(test262RegExpFeatureCounts) {
		t.Fatalf("Test262 RegExp feature tags = %v; want exact register %v", counts, test262RegExpFeatureCounts)
	}
	for feature, want := range test262RegExpFeatureCounts {
		if got := counts[feature]; got != want {
			t.Errorf("Test262 feature %q files = %d; want %d", feature, got, want)
		}
	}
}

func TestTest262BuiltInNegativeRegExpLiteralSyntax(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directory := filepath.Join(root, "test", "built-ins", "RegExp")
	paths := test262JavaScriptPaths(t, directory)
	if len(paths) != test262BuiltInFileCount {
		t.Fatalf(
			"Test262 built-in file count = %d, want %d",
			len(paths),
			test262BuiltInFileCount,
		)
	}

	negativeFiles := 0
	negativeLiterals := 0
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		negative := isNegativeParseTest262(source)
		if !negative {
			continue
		}
		negativeFiles++
		literals := extractTest262RegExpLiterals(source)
		for _, literal := range literals {
			negativeLiterals++
			_, err := ecmascript.Compile(
				literal.pattern,
				literal.flags,
				ecmascript.DefaultCompileOptions(),
			)
			if err == nil {
				t.Errorf("%s: accepted negative literal /%s/%s", path, literal.pattern, literal.flags)
			}
		}
	}
	if negativeFiles != test262BuiltInNegativeParseCount {
		t.Fatalf(
			"Test262 negative built-in files = %d, want %d",
			negativeFiles,
			test262BuiltInNegativeParseCount,
		)
	}
	if negativeLiterals != test262BuiltInNegativeParseCount {
		t.Fatalf(
			"Test262 negative built-in literals = %d, want %d",
			negativeLiterals,
			test262BuiltInNegativeParseCount,
		)
	}
}

func TestTest262RegExpSemantics(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}

	bridge := filepath.Join(t.TempDir(), "test262bridge")
	build := exec.Command("go", "build", "-o", bridge, "./internal/cmd/test262bridge")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Test262 bridge: %v\n%s", err, output)
	}

	run := exec.Command("node", "scripts/test262-runner.cjs", root, bridge)
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run Test262 semantics: %v\n%s", err, output)
	}
	var summary struct {
		Files               int `json:"files"`
		MatcherFiles        int `json:"matcherFiles"`
		BuiltInFiles        int `json:"builtInFiles"`
		BuiltInMatcherFiles int `json:"builtInMatcherFiles"`
		Calls               int `json:"calls"`
	}
	if err := json.Unmarshal(output, &summary); err != nil {
		t.Fatalf("decode Test262 summary: %v\n%s", err, output)
	}
	if summary.Files != test262SemanticFileCount {
		t.Fatalf(
			"Test262 semantic file count = %d, want %d",
			summary.Files,
			test262SemanticFileCount,
		)
	}
	if summary.MatcherFiles != test262MatcherFileCount {
		t.Fatalf(
			"Test262 matcher file count = %d, want %d",
			summary.MatcherFiles,
			test262MatcherFileCount,
		)
	}
	if summary.BuiltInFiles != test262BuiltInSelectedFileCount {
		t.Fatalf(
			"Test262 selected built-in files = %d, want %d",
			summary.BuiltInFiles,
			test262BuiltInSelectedFileCount,
		)
	}
	if summary.BuiltInMatcherFiles != test262BuiltInMatcherFileCount {
		t.Fatalf(
			"Test262 built-in matcher files = %d, want %d",
			summary.BuiltInMatcherFiles,
			test262BuiltInMatcherFileCount,
		)
	}
	if summary.Calls < summary.MatcherFiles {
		t.Fatalf(
			"Test262 matcher calls = %d, want at least one per matcher file (%d)",
			summary.Calls,
			summary.MatcherFiles,
		)
	}
}
