package ecmascript

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const test262GeneratedPropertyFileCount = 459
const test262GeneratedStringPropertyFileCount = 28
const test262CharacterClassEscapeFileCount = 12

func TestTest262GeneratedCharacterClassEscapeRanges(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directory := filepath.Join(
		root,
		"test",
		"built-ins",
		"RegExp",
		"CharacterClassEscapes",
	)
	paths := make([]string, 0, test262CharacterClassEscapeFileCount)
	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".js" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	sort.Strings(paths)
	if len(paths) != test262CharacterClassEscapeFileCount {
		t.Fatalf("character class files = %d, want %d", len(paths), test262CharacterClassEscapeFileCount)
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			sourceBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			source := string(sourceBytes)
			start := strings.Index(source, "const str = buildString")
			end := strings.Index(source, "const standard")
			if start < 0 || end < start {
				t.Fatal("fixture has no generated input block")
			}
			ranges := test262Ranges(t, source[start:end])
			patterns := test262CharacterClassPatterns(t, source)
			want := strings.Contains(filepath.Base(path), "-positive-cases.js")
			for _, fixture := range patterns {
				program, compileErr := Compile(
					fixture.pattern,
					fixture.flags,
					DefaultCompileOptions(),
				)
				if compileErr != nil {
					t.Fatalf("Compile(/%s/%s) error = %v", fixture.pattern, fixture.flags, compileErr)
				}
				if len(program.classSets) == 0 {
					t.Fatalf("/%s/%s has no character class", fixture.pattern, fixture.flags)
				}
				for _, span := range ranges {
					for char := span.lo; char <= span.hi; char++ {
						if got := program.classSets[0].matches(char, program.flags); got != want {
							t.Fatalf("/%s/%s matches(%U) = %t, want %t", fixture.pattern, fixture.flags, char, got, want)
						}
					}
				}
			}
		})
	}
}

func TestTest262GeneratedUnicodePropertyRanges(t *testing.T) {
	root := os.Getenv("TEST262_ROOT")
	if root == "" {
		t.Skip("TEST262_ROOT is required for the pinned external corpus")
	}
	directory := filepath.Join(
		root,
		"test",
		"built-ins",
		"RegExp",
		"property-escapes",
		"generated",
	)
	paths := make([]string, 0, test262GeneratedPropertyFileCount)
	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".js" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	sort.Strings(paths)
	if len(paths) != test262GeneratedPropertyFileCount {
		t.Fatalf(
			"generated property files = %d, want %d",
			len(paths),
			test262GeneratedPropertyFileCount,
		)
	}

	stringFiles := 0
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if filepath.Base(filepath.Dir(path)) == "strings" {
				stringFiles++
				return
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			patterns := test262GeneratedPatterns(string(source))
			if len(patterns) == 0 {
				t.Fatal("fixture has no generated patterns")
			}
			tableIndex := -1
			for _, fixture := range patterns {
				program, compileErr := Compile(
					fixture.pattern,
					fixture.flags,
					DefaultCompileOptions(),
				)
				if compileErr != nil {
					t.Fatalf("Compile(/%s/%s) error = %v", fixture.pattern, fixture.flags, compileErr)
				}
				term, ok := test262PropertyTerm(program)
				if !ok {
					t.Fatalf("/%s/%s has no property term", fixture.pattern, fixture.flags)
				}
				current := int(term.property - 1)
				if tableIndex < 0 {
					tableIndex = current
				} else if current != tableIndex {
					t.Fatalf("property alias resolved to table %d, want %d", current, tableIndex)
				}
				if term.negated != fixture.negated {
					t.Fatalf("/%s/%s negation = %t, want %t", fixture.pattern, fixture.flags, term.negated, fixture.negated)
				}
			}

			expected := test262MatchRanges(t, string(source))
			table := generatedUnicodeTables[tableIndex]
			actual := generatedUnicodeRanges[table.offset : table.offset+table.length]
			if len(actual) != len(expected) {
				t.Fatalf("range count = %d, want %d", len(actual), len(expected))
			}
			for index := range actual {
				if actual[index] != expected[index] {
					t.Fatalf("range %d = %#v, want %#v", index, actual[index], expected[index])
				}
			}
		})
	}
	if stringFiles != test262GeneratedStringPropertyFileCount {
		t.Fatalf(
			"generated string property files = %d, want %d",
			stringFiles,
			test262GeneratedStringPropertyFileCount,
		)
	}
}

type test262GeneratedPattern struct {
	pattern string
	flags   string
	negated bool
}

