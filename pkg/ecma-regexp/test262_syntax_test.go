package ecmascript_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

const (
	test262LiteralFileCount   = 238
	test262NegativeParseCount = 186
)

var test262LiteralSyntaxSkips = map[string]string{
	"S7.8.5_A1.2_T1.js":                            "asterisk literal is ambiguous with a JS block comment",
	"S7.8.5_A1.2_T2.js":                            "unterminated escape crosses a JS line terminator",
	"S7.8.5_A1.2_T3.js":                            "solidus is part of JS literal tokenization",
	"S7.8.5_A1.2_T4.js":                            "empty literal is ambiguous with a JS comment",
	"S7.8.5_A1.3_T1.js":                            "raw carriage return is JS source tokenization",
	"S7.8.5_A1.3_T3.js":                            "raw line feed is JS source tokenization",
	"S7.8.5_A1.5_T1.js":                            "unterminated class crosses a JS line terminator",
	"S7.8.5_A1.5_T3.js":                            "unterminated class crosses a JS line terminator",
	"S7.8.5_A2.2_T1.js":                            "unterminated escape crosses a JS line terminator",
	"S7.8.5_A2.2_T2.js":                            "solidus is part of JS literal tokenization",
	"S7.8.5_A2.3_T1.js":                            "raw carriage return is JS source tokenization",
	"S7.8.5_A2.3_T3.js":                            "raw line feed is JS source tokenization",
	"S7.8.5_A2.5_T1.js":                            "unterminated class crosses a JS line terminator",
	"S7.8.5_A2.5_T3.js":                            "unterminated class crosses a JS line terminator",
	"early-err-flags-unicode-escape.js":            "flag Unicode escapes are JS source syntax",
	"regexp-first-char-no-line-separator.js":       "raw line separator is JS source tokenization",
	"regexp-first-char-no-paragraph-separator.js":  "raw paragraph separator is JS source tokenization",
	"regexp-source-char-no-line-separator.js":      "raw line separator is JS source tokenization",
	"regexp-source-char-no-paragraph-separator.js": "raw paragraph separator is JS source tokenization",
}

var test262DynamicLiteralTests = map[string]string{
	"7.8.5-1.js":                          "constructs a source string containing a line terminator",
	"7.8.5-1gs.js":                        "tests JS comment tokenization",
	"7.8.5-2gs.js":                        "tests JS comment tokenization",
	"S7.8.5_A1.1_T2.js":                   "constructs source with eval",
	"S7.8.5_A1.3_T2.js":                   "constructs source with eval",
	"S7.8.5_A1.3_T4.js":                   "constructs source with eval",
	"S7.8.5_A1.3_T5.js":                   "constructs source with eval",
	"S7.8.5_A1.3_T6.js":                   "constructs source with eval",
	"S7.8.5_A1.4_T2.js":                   "generates code-unit patterns with eval",
	"S7.8.5_A1.5_T2.js":                   "constructs source with eval",
	"S7.8.5_A1.5_T4.js":                   "constructs source with eval",
	"S7.8.5_A1.5_T5.js":                   "constructs source with eval",
	"S7.8.5_A1.5_T6.js":                   "constructs source with eval",
	"S7.8.5_A2.1_T2.js":                   "constructs source with eval",
	"S7.8.5_A2.3_T2.js":                   "constructs source with eval",
	"S7.8.5_A2.3_T4.js":                   "constructs source with eval",
	"S7.8.5_A2.3_T5.js":                   "constructs source with eval",
	"S7.8.5_A2.3_T6.js":                   "constructs source with eval",
	"S7.8.5_A2.4_T2.js":                   "generates code-unit patterns with eval",
	"S7.8.5_A2.5_T2.js":                   "constructs source with eval",
	"S7.8.5_A2.5_T4.js":                   "constructs source with eval",
	"S7.8.5_A2.5_T5.js":                   "constructs source with eval",
	"S7.8.5_A2.5_T6.js":                   "constructs source with eval",
	"mongolian-vowel-separator-eval.js":   "constructs source with eval",
	"invalid-lone-surrogate-groupname.js": "constructs source with eval",
}

