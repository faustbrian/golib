package ruleengine

import (
	"context"
	"strings"
	"testing"
)

func TestCompilerRejectsInvalidMetadataAndGlobalBounds(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRules = 2
	limits.MaxTags = 2
	limits.MaxDerivedFacts = 1
	pathA := MustPath("derived", "a")
	pathB := MustPath("derived", "b")
	tests := []struct {
		name string
		set  RuleSet
		code Code
	}{
		{name: "empty set id", set: RuleSet{}, code: CodeInvalidRule},
		{name: "oversized set id", set: RuleSet{ID: strings.Repeat("a", limits.MaxIdentifierBytes+1)}, code: CodeInvalidRule},
		{name: "control namespace", set: RuleSet{ID: "set", Namespace: "bad\nnamespace"}, code: CodeInvalidRule},
		{name: "too many rules", set: RuleSet{ID: "set", Rules: []Rule{{ID: "a", When: True()}, {ID: "b", When: True()}, {ID: "c", When: True()}}}, code: CodeLimitExceeded},
		{name: "duplicate tags", set: RuleSet{ID: "set", Rules: []Rule{{ID: "a", Tags: []string{"x", "x"}, When: True()}}}, code: CodeInvalidRule},
		{name: "empty tag", set: RuleSet{ID: "set", Rules: []Rule{{ID: "a", Tags: []string{""}, When: True()}}}, code: CodeInvalidRule},
		{name: "too many tags", set: RuleSet{ID: "set", Rules: []Rule{{ID: "a", Tags: []string{"x", "y", "z"}, When: True()}}}, code: CodeLimitExceeded},
		{name: "global derived facts", set: RuleSet{ID: "set", Strategy: CollectAll, Rules: []Rule{
			{ID: "a", When: True(), Derive: []Fact{{Path: pathA, Value: Int(1)}}},
			{ID: "b", When: True(), Derive: []Fact{{Path: pathB, Value: Int(1)}}},
		}}, code: CodeLimitExceeded},
	}
	compiler := NewCompiler(limits)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := compiler.Compile(context.Background(), test.set)
			if !IsCode(err, test.code) {
				t.Fatalf("Compile() error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestContextUsesDedicatedFactAndDefinitionBounds(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxFacts = 1
	first := Fact{Path: MustPath("fact", "a"), Value: Int(1)}
	second := Fact{Path: MustPath("fact", "b"), Value: Int(2)}
	if _, err := NewContextWithLimits(limits, first, second); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("NewContextWithLimits() error = %v", err)
	}

	limits.MaxDefinitionBytes = 2
	if _, _, err := ParseJSON([]byte("{}"), limits); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("ParseJSON at exact bound error = %v", err)
	}
	if _, _, err := ParseJSON([]byte("{} "), limits); !IsCode(err, CodeLimitExceeded) {
		t.Fatalf("ParseJSON over bound error = %v", err)
	}
}
