package gs1

import (
	"errors"
	"sync"
	"testing"
)

func TestDefinitionLoadFailuresRemainObservable(t *testing.T) {
	originalSource := definitionsSource
	definitionsSource = func() string { return "malformed" }
	definitionsOnce = sync.Once{}
	definitions = nil
	definitionsErr = nil
	t.Cleanup(func() {
		definitionsSource = originalSource
		definitionsOnce = sync.Once{}
		definitions = nil
		definitionsErr = nil
		_, _ = loadDefinitions()
	})

	if got := DefinitionCount(); got != 0 {
		t.Fatalf("DefinitionCount() = %d, want 0", got)
	}
	if _, err := ParseBracketed("(01)123", ParseLimits{}); !errors.Is(err, ErrInvalidElement) {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	if _, err := ParseRaw("01123", ParseLimits{}); !errors.Is(err, ErrInvalidElement) {
		t.Fatalf("ParseRaw() error = %v", err)
	}
}

func TestDictionaryGrammarSupportsRangesFlagsAndOptionalComponents(t *testing.T) {
	loaded, err := parseDictionary(`
# comment
01 *? N2 # fixed
10-12 ? X..3 [N2] # ranged
`)
	if err != nil {
		t.Fatalf("parseDictionary() error = %v", err)
	}
	if len(loaded) != 4 || !loaded["01"].predefined || loaded["10"].min != 1 ||
		loaded["10"].max != 5 || loaded["12"].title != "ranged" {
		t.Fatalf("definitions = %+v", loaded)
	}
}

func TestDictionaryGrammarSupportsAssociationAttributes(t *testing.T) {
	loaded, err := parseDictionary("01 N2 req=02+03,04 req=05 ex=06,07 # associations")
	if err != nil {
		t.Fatalf("parseDictionary() error = %v", err)
	}
	definition := loaded["01"]
	if len(definition.required) != 2 || len(definition.required[0]) != 2 ||
		len(definition.required[0][0]) != 2 || len(definition.excluded) != 2 {
		t.Fatalf("definition = %+v", definition)
	}
	for _, dictionary := range []string{
		"01 N2 req=", "01 N2 req=0", "01 N2 req=0a", "01 N2 ex=12345",
	} {
		if _, err := parseDictionary(dictionary); !errors.Is(err, ErrInvalidElement) {
			t.Fatalf("parseDictionary(%q) error = %v", dictionary, err)
		}
	}
}

func TestDictionaryGrammarRejectsMalformedEntries(t *testing.T) {
	tests := []string{
		"01",
		"01 ? attribute=value",
		"01 [X2",
		"12-10 N1",
		"a-b N1",
		"1-02 N1",
	}
	for _, dictionary := range tests {
		if _, err := parseDictionary(dictionary); !errors.Is(err, ErrInvalidElement) {
			t.Fatalf("parseDictionary(%q) error = %v", dictionary, err)
		}
	}
}

func TestDictionaryTokenClassificationAndComponents(t *testing.T) {
	if isFlags("") || isFlags("*x") || !isFlags("*?") {
		t.Fatal("isFlags() classification is incorrect")
	}
	if isComponent("") || isComponent("[") || isComponent("A1") ||
		!isComponent("N2") || !isComponent("[Z..8]") {
		t.Fatal("isComponent() classification is incorrect")
	}

	for _, value := range []string{"[X2", "N", "N0", "N..0"} {
		if _, err := parseComponent(value); !errors.Is(err, ErrInvalidElement) {
			t.Fatalf("parseComponent(%q) error = %v", value, err)
		}
	}
	parsed, err := parseComponent("N2,csum")
	if err != nil || parsed.kind != 'N' || parsed.min != 2 || parsed.max != 2 ||
		!parsed.checksum || len(parsed.linters) != 1 || parsed.linters[0] != "csum" {
		t.Fatalf("parseComponent() = %+v, %v", parsed, err)
	}
}

func TestValueValidationCoversSupportedCharacterClasses(t *testing.T) {
	tests := []struct {
		kind  byte
		valid string
		bad   string
	}{
		{kind: 'N', valid: "012", bad: "12A"},
		{kind: 'X', valid: "a Z!", bad: "\x1f"},
		{kind: 'Y', valid: "ABC-./12", bad: "abc"},
		{kind: 'Z', valid: "Az_09-", bad: "."},
		{kind: 'Q', valid: "", bad: "A"},
	}
	for _, tt := range tests {
		if !validCharacters(tt.kind, tt.valid) {
			t.Fatalf("validCharacters(%q, %q) = false", tt.kind, tt.valid)
		}
		if validCharacters(tt.kind, tt.bad) {
			t.Fatalf("validCharacters(%q, %q) = true", tt.kind, tt.bad)
		}
	}

	optionalSuffix := definition{
		min: 1,
		max: 5,
		components: []component{
			{kind: 'X', min: 1, max: 3},
			{kind: 'N', min: 2, max: 2, optional: true},
		},
	}
	if err := validateValue(optionalSuffix, "A"); err != nil {
		t.Fatalf("validateValue(optional omitted) error = %v", err)
	}
	if err := validateValue(optionalSuffix, "ABC12"); err != nil {
		t.Fatalf("validateValue(optional present) error = %v", err)
	}
	for _, value := range []string{"", "ABC123", "AB1x", "ABCD"} {
		if err := validateValue(optionalSuffix, value); !errors.Is(err, ErrInvalidElement) {
			t.Fatalf("validateValue(%q) error = %v", value, err)
		}
	}
	withRequiredSuffix := definition{
		min: 1, max: 5,
		components: []component{
			{kind: 'X', min: 1, max: 3},
			{kind: 'N', min: 2, max: 2},
		},
	}
	if err := validateValue(withRequiredSuffix, "A12"); err != nil {
		t.Fatalf("validateValue(required suffix) error = %v", err)
	}
	insufficient := definition{
		min: 1, max: 5,
		components: []component{
			{kind: 'X', min: 3, max: 3},
			{kind: 'N', min: 3, max: 3},
		},
	}
	if err := validateValue(insufficient, "ABC12"); !errors.Is(err, ErrInvalidElement) {
		t.Fatalf("validateValue(insufficient) error = %v", err)
	}
}

func TestDefinitionMatchingPrefersLongestAI(t *testing.T) {
	loaded := map[string]definition{
		"12":   {ai: "12"},
		"1234": {ai: "1234"},
	}
	definition, length, ok := matchDefinition(loaded, "1234value")
	if !ok || length != 4 || definition.ai != "1234" {
		t.Fatalf("matchDefinition() = (%+v, %d, %t)", definition, length, ok)
	}
	if _, _, ok := matchDefinition(loaded, "9"); ok {
		t.Fatal("matchDefinition() unexpectedly matched")
	}
}