func TestTest262RegExpLiteralNegativeSyntax(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directory := filepath.Join(root, "test", "language", "literals", "regexp")
	paths := make([]string, 0, test262LiteralFileCount)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".js" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	sort.Strings(paths)
	if len(paths) != test262LiteralFileCount {
		t.Fatalf(
			"RegExp literal files = %d, want pinned count %d",
			len(paths),
			test262LiteralFileCount,
		)
	}

	negative := 0
	executed := 0
	skipped := 0
	for _, path := range paths {
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, readErr)
		}
		if !isNegativeParseTest262(source) {
			continue
		}
		negative++
		if reason, skip := test262LiteralSyntaxSkips[filepath.Base(path)]; skip {
			if reason == "" {
				t.Errorf("%s: empty skip reason", filepath.Base(path))
			}
			skipped++
			continue
		}
		pattern, flags, ok := extractTest262RegExpLiteral(source)
		if !ok {
			t.Errorf("%s: RegExp literal could not be extracted", filepath.Base(path))
			continue
		}
		if _, compileErr := ecmascript.Compile(
			pattern,
			flags,
			ecmascript.DefaultCompileOptions(),
		); compileErr == nil {
			t.Errorf(
				"%s: Compile(%q, %q) accepted negative parse test",
				filepath.Base(path),
				pattern,
				flags,
			)
			continue
		}
		executed++
	}
	if negative != test262NegativeParseCount {
		t.Fatalf(
			"negative parse files = %d, want pinned count %d",
			negative,
			test262NegativeParseCount,
		)
	}
	if skipped != len(test262LiteralSyntaxSkips) {
		t.Fatalf(
			"syntax skips = %d, want exact register count %d",
			skipped,
			len(test262LiteralSyntaxSkips),
		)
	}
	if executed+skipped != negative {
		t.Fatalf(
			"negative parse accounting: total=%d executed=%d skipped=%d unaccounted=%d",
			negative,
			executed,
			skipped,
			negative-executed-skipped,
		)
	}
}

func TestTest262RegExpLiteralPositiveSyntax(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directory := filepath.Join(root, "test", "language", "literals", "regexp")
	paths := test262JavaScriptPaths(t, directory)
	if len(paths) != test262LiteralFileCount {
		t.Fatalf(
			"RegExp literal files = %d, want pinned count %d",
			len(paths),
			test262LiteralFileCount,
		)
	}

	positive := 0
	executedFiles := 0
	dynamicFiles := 0
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if isNegativeParseTest262(source) {
			continue
		}
		positive++
		if reason, dynamic := test262DynamicLiteralTests[filepath.Base(path)]; dynamic {
			if reason == "" {
				t.Errorf("%s: empty dynamic reason", filepath.Base(path))
			}
			dynamicFiles++
			continue
		}
		literals := extractTest262RegExpLiterals(source)
		if len(literals) == 0 {
			t.Errorf("%s: unaccounted dynamic test", filepath.Base(path))
			continue
		}
		filePassed := true
		for _, literal := range literals {
			if _, compileErr := ecmascript.Compile(
				literal.pattern,
				literal.flags,
				ecmascript.DefaultCompileOptions(),
			); compileErr != nil {
				t.Errorf(
					"%s: Compile(%q, %q) error = %v",
					filepath.Base(path),
					literal.pattern,
					literal.flags,
					compileErr,
				)
				filePassed = false
			}
		}
		if filePassed {
			executedFiles++
		}
	}
	if positive != test262LiteralFileCount-test262NegativeParseCount {
		t.Fatalf("positive files = %d", positive)
	}
	if dynamicFiles != len(test262DynamicLiteralTests) {
		t.Fatalf(
			"dynamic files = %d, want exact register count %d",
			dynamicFiles,
			len(test262DynamicLiteralTests),
		)
	}
	if executedFiles+dynamicFiles != positive {
		t.Fatalf(
			"positive syntax accounting: total=%d executed=%d dynamic=%d unaccounted=%d",
			positive,
			executedFiles,
			dynamicFiles,
			positive-executedFiles-dynamicFiles,
		)
	}
}