func test262GeneratedPatterns(source string) []test262GeneratedPattern {
	patterns := make([]test262GeneratedPattern, 0, 8)
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		start := strings.Index(line, "/")
		if start < 0 ||
			(!strings.Contains(line[start:], `\p{`) &&
				!strings.Contains(line[start:], `\P{`)) {
			continue
		}
		end := strings.LastIndex(line, "/")
		if end <= start {
			continue
		}
		flagsEnd := end + 1
		for flagsEnd < len(line) && strings.ContainsRune("dgimsuvy", rune(line[flagsEnd])) {
			flagsEnd++
		}
		flags := line[end+1 : flagsEnd]
		pattern := line[start+1 : end]
		patterns = append(patterns, test262GeneratedPattern{
			pattern: pattern,
			flags:   flags,
			negated: strings.Contains(pattern, `\P{`),
		})
	}
	return patterns
}

func test262CharacterClassPatterns(t *testing.T, source string) []test262GeneratedPattern {
	t.Helper()
	patterns := make([]test262GeneratedPattern, 0, 3)
	for _, name := range []string{"standard", "unicode", "vflag"} {
		marker := "const " + name + " = /"
		start := strings.Index(source, marker)
		if start < 0 {
			t.Fatalf("fixture has no %s pattern", name)
		}
		start += len(marker)
		lineEnd := strings.IndexByte(source[start:], '\n')
		if lineEnd < 0 {
			t.Fatalf("fixture has no %s pattern terminator", name)
		}
		line := source[start : start+lineEnd]
		end := strings.LastIndex(line, "/")
		if end < 0 {
			t.Fatalf("fixture has no %s closing slash", name)
		}
		flags := strings.TrimSuffix(line[end+1:], ";")
		patterns = append(patterns, test262GeneratedPattern{
			pattern: line[:end],
			flags:   flags,
		})
	}
	return patterns
}

func test262PropertyTerm(program *Program) (classTerm, bool) {
	for _, class := range program.classSets {
		if term, ok := test262PropertyNodeTerm(class.node); ok {
			return term, true
		}
	}
	return classTerm{}, false
}

func test262PropertyNodeTerm(node Node) (classTerm, bool) {
	for _, term := range node.class {
		if term.property > 0 {
			return term, true
		}
	}
	for _, child := range node.children {
		if term, ok := test262PropertyNodeTerm(child); ok {
			return term, true
		}
	}
	return classTerm{}, false
}

func test262MatchRanges(t *testing.T, source string) []unicodeRange {
	t.Helper()
	start := strings.Index(source, "const matchSymbols")
	end := strings.Index(source, "const nonMatchSymbols")
	if end < 0 {
		end = len(source)
	}
	if start < 0 || end < start {
		t.Fatal("fixture has no matchSymbols block")
	}
	return test262Ranges(t, source[start:end])
}

func test262Ranges(t *testing.T, block string) []unicodeRange {
	t.Helper()
	loneStart := strings.Index(block, "loneCodePoints: [")
	rangesStart := strings.Index(block, "ranges: [")
	if loneStart < 0 || rangesStart < 0 || rangesStart < loneStart {
		t.Fatal("matchSymbols block has no range data")
	}
	loneValues := test262HexValues(block[loneStart:rangesStart])
	rangeValues := test262HexValues(block[rangesStart:])
	if len(rangeValues)%2 != 0 {
		t.Fatalf("range endpoint count = %d, want even", len(rangeValues))
	}
	ranges := make([]unicodeRange, 0, len(loneValues)+len(rangeValues)/2)
	for _, value := range loneValues {
		ranges = append(ranges, unicodeRange{lo: value, hi: value})
	}
	for index := 0; index < len(rangeValues); index += 2 {
		ranges = append(ranges, unicodeRange{lo: rangeValues[index], hi: rangeValues[index+1]})
	}
	sort.Slice(ranges, func(left, right int) bool {
		return ranges[left].lo < ranges[right].lo
	})
	merged := make([]unicodeRange, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) > 0 && current.lo <= merged[len(merged)-1].hi+1 {
			merged[len(merged)-1].hi = max(merged[len(merged)-1].hi, current.hi)
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func test262HexValues(source string) []rune {
	values := make([]rune, 0, 32)
	for offset := 0; offset+2 < len(source); offset++ {
		if source[offset] != '0' || source[offset+1] != 'x' {
			continue
		}
		end := offset + 2
		for end < len(source) && isHex(source[end]) {
			end++
		}
		value, err := strconv.ParseUint(source[offset+2:end], 16, 32)
		if err == nil {
			values = append(values, rune(value))
		}
		offset = end - 1
	}
	return values
}
