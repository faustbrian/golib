package rules_test

import (
	"errors"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestStringRules(t *testing.T) {
	ctx := contextFor(t)
	tests := []struct {
		name      string
		validator validation.Validator[string]
		value     string
		valid     bool
		code      string
	}{
		{"bytes min", rules.ByteLength(2, 4), "é", true, "byte_length"},
		{"bytes max", rules.ByteLength(1, 1), "é", false, "byte_length"},
		{"runes unicode", rules.RuneLength(1, 1), "é", true, "rune_length"},
		{"runes max", rules.RuneLength(0, 1), "ab", false, "rune_length"},
		{"prefix", rules.Prefix("go"), "gopher", true, "prefix"},
		{"prefix fail", rules.Prefix("go"), "rust", false, "prefix"},
		{"suffix", rules.Suffix("pher"), "gopher", true, "suffix"},
		{"suffix fail", rules.Suffix("go"), "gopher", false, "suffix"},
		{"one of", rules.OneOf("red", "green"), "green", true, "one_of"},
		{"one of fail", rules.OneOf("red", "green"), "blue", false, "one_of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.validator.Validate(ctx, tt.value)
			if got := report.Empty(); got != tt.valid {
				t.Fatalf("Empty() = %v, want %v (%v)", got, tt.valid, report)
			}
			if !tt.valid && !report.HasCode(tt.code) {
				t.Fatalf("missing code %q", tt.code)
			}
		})
	}
}

func TestPatternIsPrecompiledAndBounded(t *testing.T) {
	limits := validation.DefaultLimits()
	validator, err := rules.Pattern("^[a-z]+$", limits)
	if err != nil {
		t.Fatalf("Pattern() error = %v", err)
	}
	if report := validator.Validate(contextFor(t), "abc"); !report.Empty() {
		t.Fatalf("valid pattern = %v", report)
	}
	if report := validator.Validate(contextFor(t), "123"); !report.HasCode("pattern") {
		t.Fatalf("invalid pattern = %v", report)
	}
	if _, err := rules.Pattern("[", limits); err == nil {
		t.Fatal("malformed pattern error = nil")
	}
	limits.MaxRegexPatternLength = 2
	if _, err := rules.Pattern("long", limits); !errors.Is(err, validation.ErrLimitExceeded) {
		t.Fatalf("oversized pattern error = %v", err)
	}
}
