package ecmascript

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestMatcherPrimitiveBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		start int
		end   int
		want  bool
	}{{0, 0, true}, {0, 1, true}, {-1, 0, false}, {0, -1, false}, {-1, -1, false}} {
		if got := captureParticipated(test.start, test.end); got != test.want {
			t.Errorf("captureParticipated(%d, %d) = %t", test.start, test.end, got)
		}
	}
	for steps, want := range map[uint64]bool{0: false, 1: true, 2: false, 255: false, 256: true, 257: false} {
		if got := shouldCheckExecution(steps); got != want {
			t.Errorf("shouldCheckExecution(%d) = %t", steps, got)
		}
	}
	if wallTimeExceeded(time.Second, time.Second) || !wallTimeExceeded(time.Second+1, time.Second) {
		t.Fatal("wall-time boundary is incorrect")
	}

	captures := []int{10, 11, 20, 21, 30, 31}
	clearCapture(captures, 1)
	if !slices.Equal(captures, []int{10, 11, -1, -1, 30, 31}) {
		t.Fatalf("clearCapture() = %v", captures)
	}
	if got := participatingCapture([]int{-1, -1, 2, 3, 4, 5}, []int{0, 2, 1}); got != 2 {
		t.Fatalf("participatingCapture() = %d", got)
	}
	if got := participatingCapture([]int{-1, -1}, []int{0}); got != 0 {
		t.Fatalf("participatingCapture(unmatched) = %d", got)
	}
}

func TestMatcherCodePointWidthBoundaries(t *testing.T) {
	t.Parallel()

	units := []uint16{'a', 0xD83D, 0xDE00, 0xD800, 'b'}
	for _, test := range []struct {
		position int
		at       int
		before   int
	}{{-1, 0, 0}, {0, 1, 0}, {1, 2, 1}, {2, 1, 1}, {3, 1, 2}, {4, 1, 1}, {5, 0, 1}, {6, 0, 0}} {
		if got := codePointWidthAt(units, test.position); got != test.at {
			t.Errorf("codePointWidthAt(%d) = %d; want %d", test.position, got, test.at)
		}
		if got := codePointWidthBefore(units, test.position); got != test.before {
			t.Errorf("codePointWidthBefore(%d) = %d; want %d", test.position, got, test.before)
		}
	}
	char, width, ok := codePointAtUnits(units, 1)
	if !ok || char != '😀' || width != 2 {
		t.Fatalf("codePointAtUnits(pair) = %U, %d, %t", char, width, ok)
	}
	char, width, ok = codePointBeforeUnits(units, 3)
	if !ok || char != '😀' || width != 2 {
		t.Fatalf("codePointBeforeUnits(pair) = %U, %d, %t", char, width, ok)
	}
	for _, position := range []int{-1, len(units)} {
		if _, _, ok := codePointAtUnits(units, position); ok {
			t.Errorf("codePointAtUnits(%d) succeeded", position)
		}
	}
	for _, position := range []int{0, len(units) + 1} {
		if _, _, ok := codePointBeforeUnits(units, position); ok {
			t.Errorf("codePointBeforeUnits(%d) succeeded", position)
		}
	}
}

func TestMatcherCharacterPredicates(t *testing.T) {
	t.Parallel()

	for unit, want := range map[uint16]bool{0xD7FF: false, 0xD800: true, 0xDBFF: true, 0xDC00: false} {
		if got := isHighSurrogate(unit); got != want {
			t.Errorf("isHighSurrogate(%04X) = %t", unit, got)
		}
	}
	for unit, want := range map[uint16]bool{0xDBFF: false, 0xDC00: true, 0xDFFF: true, 0xE000: false} {
		if got := isLowSurrogate(unit); got != want {
			t.Errorf("isLowSurrogate(%04X) = %t", unit, got)
		}
	}
	for unit, want := range map[uint16]bool{'\n': true, '\r': true, 0x2028: true, 0x2029: true, ' ': false} {
		if got := isLineTerminator(unit); got != want {
			t.Errorf("isLineTerminator(%04X) = %t", unit, got)
		}
	}
	for char, want := range map[rune]bool{'/': false, '0': true, '9': true, ':': false} {
		if got := isASCIIDigit(char); got != want {
			t.Errorf("isASCIIDigit(%q) = %t", char, got)
		}
	}
	for char, want := range map[rune]bool{'_': true, '0': true, 'A': true, 'Z': true, 'a': true, 'z': true, '@': false, '[': false, '`': false, '{': false} {
		if got := isASCIIWord(char); got != want {
			t.Errorf("isASCIIWord(%q) = %t", char, got)
		}
	}
	for char, want := range map[rune]bool{'\t': true, '\n': true, '\v': true, '\f': true, '\r': true, 0xFEFF: true, 0x2028: true, 0x2029: true, ' ': true, 'a': false} {
		if got := isECMAScriptSpace(char); got != want {
			t.Errorf("isECMAScriptSpace(%U) = %t", char, got)
		}
	}
	for _, test := range []struct {
		char, start, end rune
		want             bool
	}{{'a', 'a', 'z', true}, {'z', 'a', 'z', true}, {'`', 'a', 'z', false}, {'{', 'a', 'z', false}} {
		if got := runeInRange(test.char, test.start, test.end); got != test.want {
			t.Errorf("runeInRange(%q, %q, %q) = %t", test.char, test.start, test.end, got)
		}
	}
}

func TestMatcherClassHelperBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value []uint16
		char  rune
		want  bool
	}{{[]uint16{'a'}, 'a', true}, {[]uint16{'a'}, 'b', false}, {[]uint16{0xD83D, 0xDE00}, '😀', true}, {[]uint16{'a', 'b'}, 'a', false}} {
		if got := classStringMatches(test.value, test.char); got != test.want {
			t.Errorf("classStringMatches(%v, %U) = %t", test.value, test.char, got)
		}
	}
	for _, test := range []struct {
		term  classTerm
		char  rune
		flags Flags
		want  bool
	}{
		{term: classTerm{start: 'a', end: 'z'}, char: 'a', want: true},
		{term: classTerm{start: 'a', end: 'z', negated: true}, char: 'a'},
		{term: classTerm{builtin: classBuiltinDigit}, char: '9', want: true},
		{term: classTerm{builtin: classBuiltinDigit, negated: true}, char: 'x', want: true},
	} {
		if got := classTermMatches(test.term, test.char, test.flags); got != test.want {
			t.Errorf("classTermMatches(%#v, %U) = %t", test.term, test.char, got)
		}
	}
}

func TestMatcherDirectionalAndAnchorBoundaries(t *testing.T) {
	t.Parallel()

	program, err := Compile(".", "u", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	view, err := makeInputView("a😀\nb", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	executor := newExecutor(context.Background(), program, view, DefaultMatchOptions().Limits)
	if got := executor.anyWidth(1, 1, Flags{}); got != 2 {
		t.Fatalf("anyWidth(forward pair) = %d", got)
	}
	if got := executor.anyWidth(3, -1, Flags{}); got != 2 {
		t.Fatalf("anyWidth(reverse pair) = %d", got)
	}
	if got := executor.anyWidth(3, 1, Flags{}); got != 0 {
		t.Fatalf("anyWidth(line terminator) = %d", got)
	}
	if got := executor.anyWidth(3, 1, Flags{bits: flagDotAll}); got != 1 {
		t.Fatalf("anyWidth(dot-all line terminator) = %d", got)
	}
	lineView, err := makeInputView("a\nbc", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView(line) error = %v", err)
	}
	lineExecutor := newExecutor(context.Background(), program, lineView, DefaultMatchOptions().Limits)
	if got := lineExecutor.anyWidth(2, -1, Flags{}); got != 0 {
		t.Fatalf("anyWidth(reverse line terminator) = %d", got)
	}
	for _, test := range []struct {
		position int
		start    bool
		end      bool
	}{{0, true, false}, {3, false, false}, {4, false, false}, {5, false, true}} {
		if got := executor.atStart(test.position, Flags{}); got != test.start {
			t.Errorf("atStart(%d) = %t", test.position, got)
		}
		if got := executor.atEnd(test.position, Flags{}); got != test.end {
			t.Errorf("atEnd(%d) = %t", test.position, got)
		}
	}
	multiline := Flags{bits: flagMultiline}
	if !executor.atEnd(3, multiline) || !executor.atStart(4, multiline) {
		t.Fatal("multiline anchors reject line boundary")
	}
	if !executor.equal('a', 'a', Flags{}) || executor.equal('a', 'b', Flags{}) ||
		!executor.equal('a', 'A', Flags{bits: flagIgnoreCase}) ||
		executor.equal(0xD800, 0xD800+1, Flags{bits: flagIgnoreCase}) {
		t.Fatal("legacy equality boundaries are incorrect")
	}
	if !executor.equalCodePoint('a', 'a', Flags{}) || executor.equalCodePoint('a', 'A', Flags{}) ||
		!executor.equalCodePoint('a', 'A', Flags{bits: flagIgnoreCase}) {
		t.Fatal("Unicode equality boundaries are incorrect")
	}
}

func TestMatcherBackreferenceAndWordBoundaries(t *testing.T) {
	t.Parallel()

	program, err := Compile("(ab)\\1", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	view, err := makeInputView("abab", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	executor := newExecutor(context.Background(), program, view, DefaultMatchOptions().Limits)
	current := thread{position: 2, captures: []int{0, 4, 0, 2}}
	if width, matched := executor.backreference(current, []int{1}, 1, Flags{}); !matched || width != 2 {
		t.Fatalf("backreference(forward) = %d, %t", width, matched)
	}
	current.position = 4
	if width, matched := executor.backreference(current, []int{1}, -1, Flags{}); !matched || width != 2 {
		t.Fatalf("backreference(reverse) = %d, %t", width, matched)
	}
	current.position = 2
	if width, matched := executor.backreference(current, []int{1}, -1, Flags{}); !matched || width != 2 {
		t.Fatalf("backreference(reverse exact start) = %d, %t", width, matched)
	}
	current.position = 3
	if _, matched := executor.backreference(current, []int{1}, 1, Flags{}); matched {
		t.Fatal("backreference(out of range) matched")
	}
	current.position = 1
	if _, matched := executor.backreference(current, []int{1}, -1, Flags{}); matched {
		t.Fatal("backreference(reverse out of range) matched")
	}
	current.position = 2
	current.captures = []int{0, 4, -1, -1}
	if width, matched := executor.backreference(current, []int{1}, 1, Flags{}); !matched || width != 0 {
		t.Fatalf("backreference(unmatched) = %d, %t", width, matched)
	}
	emojiView, err := makeInputView("😀😀", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView(emoji) error = %v", err)
	}
	emojiExecutor := newExecutor(context.Background(), unicodeProgramForTest(t), emojiView, DefaultMatchOptions().Limits)
	if width, matched := emojiExecutor.unicodeBackreference(2, 0, 2, 1, Flags{}); !matched || width != 2 {
		t.Fatalf("unicodeBackreference(forward) = %d, %t", width, matched)
	}
	if width, matched := emojiExecutor.unicodeBackreference(4, 0, 2, -1, Flags{}); !matched || width != 2 {
		t.Fatalf("unicodeBackreference(reverse) = %d, %t", width, matched)
	}
	if _, matched := emojiExecutor.unicodeBackreference(2, 0, 1, 1, Flags{}); matched {
		t.Fatal("unicodeBackreference(partial capture) matched")
	}

	unicodeProgram, err := Compile(".", "iu", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(unicode) error = %v", err)
	}
	wordView, err := makeInputView("a😀K", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView(word) error = %v", err)
	}
	wordExecutor := newExecutor(context.Background(), unicodeProgram, wordView, DefaultMatchOptions().Limits)
	if !wordExecutor.wordAt(0, false, unicodeProgram.flags) || wordExecutor.wordAt(1, false, unicodeProgram.flags) {
		t.Fatal("forward word classification is incorrect")
	}
	if wordExecutor.wordAt(-1, true, unicodeProgram.flags) || wordExecutor.wordAt(2, true, unicodeProgram.flags) {
		t.Fatal("previous astral word classification is incorrect")
	}
	if !wordExecutor.wordAt(3, false, unicodeProgram.flags) {
		t.Fatal("Unicode folded word classification is incorrect")
	}
}

func unicodeProgramForTest(t *testing.T) *Program {
	t.Helper()
	program, err := Compile(".", "u", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(unicode helper) error = %v", err)
	}
	return program
}

func TestMatcherClassOperationBoundaries(t *testing.T) {
	t.Parallel()

	a := Node{class: []classTerm{{start: 'a', end: 'c'}}}
	b := Node{class: []classTerm{{start: 'b', end: 'd'}}}
	for _, test := range []struct {
		node Node
		char rune
		want bool
	}{
		{node: Node{classOp: classOperationUnion, children: []Node{a, b}}, char: 'a', want: true},
		{node: Node{classOp: classOperationUnion, children: []Node{a, b}}, char: 'd', want: true},
		{node: Node{classOp: classOperationIntersection, children: []Node{a, b}}, char: 'b', want: true},
		{node: Node{classOp: classOperationIntersection, children: []Node{a, b}}, char: 'a'},
		{node: Node{classOp: classOperationSubtraction, children: []Node{a, b}}, char: 'a', want: true},
		{node: Node{classOp: classOperationSubtraction, children: []Node{a, b}}, char: 'b'},
		{node: Node{classOp: classOperationComplement, children: []Node{a}}, char: 'z', want: true},
		{node: Node{classOp: classOperationComplement, children: []Node{a}}, char: 'a'},
		{node: Node{class: a.class, negated: true}, char: 'z', want: true},
		{node: Node{class: a.class, negated: true}, char: 'a'},
	} {
		if got := classNodeMatches(test.node, test.char, Flags{}); got != test.want {
			t.Errorf("classNodeMatches(%#v, %q) = %t", test.node, test.char, got)
		}
	}
}

func TestMatcherExactExecutionLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		limit   func(*MatchLimits)
		kind    LimitKind
	}{
		{pattern: "a|b", limit: func(limits *MatchLimits) { limits.StackDepth = 0 }, kind: LimitStackDepth},
		{pattern: "(?=a)", limit: func(limits *MatchLimits) { limits.RecursionDepth = 0 }, kind: LimitRecursionDepth},
	} {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		options := DefaultMatchOptions()
		test.limit(&options.Limits)
		_, _, err = program.Match(context.Background(), "a", options)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != test.kind || limit.Used != 1 {
			t.Errorf("Match(%q) error = %v", test.pattern, err)
		}
	}
	for _, test := range []struct {
		pattern string
		limit   func(*MatchLimits)
	}{
		{pattern: "a|b", limit: func(limits *MatchLimits) { limits.StackDepth = 1 }},
		{pattern: "(?=a)", limit: func(limits *MatchLimits) { limits.RecursionDepth = 1 }},
	} {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		options := DefaultMatchOptions()
		test.limit(&options.Limits)
		if _, matched, matchErr := program.Match(context.Background(), "a", options); matchErr != nil || !matched {
			t.Errorf("Match(%q, exact limit) = _, %t, %v", test.pattern, matched, matchErr)
		}
	}
	executor := &executor{
		ctx:     context.Background(),
		limits:  DefaultMatchOptions().Limits,
		started: time.Now(),
	}
	executor.limits.Backtracks = 1
	current := thread{}
	stack := []thread{{position: 1}}
	if resumed, err := executor.backtrack(&current, &stack); err != nil || !resumed || current.position != 1 {
		t.Fatalf("backtrack(exact limit) = %t, %v, %#v", resumed, err, current)
	}
	stack = []thread{{position: 2}}
	if _, err := executor.backtrack(&current, &stack); !isLimitKind(err, LimitBacktracks) {
		t.Fatalf("backtrack(over limit) error = %v", err)
	}
	executor.allocations = 0
	executor.limits.Allocations = 2
	if err := executor.allocate(2); err != nil {
		t.Fatalf("allocate(exact limit) error = %v", err)
	}
	if err := executor.allocate(1); !isLimitKind(err, LimitAllocations) {
		t.Fatalf("allocate(over limit) error = %v", err)
	}
	executor.steps = 0
	executor.limits.Steps = 1
	if err := executor.step(); err != nil {
		t.Fatalf("step(exact limit) error = %v", err)
	}
	if err := executor.step(); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("step(over limit) error = %v", err)
	}
}

func TestMatcherPropertyIndexBoundaries(t *testing.T) {
	t.Parallel()

	if propertyTableIndex(1) != 0 || propertyTableIndex(2) != 1 {
		t.Fatal("property table index conversion is incorrect")
	}
	unicodeIgnoreCase := Flags{bits: flagUnicode | flagIgnoreCase}
	for _, test := range []struct {
		term  classTerm
		char  rune
		flags Flags
		want  bool
	}{
		{term: classTerm{property: 1}, char: 'A', want: true},
		{term: classTerm{property: 1, negated: true}, char: 'A'},
		{term: classTerm{property: 1, negated: true}, char: 'Ω', flags: unicodeIgnoreCase, want: true},
	} {
		if got := classTermMatches(test.term, test.char, test.flags); got != test.want {
			t.Errorf("classTermMatches(property %#v, %U) = %t", test.term, test.char, got)
		}
	}
}
