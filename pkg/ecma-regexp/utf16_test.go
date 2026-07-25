package ecmascript_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestNonUnicodeMatchPreservesLoneSurrogate(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(".", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Match(context.Background(), "😀", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	value := result.Full().Value()
	units := value.Units()
	if len(units) != 1 || units[0] != 0xD83D {
		t.Fatalf("UTF-16 units = %04X", units)
	}
	if _, err := value.GoString(); !errors.Is(err, ecmascript.ErrUnpairedSurrogate) {
		t.Fatalf("GoString() error = %v, want ErrUnpairedSurrogate", err)
	}
}

func TestUnicodeMatchReturnsScalarGoString(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(".", "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Match(context.Background(), "😀", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	text, err := result.Full().Value().GoString()
	if err != nil || text != "😀" {
		t.Fatalf("GoString() = %q, %v", text, err)
	}
}

func TestUnicodeEscapesPreserveSurrogateSemantics(t *testing.T) {
	t.Parallel()

	pair, err := ecmascript.Compile(`^\uD83D\uDE00$`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(pair) error = %v", err)
	}
	_, matched, err := pair.Match(context.Background(), "😀", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(pair) = _, %t, %v", matched, err)
	}

	lone, err := ecmascript.Compile(`^\uD83D$`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(lone) error = %v", err)
	}
	_, matched, err = lone.Match(context.Background(), "�", ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Match(lone) error = %v", err)
	}
	if matched {
		t.Fatal("lone surrogate escape matched U+FFFD")
	}
}

func TestMatchUTF16AcceptsLoneSurrogatesWithoutLoss(t *testing.T) {
	t.Parallel()

	input := ecmascript.UTF16FromUnits([]uint16{0xD83D})
	units := input.Units()
	units[0] = 'x'
	if input.Units()[0] != 0xD83D {
		t.Fatal("UTF16FromUnits() retained caller storage")
	}

	program, err := ecmascript.Compile(`^\uD83D$`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.MatchUTF16(context.Background(), input, ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("MatchUTF16() = _, %t, %v", matched, err)
	}
	span := result.Full().Span()
	if span.Start.UTF16 != 0 || span.End.UTF16 != 1 || span.End.Exact {
		t.Fatalf("MatchUTF16() span = %+v", span)
	}
}

func TestFindUTF16TreatsLoneSurrogateAsCodePointBoundary(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	input := ecmascript.UTF16FromUnits([]uint16{0xD800, 'a'})
	result, matched, err := program.FindUTF16(context.Background(), input, ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("FindUTF16() = _, %t, %v", matched, err)
	}
	if got := result.Full().Span().Start.UTF16; got != 1 {
		t.Fatalf("FindUTF16() start = %d, want 1", got)
	}
}

func TestUTF16OperationsPreserveExactInput(t *testing.T) {
	t.Parallel()

	input := ecmascript.UTF16FromUnits([]uint16{'a', 0xD800, 'b'})
	replacer, err := ecmascript.Compile(`\uD800`, "gu", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(replacer) error = %v", err)
	}
	replaced, err := replacer.ReplaceUTF16(
		context.Background(),
		input,
		ecmascript.UTF16FromString("x"),
		ecmascript.DefaultMatchOptions(),
	)
	if err != nil {
		t.Fatalf("ReplaceUTF16() error = %v", err)
	}
	if got := replaced.Units(); !slices.Equal(got, []uint16{'a', 'x', 'b'}) {
		t.Fatalf("ReplaceUTF16() = %04X", got)
	}

	separator, err := ecmascript.Compile(`\uD800`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(separator) error = %v", err)
	}
	parts, err := separator.SplitUTF16(context.Background(), input, ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("SplitUTF16() error = %v", err)
	}
	if len(parts) != 2 || parts[0].Value().LossyString() != "a" || parts[1].Value().LossyString() != "b" {
		t.Fatalf("SplitUTF16() = %#v", parts)
	}
}

func TestUTF16FindAllAndSessionUseCodeUnitLastIndex(t *testing.T) {
	t.Parallel()

	input := ecmascript.UTF16FromUnits([]uint16{0xD800})
	empty, err := ecmascript.Compile("", "gu", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(empty) error = %v", err)
	}
	results, err := empty.FindAllUTF16(context.Background(), input, ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("FindAllUTF16() error = %v", err)
	}
	if len(results) != 2 || results[0].Full().Span().Start.UTF16 != 0 || results[1].Full().Span().Start.UTF16 != 1 {
		t.Fatalf("FindAllUTF16() = %#v", results)
	}

	sticky, err := ecmascript.Compile(`\uD800`, "yu", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(sticky) error = %v", err)
	}
	session := ecmascript.NewSession(sticky)
	_, matched, err := session.ExecUTF16(context.Background(), input, ecmascript.DefaultMatchOptions().Limits)
	if err != nil || !matched || session.LastIndex() != 1 {
		t.Fatalf("ExecUTF16() = _, %t, %v; lastIndex = %d", matched, err, session.LastIndex())
	}
}

func TestInvalidUTF8MapsEachByteToReplacementCharacter(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`\uFFFD`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Find(
		context.Background(),
		string([]byte{'a', 0xff, 'b'}),
		ecmascript.DefaultMatchOptions(),
	)
	if err != nil || !matched {
		t.Fatalf("Find() = _, %t, %v", matched, err)
	}
	span := result.Full().Span()
	if span.Start.UTF16 != 1 || span.End.UTF16 != 2 ||
		span.Start.Rune != 1 || span.End.Rune != 2 ||
		span.Start.Byte != 1 || span.End.Byte != 2 ||
		!span.Start.Exact || !span.End.Exact {
		t.Fatalf("Find() span = %+v", span)
	}
}
