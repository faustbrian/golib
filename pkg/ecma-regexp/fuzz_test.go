package ecmascript_test

import (
	"context"

	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func FuzzTokenizeAndParse(f *testing.F) {
	for _, pattern := range []string{"", "(a|ab)+c", "(?<=a)b", "[\\q{ab|}]", "\\uD83D\\uDE00", "(?<π>a)"} {
		f.Add(pattern, "u")
	}
	f.Fuzz(func(t *testing.T, pattern, flags string) {
		parsedFlags, err := ecmascript.ParseFlags(flags)
		if err != nil {
			return
		}
		options := ecmascript.DefaultParseOptions()
		options.Flags = parsedFlags
		options.Limits.PatternBytes = 4 << 10
		if len(pattern) > int(options.Limits.PatternBytes) {
			return
		}
		_, _ = ecmascript.Tokenize(pattern, options)
		_, _ = ecmascript.Parse(pattern, options)
	})
}

func FuzzCompileAndMatch(f *testing.F) {
	f.Add("(a|aa)*b", "", "aaaaaaaa")
	f.Add("(?=(a+))a*b\\1", "", "aaabaaa")
	f.Add("[[a-z]&&[^aeiou]]+", "v", "bcdf")
	f.Fuzz(func(t *testing.T, pattern, flags, input string) {
		options := ecmascript.DefaultCompileOptions()
		options.Parse.Limits.PatternBytes = 4 << 10
		options.Parse.Limits.ASTNodes = 4 << 10
		options.Limits.ProgramInstructions = 8 << 10
		if len(pattern) > int(options.Parse.Limits.PatternBytes) {
			return
		}
		program, err := ecmascript.Compile(pattern, flags, options)
		if err != nil {
			return
		}
		matchOptions := fuzzMatchOptions()
		_, _, _ = program.Match(context.Background(), input, matchOptions)
		_, _, _ = program.Find(context.Background(), input, matchOptions)
	})
}

func FuzzUTF16Matcher(f *testing.F) {
	f.Add("\\uD800", "u", []byte{0xD8, 0x00})
	f.Add(".", "", []byte{0xD8, 0x3D, 0xDE, 0x00})
	f.Fuzz(func(t *testing.T, pattern, flags string, encoded []byte) {
		if len(encoded) > 2<<10 {
			return
		}
		program, err := ecmascript.Compile(pattern, flags, ecmascript.DefaultCompileOptions())
		if err != nil {
			return
		}
		units := make([]uint16, (len(encoded)+1)/2)
		for index := range units {
			units[index] = uint16(encoded[index*2]) << 8
			if index*2+1 < len(encoded) {
				units[index] |= uint16(encoded[index*2+1])
			}
		}
		input := ecmascript.UTF16FromUnits(units)
		options := fuzzMatchOptions()
		_, _, _ = program.FindUTF16(context.Background(), input, options)
		_, _ = program.FindAllUTF16(context.Background(), input, options)
	})
}

func FuzzReplaceAndSplit(f *testing.F) {
	f.Add("(a)?", "g", "aba", "$1")
	f.Add("(?<x>a)", "g", "aa", "$<x>")
	f.Fuzz(func(t *testing.T, pattern, flags, input, replacement string) {
		if len(pattern) > 4<<10 || len(input) > 4<<10 || len(replacement) > 4<<10 {
			return
		}
		program, err := ecmascript.Compile(pattern, flags, ecmascript.DefaultCompileOptions())
		if err != nil {
			return
		}
		options := fuzzMatchOptions()
		_, _ = program.Replace(context.Background(), input, ecmascript.UTF16FromString(replacement), options)
		_, _ = program.Split(context.Background(), input, options)
	})
}

func fuzzMatchOptions() ecmascript.MatchOptions {
	options := ecmascript.DefaultMatchOptions()
	options.Limits.InputBytes = 8 << 10
	options.Limits.InputRunes = 8 << 10
	options.Limits.Steps = 50_000
	options.Limits.Backtracks = 10_000
	options.Limits.StackDepth = 2_000
	options.Limits.RecursionDepth = 64
	options.Limits.Allocations = 20_000
	options.Limits.Results = 2_000
	options.Limits.OutputUTF16 = 8 << 10
	return options
}
