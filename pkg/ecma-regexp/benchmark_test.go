package ecmascript_test

import (
	"context"
	"errors"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func BenchmarkCompileUnicodeProperty(b *testing.B) {
	options := ecmascript.DefaultCompileOptions()
	for b.Loop() {
		program, err := ecmascript.Compile(
			"^(?<word>\\p{Letter}+)$",
			"u",
			options,
		)
		if err != nil || program.CaptureCount() != 1 {
			b.Fatalf("Compile() = %#v, %v", program, err)
		}
	}
}

func BenchmarkFindASCII(b *testing.B) {
	program := benchmarkProgram(b, "(?<word>[A-Za-z]+)", "u")
	options := ecmascript.DefaultMatchOptions()
	ctx := context.Background()
	for b.Loop() {
		result, matched, err := program.Find(ctx, "42 Helsinki 00100", options)
		if err != nil || !matched ||
			result.Full().Value().LossyString() != "Helsinki" {
			b.Fatalf("Find() = %#v, %t, %v", result, matched, err)
		}
	}
}

func BenchmarkMatchAstralUnicode(b *testing.B) {
	program := benchmarkProgram(b, "^\\p{Emoji_Presentation}+$", "u")
	options := ecmascript.DefaultMatchOptions()
	ctx := context.Background()
	for b.Loop() {
		_, matched, err := program.Match(ctx, "😀😀😀😀", options)
		if err != nil || !matched {
			b.Fatalf("Match() = _, %t, %v", matched, err)
		}
	}
}

func BenchmarkAdversarialBudget(b *testing.B) {
	program := benchmarkProgram(b, "(a|aa)*b", "")
	options := ecmascript.DefaultMatchOptions()
	options.Limits.Steps = 100
	ctx := context.Background()
	for b.Loop() {
		_, _, err := program.Match(ctx, "aaaaaaaaaaaaaaaa", options)
		var limitError *ecmascript.LimitError
		if !errors.As(err, &limitError) ||
			limitError.Kind != ecmascript.LimitMatchSteps {
			b.Fatalf("Match() error = %v, want step LimitError", err)
		}
	}
}

func benchmarkProgram(
	b *testing.B,
	pattern string,
	flags string,
) *ecmascript.Program {
	b.Helper()
	program, err := ecmascript.Compile(
		pattern,
		flags,
		ecmascript.DefaultCompileOptions(),
	)
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	return program
}
