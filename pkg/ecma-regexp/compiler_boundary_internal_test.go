package ecmascript

import (
	"context"
	"errors"
	"slices"
	"testing"
	"unicode/utf16"
)

func TestCompilerReversesLiteralUnitsAndCodePoints(t *testing.T) {
	t.Parallel()

	nonUnicode := compiler{limit: 10}
	if err := nonUnicode.compile(Node{kind: NodeLiteral, literalUnits: []uint16{'a', 'b', 'c'}}, true); err != nil {
		t.Fatalf("compile(non-Unicode) error = %v", err)
	}
	gotUnits := make([]uint16, len(nonUnicode.code))
	for index, instruction := range nonUnicode.code {
		gotUnits[index] = instruction.value
	}
	if !slices.Equal(gotUnits, []uint16{'c', 'b', 'a'}) {
		t.Fatalf("non-Unicode reverse = %v", gotUnits)
	}

	unicodeCompiler := compiler{limit: 10, flags: Flags{bits: flagUnicode}}
	units := utf16.Encode([]rune{'a', '😀', 'b'})
	if err := unicodeCompiler.compile(Node{kind: NodeLiteral, literalUnits: units}, true); err != nil {
		t.Fatalf("compile(Unicode) error = %v", err)
	}
	gotRunes := make([]rune, len(unicodeCompiler.code))
	for index, instruction := range unicodeCompiler.code {
		gotRunes[index] = instruction.runeValue
	}
	if !slices.Equal(gotRunes, []rune{'b', '😀', 'a'}) {
		t.Fatalf("Unicode reverse = %U", gotRunes)
	}
}

func TestCompilerPreservesNegativeLookaround(t *testing.T) {
	t.Parallel()

	compiled := compiler{limit: 10}
	node := Node{
		kind:     NodeLookaround,
		negated:  true,
		children: []Node{{kind: NodeLiteral, literalUnits: []uint16{'a'}}},
	}
	if err := compiled.compile(node, false); err != nil {
		t.Fatalf("compile() error = %v", err)
	}
	if len(compiled.code) == 0 || compiled.code[0].value&1 == 0 {
		t.Fatalf("lookaround instruction = %#v", compiled.code)
	}

	compiled = compiler{limit: 10}
	node.behind = true
	if err := compiled.compile(node, false); err != nil {
		t.Fatalf("compile(negative lookbehind) error = %v", err)
	}
	if len(compiled.code) == 0 || compiled.code[0].value != 3 {
		t.Fatalf("negative lookbehind instruction = %#v", compiled.code)
	}
}

func TestCompilerPrefersLongestClassString(t *testing.T) {
	t.Parallel()

	program, err := Compile(`^[\q{a|ab}]$`, "v", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, input := range []string{"a", "ab"} {
		_, matched, matchErr := program.Match(context.Background(), input, DefaultMatchOptions())
		if matchErr != nil || !matched {
			t.Errorf("Match(%q) = _, %t, %v", input, matched, matchErr)
		}
	}
}

func TestStringSetPreservesDistinctUTF16Sequences(t *testing.T) {
	t.Parallel()

	values := [][]uint16{{0x0102}, {0x0002}, {0x0103}, {0x0102, 0x0003}}
	set := stringSet(values)
	if len(set) != len(values) {
		t.Fatalf("stringSet() size = %d, want %d", len(set), len(values))
	}
	for _, want := range values {
		found := false
		for _, got := range set {
			found = found || slices.Equal(got, want)
		}
		if !found {
			t.Errorf("stringSet() lost %04X", want)
		}
	}
}

func TestClassSequenceSetOperationsUseBothOperands(t *testing.T) {
	t.Parallel()

	left := Node{classStrings: [][]uint16{{'a'}, {'b'}}}
	right := Node{classStrings: [][]uint16{{'b'}, {'c'}}}
	tests := []struct {
		name string
		node Node
		want map[uint16]bool
	}{
		{
			name: "union",
			node: Node{classOp: classOperationUnion, children: []Node{left, right}},
			want: map[uint16]bool{'a': true, 'b': true, 'c': true, 'd': false},
		},
		{
			name: "intersection",
			node: Node{classOp: classOperationIntersection, children: []Node{left, right}},
			want: map[uint16]bool{'a': false, 'b': true, 'c': false},
		},
		{
			name: "subtraction",
			node: Node{classOp: classOperationSubtraction, children: []Node{left, right}},
			want: map[uint16]bool{'a': true, 'b': false, 'c': false},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for value, want := range test.want {
				if got := classSequenceMatches(test.node, []uint16{value}, Flags{}); got != want {
					t.Errorf("classSequenceMatches(%q) = %t, want %t", rune(value), got, want)
				}
			}
		})
	}
}

func TestClassSequenceRejectsMultipleCodePointsForCharacterRange(t *testing.T) {
	t.Parallel()

	node := Node{class: []classTerm{{start: 'a', end: 'a'}}}
	if classSequenceMatches(node, []uint16{'a', 'b'}, Flags{}) {
		t.Fatal("two code points matched a one-character range")
	}
}

func TestDecodePatternUnitsConsumesSurrogatePairsOnce(t *testing.T) {
	t.Parallel()

	units := []uint16{0xD83D, 0xDE00, 'a', 0xD800}
	if got, want := decodePatternUnits(units), []rune{'😀', 'a', 0xD800}; !slices.Equal(got, want) {
		t.Fatalf("decodePatternUnits() = %U, want %U", got, want)
	}
}

func TestCompilerQuantifierBoundaries(t *testing.T) {
	t.Parallel()

	program, err := Compile(`^a{1,2}$`, "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for input, want := range map[string]bool{"": false, "a": true, "aa": true, "aaa": false} {
		_, matched, matchErr := program.Match(context.Background(), input, DefaultMatchOptions())
		if matchErr != nil || matched != want {
			t.Errorf("Match(%q) = _, %t, %v; want %t", input, matched, matchErr, want)
		}
	}
}

func TestCaptureBoundsFindsNestedMinimumAndMaximum(t *testing.T) {
	t.Parallel()

	root := Node{kind: NodeConcatenation, children: []Node{
		{kind: NodeGroup, capturing: true, capture: 3},
		{kind: NodeGroup, children: []Node{
			{kind: NodeGroup, capturing: true, capture: 1},
			{kind: NodeGroup, capturing: true, capture: 2},
		}},
	}}
	minimum, maximum, found := captureBounds(root)
	if !found || minimum != 1 || maximum != 3 {
		t.Fatalf("captureBounds() = %d, %d, %t", minimum, maximum, found)
	}
	if minimum, maximum, found = captureBounds(Node{kind: NodeGroup, capture: 99}); found || minimum != 0 || maximum != 0 {
		t.Fatalf("captureBounds(non-capturing) = %d, %d, %t", minimum, maximum, found)
	}
}

func TestCompilerEmitAllowsExactInstructionLimit(t *testing.T) {
	t.Parallel()

	compiled := compiler{limit: 1}
	if index, err := compiled.emit(instruction{op: opMatch}); err != nil || index != 0 {
		t.Fatalf("emit(exact) = %d, %v", index, err)
	}
	_, err := compiled.emit(instruction{op: opMatch})
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitProgramInstructions || limit.Limit != 1 || limit.Used != 2 {
		t.Fatalf("emit(over limit) error = %v", err)
	}
}