type test262RegExpLiteral struct {
	pattern string
	flags   string
}

func test262JavaScriptPaths(t *testing.T, directory string) []string {
	t.Helper()
	paths := make([]string, 0, test262LiteralFileCount)
	err := filepath.WalkDir(
		directory,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && filepath.Ext(path) == ".js" {
				paths = append(paths, path)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	sort.Strings(paths)
	return paths
}

func isNegativeParseTest262(source []byte) bool {
	headerEnd := strings.Index(string(source), "---*/")
	if headerEnd < 0 {
		return false
	}
	header := string(source[:headerEnd])
	return strings.Contains(header, "\nnegative:\n") &&
		strings.Contains(header, "phase: parse") &&
		strings.Contains(header, "type: SyntaxError")
}

func extractTest262RegExpLiteral(source []byte) (string, string, bool) {
	literals := extractTest262RegExpLiterals(source)
	if len(literals) == 0 {
		return "", "", false
	}
	return literals[0].pattern, literals[0].flags, true
}

func extractTest262RegExpLiterals(source []byte) []test262RegExpLiteral {
	headerEnd := strings.Index(string(source), "---*/")
	if headerEnd < 0 {
		return nil
	}
	body := source[headerEnd+len("---*/"):]
	literals := make([]test262RegExpLiteral, 0, 2)
	for index := 0; index < len(body); index++ {
		if body[index] != '/' {
			continue
		}
		if test262LinePrefixIsWhitespace(body, index) &&
			index+1 < len(body) &&
			(body[index+1] == '/' || body[index+1] == '*') {
			if body[index+1] == '/' {
				for index < len(body) && body[index] != '\n' {
					index++
				}
				continue
			}
			end := strings.Index(string(body[index+2:]), "*/")
			if end < 0 {
				break
			}
			index += end + 3
			continue
		}
		if canStartTest262RegExp(body, index) {
			pattern, flags, end, ok := scanTest262RegExp(body, index)
			if ok {
				literals = append(literals, test262RegExpLiteral{
					pattern: pattern,
					flags:   flags,
				})
			}
			index = end
			continue
		}
		if index+1 < len(body) && body[index+1] == '/' {
			for index < len(body) && body[index] != '\n' {
				index++
			}
			continue
		}
		if index+1 < len(body) && body[index+1] == '*' {
			end := strings.Index(string(body[index+2:]), "*/")
			if end < 0 {
				break
			}
			index += end + 3
			continue
		}
	}
	return literals
}

func test262LinePrefixIsWhitespace(source []byte, position int) bool {
	for index := position - 1; index >= 0 && source[index] != '\n'; index-- {
		if source[index] != ' ' && source[index] != '\t' && source[index] != '\r' {
			return false
		}
	}
	return true
}

func canStartTest262RegExp(source []byte, slash int) bool {
	for index := slash - 1; index >= 0; index-- {
		if unicode.IsSpace(rune(source[index])) {
			continue
		}
		return strings.ContainsRune("(=,:;!&|?{[", rune(source[index]))
	}
	return true
}

func scanTest262RegExp(
	source []byte,
	start int,
) (string, string, int, bool) {
	escaped := false
	inClass := false
	for index := start + 1; index < len(source); index++ {
		char := source[index]
		if char == '\n' || char == '\r' || char == 0xE2 &&
			index+2 < len(source) &&
			(source[index+1] == 0x80 &&
				(source[index+2] == 0xA8 || source[index+2] == 0xA9)) {
			return "", "", index, false
		}
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '[' {
			inClass = true
			continue
		}
		if char == ']' {
			inClass = false
			continue
		}
		if char != '/' || inClass {
			continue
		}
		flagsEnd := index + 1
		for flagsEnd < len(source) {
			flag := source[flagsEnd]
			if flag < 'A' || flag > 'Z' && flag < 'a' || flag > 'z' {
				break
			}
			flagsEnd++
		}
		return string(source[start+1 : index]),
			string(source[index+1 : flagsEnd]),
			flagsEnd,
			true
	}
	return "", "", len(source), false
}
