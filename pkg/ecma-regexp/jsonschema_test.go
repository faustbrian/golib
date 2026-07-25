package ecmascript

import (
	"context"
	"errors"
	"testing"
)

func TestJSONSchemaPatternIsUnicodeAndUnanchored(t *testing.T) {
	t.Parallel()

	pattern, err := CompileJSONSchemaPattern(`es`, DefaultCompileOptions())
	if err != nil {
		t.Fatalf("CompileJSONSchemaPattern() error = %v", err)
	}

	matched, err := pattern.Match(context.Background(), "expression", DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if !matched {
		t.Fatal("Match() = false, want true for an unanchored JSON Schema pattern")
	}
	if !pattern.Program().Flags().Unicode() {
		t.Fatal("JSON Schema profile must process patterns in Unicode mode")
	}
}

func TestJSONSchemaPatternHonorsExplicitAnchors(t *testing.T) {
	t.Parallel()

	pattern, err := CompileJSONSchemaPattern(`^es$`, DefaultCompileOptions())
	if err != nil {
		t.Fatalf("CompileJSONSchemaPattern() error = %v", err)
	}

	for input, want := range map[string]bool{"es": true, "expression": false} {
		matched, matchErr := pattern.Match(context.Background(), input, DefaultMatchOptions())
		if matchErr != nil {
			t.Fatalf("Match(%q) error = %v", input, matchErr)
		}
		if matched != want {
			t.Errorf("Match(%q) = %t, want %t", input, matched, want)
		}
	}
}

func TestJSONSchemaPatternUsesCodePointSemantics(t *testing.T) {
	t.Parallel()

	pattern, err := CompileJSONSchemaPattern(`^.$`, DefaultCompileOptions())
	if err != nil {
		t.Fatalf("CompileJSONSchemaPattern() error = %v", err)
	}

	matched, err := pattern.Match(context.Background(), "🙂", DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if !matched {
		t.Fatal("Match() = false, want one astral code point to match dot")
	}
}

func TestJSONSchemaPatternRejectsNonUnicodeLegacySyntax(t *testing.T) {
	t.Parallel()

	_, err := CompileJSONSchemaPattern(`\a`, DefaultCompileOptions())
	if err == nil {
		t.Fatal("CompileJSONSchemaPattern() error = nil, want Unicode syntax error")
	}
	var syntaxError *SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("error type = %T, want *SyntaxError", err)
	}
}

func TestJSONSchemaPatternPropagatesExecutionLimits(t *testing.T) {
	t.Parallel()

	pattern, err := CompileJSONSchemaPattern(`(a+)+$`, DefaultCompileOptions())
	if err != nil {
		t.Fatalf("CompileJSONSchemaPattern() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.Steps = 8

	_, err = pattern.Match(context.Background(), "aaaaaaaa!", options)
	var limitError *LimitError
	if !errors.As(err, &limitError) || limitError.Kind != LimitMatchSteps {
		t.Fatalf("Match() error = %v, want match-step LimitError", err)
	}
}

func TestJSONSchemaPatternProgramCannotBeMutatedThroughAccessor(t *testing.T) {
	t.Parallel()

	pattern, err := CompileJSONSchemaPattern(`(?<word>es)`, DefaultCompileOptions())
	if err != nil {
		t.Fatalf("CompileJSONSchemaPattern() error = %v", err)
	}
	names := pattern.Program().CaptureNameIndices()
	names["word"][0] = 99

	if got := pattern.Program().CaptureNameIndices()["word"][0]; got != 1 {
		t.Fatalf("capture index = %d, want immutable value 1", got)
	}
}
