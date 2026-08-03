package ecmascript

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseAllowsExactPatternByteLimit(t *testing.T) {
	t.Parallel()

	options := DefaultParseOptions()
	options.Limits.PatternBytes = 2
	pattern, err := Parse("é", options)
	if err != nil || pattern.Source() != "é" {
		t.Fatalf("Parse(exact byte limit) = %#v, %v", pattern, err)
	}
	options.Limits.PatternBytes = 1
	_, err = Parse("é", options)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitPatternBytes || limit.Limit != 1 || limit.Used != 2 {
		t.Fatalf("Parse(over byte limit) error = %v", err)
	}
}

func TestScanCaptureMetadataDistinguishesClassesAndGroupKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern     string
		unicodeSets bool
		captures    int
		named       bool
	}{
		{pattern: `(a)(b)`, captures: 2},
		{pattern: `[(a)](b)`, captures: 1},
		{pattern: `][(a)](b)`, captures: 1},
		{pattern: `(?<x>a)`, captures: 1, named: true},
		{pattern: `(?:a)(?=b)(?!c)(?<=d)(?<!e)`},
		{pattern: `[[(a)]](b)`, unicodeSets: true, captures: 1},
	}
	for _, test := range tests {
		options := DefaultParseOptions()
		if test.unicodeSets {
			options.Flags = Flags{bits: flagUnicodeSets}
		}
		tokens, err := Tokenize(test.pattern, options)
		if err != nil {
			t.Fatalf("Tokenize(%q) error = %v", test.pattern, err)
		}
		captures, named := scanCaptureMetadata(tokens, test.unicodeSets)
		if captures != test.captures || named != test.named {
			t.Errorf("scanCaptureMetadata(%q) = %d, %t; want %d, %t", test.pattern, captures, named, test.captures, test.named)
		}
	}
}

func TestCaptureMetadataHandlesEveryTokenBoundary(t *testing.T) {
	t.Parallel()

	character := func(text string) Token { return Token{kind: TokenCharacter, text: text} }
	tests := []struct {
		tokens    []Token
		capturing bool
		named     bool
	}{
		{tokens: nil, capturing: true},
		{tokens: []Token{{kind: TokenLeftParen}}, capturing: true},
		{tokens: []Token{{kind: TokenLeftParen}, character("a")}, capturing: true},
		{tokens: []Token{{kind: TokenLeftParen}, {kind: TokenQuestion}}, capturing: false},
		{tokens: []Token{{kind: TokenLeftParen}, {kind: TokenQuestion}, {kind: TokenEscape}, character("x")}},
		{tokens: []Token{{kind: TokenLeftParen}, {kind: TokenQuestion}, character("="), character("x")}},
		{tokens: []Token{{kind: TokenLeftParen}, {kind: TokenQuestion}, character("<"), character("x")}, capturing: true, named: true},
		{tokens: []Token{{kind: TokenLeftParen}, {kind: TokenQuestion}, character("<"), character("=")}},
	}
	for _, test := range tests {
		capturing, named := captureMetadataAt(test.tokens)
		if capturing != test.capturing || named != test.named {
			t.Errorf("captureMetadataAt(%#v) = %t, %t; want %t, %t", test.tokens, capturing, named, test.capturing, test.named)
		}
	}
}

func TestParseMergesAdjacentLiteralMetadata(t *testing.T) {
	t.Parallel()

	pattern, err := Parse("aé😀", DefaultParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	root := pattern.Root()
	if root.Kind() != NodeLiteral || root.Text() != "aé😀" || root.Span() != (Span{Start: 0, End: 7}) {
		t.Fatalf("literal root = kind %v text %q span %+v", root.Kind(), root.Text(), root.Span())
	}
}

func TestDecimalEscapeBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		input   string
		want    bool
	}{
		{pattern: `^(a)\1$`, input: "aa", want: true},
		{pattern: `^(a)(b)\2$`, input: "abb", want: true},
		{pattern: `^\0$`, input: string([]byte{0}), want: true},
		{pattern: `^[\1]$`, input: string([]byte{1}), want: true},
		{pattern: `^\8$`, input: "8", want: true},
	} {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, matchErr := program.Match(context.Background(), test.input, DefaultMatchOptions())
		if matchErr != nil || matched != test.want {
			t.Errorf("Match(%q, %q) = _, %t, %v; want %t", test.pattern, test.input, matched, matchErr, test.want)
		}
	}
	for _, pattern := range []string{`[\1]`, `\999999999999999999999999999999999999`} {
		if _, err := Compile(pattern, "u", DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q, u) error = nil", pattern)
		} else if strings.Contains(pattern, "999") {
			var syntax *SyntaxError
			if !errors.As(err, &syntax) || syntax.Message != "backreference is too large" {
				t.Errorf("Compile(%q, u) error = %v", pattern, err)
			}
		}
	}
	program, err := Compile(`^(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)\12$`, "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(twelve captures) error = %v", err)
	}
	_, matched, err := program.Match(context.Background(), "abcdefghijkll", DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(twelve captures) = _, %t, %v", matched, err)
	}
	program, err = Compile(`^(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)\1a2$`, "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(decimal escape terminator) error = %v", err)
	}
	_, matched, err = program.Match(context.Background(), "abcdefghijklaa2", DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(decimal escape terminator) = _, %t, %v", matched, err)
	}
	for _, test := range []struct {
		inClass  bool
		value    int
		err      error
		captures int
		want     bool
	}{
		{value: 1, captures: 1, want: true},
		{inClass: true, value: 1, captures: 1},
		{value: 1, err: errors.New("overflow"), captures: 1},
		{value: 0, captures: 1},
		{value: 2, captures: 1},
	} {
		if got := validNumericBackreference(test.inClass, test.value, test.err, test.captures); got != test.want {
			t.Errorf("validNumericBackreference(%t, %d, %v, %d) = %t", test.inClass, test.value, test.err, test.captures, got)
		}
	}
}

func TestControlEscapeBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		input   string
	}{
		{pattern: `^\cA$`, input: string([]byte{1})},
		{pattern: `^\ca$`, input: string([]byte{1})},
		{pattern: `^[\c0]$`, input: string([]byte{16})},
		{pattern: `^[\c_]$`, input: string([]byte{31})},
		{pattern: `^\c0$`, input: `\c0`},
	} {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, matchErr := program.Match(context.Background(), test.input, DefaultMatchOptions())
		if matchErr != nil || !matched {
			t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, test.input, matched, matchErr)
		}
	}
	for _, pattern := range []string{`\c0`, `\cé`} {
		if _, err := Compile(pattern, "u", DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q, u) error = nil", pattern)
		}
	}
}

func TestAnnexBControlEscapePreservesInsertedTokenSpans(t *testing.T) {
	t.Parallel()

	tokens, err := Tokenize(`\c0`, DefaultParseOptions())
	if err != nil {
		t.Fatalf("Tokenize() error = %v", err)
	}
	p := parser{tokens: tokens, options: DefaultParseOptions()}
	node, err := p.escape(false)
	if err != nil {
		t.Fatalf("escape() error = %v", err)
	}
	if node.span != (Span{Start: 0, End: 1}) || p.current().text != "c" || p.current().span != (Span{Start: 1, End: 2}) {
		t.Fatalf("escape() node/token = %#v / %#v", node, p.current())
	}
}

func TestNamedAndIdentityEscapePredicates(t *testing.T) {
	t.Parallel()

	character := func(text string) Token { return Token{kind: TokenCharacter, text: text} }
	p := parser{tokens: []Token{character("<")}, namedCaptureGroups: true}
	if !p.canParseNamedBackreference(false) || p.canParseNamedBackreference(true) {
		t.Fatal("named capture group backreference predicate is incorrect")
	}
	p.namedCaptureGroups = false
	if p.canParseNamedBackreference(false) {
		t.Fatal("legacy unnamed pattern accepts named backreference")
	}
	p.options.Flags = Flags{bits: flagUnicode}
	if !p.canParseNamedBackreference(false) {
		t.Fatal("Unicode pattern rejects named backreference syntax")
	}
	p.tokens[0] = character("x")
	if p.canParseNamedBackreference(false) {
		t.Fatal("named backreference accepted a non-delimiter character")
	}
	p.tokens[0] = Token{kind: TokenEOF}
	if p.canParseNamedBackreference(false) || !namedBackreferenceNeedsIdentifier(false, p.tokens[0]) {
		t.Fatal("EOF named backreference predicate is incorrect")
	}
	if !namedBackreferenceNeedsIdentifier(true, character("<")) || namedBackreferenceNeedsIdentifier(false, character("<")) {
		t.Fatal("class named backreference predicate is incorrect")
	}

	unicode := Flags{bits: flagUnicode}
	unicodeSets := Flags{bits: flagUnicodeSets}
	for _, test := range []struct {
		inClass bool
		flags   Flags
		char    rune
		want    bool
	}{
		{char: '^', flags: unicode, want: true},
		{char: 'a', flags: unicode},
		{inClass: true, char: '!', flags: unicode},
		{inClass: true, char: '!', flags: unicodeSets, want: true},
		{inClass: true, char: 'a', flags: unicodeSets},
	} {
		if got := allowsUnicodeIdentityEscape(test.inClass, test.flags, test.char); got != test.want {
			t.Errorf("allowsUnicodeIdentityEscape(%t, %#v, %q) = %t", test.inClass, test.flags, test.char, got)
		}
	}
}

func TestAnnexBBracesAndBracketsDistinguishQuantifiers(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{`{1}`, `{1,}`, `{1,2}`} {
		if _, err := Parse(pattern, DefaultParseOptions()); err == nil {
			t.Errorf("Parse(%q) error = nil", pattern)
		}
	}
	for _, pattern := range []string{`{x}`, `{1,x}`, `}`, `]`} {
		parsed, err := Parse(pattern, DefaultParseOptions())
		if err != nil || parsed.Root().Text() != pattern {
			t.Errorf("Parse(%q) = %#v, %v", pattern, parsed, err)
		}
	}
	options := DefaultParseOptions()
	options.Flags = Flags{bits: flagUnicode}
	for _, pattern := range []string{`{x}`, `}`, `]`} {
		if _, err := Parse(pattern, options); err == nil {
			t.Errorf("Parse(%q, u) error = nil", pattern)
		}
	}
}

func TestLegacyOctalEscapeUsesThreeDigitsOnlyThroughThree(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		input   string
	}{
		{pattern: `^\377$`, input: "ÿ"},
		{pattern: `^\400$`, input: " 0"},
	} {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, matchErr := program.Match(context.Background(), test.input, DefaultMatchOptions())
		if matchErr != nil || !matched {
			t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, test.input, matched, matchErr)
		}
	}
}

func TestCharacterClassRangeAndLimitBoundaries(t *testing.T) {
	t.Parallel()

	options := DefaultCompileOptions()
	options.Parse.Limits.CharacterClasses = 1
	if _, err := Compile(`[a-a]`, "", options); err != nil {
		t.Fatalf("Compile(exact class limit) error = %v", err)
	}
	options.Parse.Limits.CharacterClasses = 0
	_, err := Compile(`[a]`, "", options)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCharacterClasses || limit.Used != 1 {
		t.Fatalf("Compile(class limit) error = %v", err)
	}
	if _, err := Compile(`[b-a]`, "", DefaultCompileOptions()); err == nil {
		t.Fatal("Compile(descending range) error = nil")
	}

	program, err := Compile(`^[\d-a]+$`, "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(Annex B set range) error = %v", err)
	}
	for _, input := range []string{"1", "-", "a"} {
		_, matched, matchErr := program.Match(context.Background(), input, DefaultMatchOptions())
		if matchErr != nil || !matched {
			t.Errorf("Match(%q) = _, %t, %v", input, matched, matchErr)
		}
	}
	if _, err := Compile(`[\d-a]`, "u", DefaultCompileOptions()); err == nil {
		t.Fatal("Compile(Unicode set range) error = nil")
	}
}

func TestUnicodeSetRangeDepthAndClassBoundaries(t *testing.T) {
	t.Parallel()

	options := DefaultCompileOptions()
	options.Parse.Limits.ASTDepth = 1
	options.Parse.Limits.CharacterClasses = 1
	if _, err := Compile(`[a-a]`, "v", options); err != nil {
		t.Fatalf("Compile(exact Unicode Sets limits) error = %v", err)
	}
	for _, test := range []struct {
		kind   LimitKind
		adjust func(*ParseLimits)
	}{
		{kind: LimitASTDepth, adjust: func(limits *ParseLimits) { limits.ASTDepth = 0 }},
		{kind: LimitCharacterClasses, adjust: func(limits *ParseLimits) { limits.CharacterClasses = 0 }},
	} {
		limited := options
		test.adjust(&limited.Parse.Limits)
		_, err := Compile(`[a]`, "v", limited)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != test.kind || limit.Used != 1 {
			t.Errorf("Compile(Unicode Sets limit %d) error = %v", test.kind, err)
		}
	}
	if _, err := Compile(`[b-a]`, "v", DefaultCompileOptions()); err == nil {
		t.Fatal("Compile(descending Unicode Sets range) error = nil")
	}
}

func TestUnicodeSetParserHelpersDistinguishEveryOperandShape(t *testing.T) {
	t.Parallel()

	operator := func(tokens []Token) classOperation {
		return (&parser{tokens: tokens}).unicodeSetOperator()
	}
	character := func(text string) Token { return Token{kind: TokenCharacter, text: text} }
	if got := operator([]Token{character("&"), character("&")}); got != classOperationIntersection {
		t.Fatalf("unicodeSetOperator(&&) = %v", got)
	}
	if got := operator([]Token{character("-"), character("-")}); got != classOperationSubtraction {
		t.Fatalf("unicodeSetOperator(--) = %v", got)
	}
	for _, tokens := range [][]Token{
		{{kind: TokenEOF}, character("&")},
		{character("&"), {kind: TokenEOF}},
		{character("&"), character("-")},
		{character("!"), character("!")},
	} {
		if got := operator(tokens); got != classOperationNone {
			t.Errorf("unicodeSetOperator(%#v) = %v", tokens, got)
		}
	}
	doubleReserved := func(first, second string) bool {
		return (&parser{tokens: []Token{character(first), character(second)}}).isUnicodeSetDoubleReserved()
	}
	if !doubleReserved("!", "!") || doubleReserved("!", "#") || doubleReserved("a", "a") {
		t.Fatal("Unicode Sets double-reserved predicate is incorrect")
	}

	valid := Node{class: []classTerm{{start: 'a', end: 'a'}}}
	invalid := []Node{
		{classOp: classOperationUnion, class: valid.class},
		{},
		{class: []classTerm{{}, {}}},
		{class: []classTerm{{builtin: classBuiltinDigit}}},
		{class: []classTerm{{property: 1}}},
		{class: valid.class, classStrings: [][]uint16{{'a'}}},
	}
	if !singleClassCharacter(valid) {
		t.Fatal("singleClassCharacter(valid) = false")
	}
	for index, node := range invalid {
		if singleClassCharacter(node) {
			t.Errorf("singleClassCharacter(invalid %d) = true", index)
		}
	}
	for text, want := range map[string]bool{"(": true, "|": true, "a": false, "()": false, "": false} {
		if got := isUnicodeSetReservedSingle(text); got != want {
			t.Errorf("isUnicodeSetReservedSingle(%q) = %t", text, got)
		}
	}
}

func TestUnicodePropertyAndCodePointBoundaries(t *testing.T) {
	t.Parallel()

	valid := []string{`\u{0}`, `\u{D7FF}`, `\u{E000}`, `\u{10FFFF}`, `\x00`, `\xFF`, `\u0000`, `\uFFFF`}
	for _, pattern := range valid {
		if _, err := Compile(pattern, "u", DefaultCompileOptions()); err != nil {
			t.Errorf("Compile(%q) error = %v", pattern, err)
		}
	}
	invalid := []string{`\u{}`, `\u{D800}`, `\u{DFFF}`, `\u{110000}`, `\u{FFFFFFFFFFFFFFFF}`, `\x0`, `\xGG`, `\p{\d}`, `\p{é}`}
	for _, pattern := range invalid {
		if _, err := Compile(pattern, "u", DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q) error = nil", pattern)
		}
	}

	options := DefaultCompileOptions()
	options.Parse.Limits.CharacterClasses = 1
	if _, err := Compile(`\p{ASCII}`, "u", options); err != nil {
		t.Fatalf("Compile(property exact class limit) error = %v", err)
	}
	if _, err := Compile(`\p{RGI_Emoji}`, "v", options); err != nil {
		t.Fatalf("Compile(string property exact class limit) error = %v", err)
	}
	options.Parse.Limits.CharacterClasses = 0
	for _, test := range []struct{ pattern, flags string }{{`\p{ASCII}`, "u"}, {`\p{RGI_Emoji}`, "v"}} {
		_, err := Compile(test.pattern, test.flags, options)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != LimitCharacterClasses || limit.Used != 1 {
			t.Errorf("Compile(%q, %q) error = %v", test.pattern, test.flags, err)
		}
	}
}

func TestUnicodeClassSurrogatePairIsOneCharacter(t *testing.T) {
	t.Parallel()

	program, err := Compile(`^[\uD83D\uDE00]$`, "u", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, matched, err := program.Match(context.Background(), "😀", DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(emoji) = _, %t, %v", matched, err)
	}
	_, matched, err = program.Match(context.Background(), "�", DefaultMatchOptions())
	if err != nil || matched {
		t.Fatalf("Match(replacement) = _, %t, %v", matched, err)
	}
	high := Node{kind: NodeLiteral, literalUnits: []uint16{0xD83D}}
	low := Node{kind: NodeLiteral, literalUnits: []uint16{0xDE00}}
	p := parser{options: DefaultParseOptions(), tokens: []Token{{kind: TokenEscape, text: `\u`}}}
	if p.startsEscapedSurrogatePair(high) {
		t.Fatal("legacy mode starts escaped surrogate pair")
	}
	p.options.Flags = Flags{bits: flagUnicode}
	if !p.startsEscapedSurrogatePair(high) {
		t.Fatal("Unicode mode rejects escaped surrogate pair")
	}
	for _, node := range []Node{{kind: NodeLiteral}, {kind: NodeLiteral, literalUnits: []uint16{'a'}}} {
		if p.startsEscapedSurrogatePair(node) {
			t.Errorf("startsEscapedSurrogatePair(%#v) = true", node)
		}
	}
	p.tokens[0] = Token{kind: TokenCharacter, text: `\u`}
	if p.startsEscapedSurrogatePair(high) {
		t.Fatal("character token starts escaped surrogate pair")
	}
	p.tokens[0] = Token{kind: TokenEscape, text: `\x`}
	if p.startsEscapedSurrogatePair(high) {
		t.Fatal("non-Unicode escape starts surrogate pair")
	}
	if !isEscapedLowSurrogate(low) {
		t.Fatal("low surrogate node is not recognized")
	}
	for _, node := range []Node{{kind: NodeEmpty, literalUnits: low.literalUnits}, {kind: NodeLiteral}, {kind: NodeLiteral, literalUnits: []uint16{'a'}}} {
		if isEscapedLowSurrogate(node) {
			t.Errorf("isEscapedLowSurrogate(%#v) = true", node)
		}
	}
}

func TestParserTokenClassifiersAndPeekBoundaries(t *testing.T) {
	t.Parallel()

	p := parser{tokens: []Token{{kind: TokenCharacter, text: "0"}, {kind: TokenEOF}}}
	if !p.isDecimal(p.tokens[0]) || p.isDecimal(Token{kind: TokenCharacter, text: "/"}) ||
		p.isDecimal(Token{kind: TokenCharacter, text: ":"}) || p.isDecimal(Token{kind: TokenEOF, text: "0"}) {
		t.Fatal("decimal token classification is incorrect")
	}
	for _, char := range []byte{'0', '9', 'a', 'f', 'A', 'F'} {
		if !isHex(char) {
			t.Errorf("isHex(%q) = false", char)
		}
	}
	for _, char := range []byte{'/', ':', '`', 'g', '@', 'G'} {
		if isHex(char) {
			t.Errorf("isHex(%q) = true", char)
		}
	}
	if !isSingleCharacterToken(Token{kind: TokenCharacter, text: "a"}) ||
		isSingleCharacterToken(Token{kind: TokenEscape, text: "a"}) ||
		isSingleCharacterToken(Token{kind: TokenCharacter, text: "é"}) {
		t.Fatal("single character token classification is incorrect")
	}
	if !isSingleHexToken(Token{kind: TokenCharacter, text: "F"}) ||
		isSingleHexToken(Token{kind: TokenEscape, text: "F"}) ||
		isSingleHexToken(Token{kind: TokenCharacter, text: "FF"}) {
		t.Fatal("single hex token classification is incorrect")
	}
	rangeParser := func(tokens ...Token) *parser { return &parser{tokens: tokens} }
	character := func(text string) Token { return Token{kind: TokenCharacter, text: text} }
	if !rangeParser(character("-"), character("a")).startsCharacterClassRange() {
		t.Fatal("character class range start is not recognized")
	}
	for _, candidate := range []*parser{
		rangeParser(Token{kind: TokenEscape, text: "-"}, character("a")),
		rangeParser(character("+"), character("a")),
		rangeParser(character("-"), Token{kind: TokenRightBracket}),
	} {
		if candidate.startsCharacterClassRange() {
			t.Errorf("startsCharacterClassRange(%#v) = true", candidate.tokens)
		}
	}
	if got := p.peek(); got.kind != TokenEOF {
		t.Fatalf("peek(first) = %#v", got)
	}
	p.position = 1
	if got := p.peek(); got.kind != TokenEOF {
		t.Fatalf("peek(last) = %#v", got)
	}
}

func TestResolveBackreferencesTraversesNestedNamedAndNumericReferences(t *testing.T) {
	t.Parallel()

	root := Node{kind: NodeConcatenation, children: []Node{
		{kind: NodeBackreference, capture: 2},
		{kind: NodeGroup, children: []Node{{kind: NodeBackreference, name: "x"}}},
	}}
	if err := resolveBackreferences(&root, 2, map[string][]int{"x": {1, 2}}); err != nil {
		t.Fatalf("resolveBackreferences() error = %v", err)
	}
	if got := root.children[0].backrefs; !slices.Equal(got, []int{2}) {
		t.Fatalf("numeric backreferences = %v", got)
	}
	if got := root.children[1].children[0]; got.capture != 1 || !slices.Equal(got.backrefs, []int{1, 2}) {
		t.Fatalf("named backreference = %#v", got)
	}
	for _, node := range []Node{{kind: NodeBackreference, capture: 3}, {kind: NodeBackreference, name: "missing"}} {
		if err := resolveBackreferences(&node, 2, map[string][]int{"x": {1}}); err == nil {
			t.Errorf("resolveBackreferences(%#v) error = nil", node)
		}
	}
}

func TestNamedCaptureParticipationUsesSharedAlternationChoices(t *testing.T) {
	t.Parallel()

	first := &Node{}
	second := &Node{}
	occurrence := func(choices map[*Node]int) namedCaptureOccurrence {
		return namedCaptureOccurrence{choices: choices}
	}
	if namedCapturesMightBothParticipate(occurrence(map[*Node]int{first: 0}), occurrence(map[*Node]int{first: 1})) {
		t.Fatal("disjoint branches might both participate")
	}
	if !namedCapturesMightBothParticipate(occurrence(map[*Node]int{first: 0}), occurrence(map[*Node]int{first: 0})) {
		t.Fatal("same branch cannot both participate")
	}
	if !namedCapturesMightBothParticipate(occurrence(map[*Node]int{first: 0}), occurrence(map[*Node]int{second: 1})) {
		t.Fatal("independent alternations cannot both participate")
	}
	if _, err := Parse(`(?<x>a)|(?<x>b)|(?<x>c)`, DefaultParseOptions()); err != nil {
		t.Fatalf("Parse(mutually exclusive duplicate names) error = %v", err)
	}
}

func TestGroupAndNodeLimitsAllowExactBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		adjust func(*ParseLimits)
	}{
		{name: "depth", adjust: func(limits *ParseLimits) { limits.ASTDepth = 1 }},
		{name: "captures", adjust: func(limits *ParseLimits) { limits.Captures = 1 }},
		{name: "nodes", adjust: func(limits *ParseLimits) { limits.ASTNodes = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultParseOptions()
			test.adjust(&options.Limits)
			if _, err := Parse("(a)", options); err != nil {
				t.Fatalf("Parse(exact %s limit) error = %v", test.name, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		kind   LimitKind
		adjust func(*ParseLimits)
	}{
		{name: "depth", kind: LimitASTDepth, adjust: func(limits *ParseLimits) { limits.ASTDepth = 0 }},
		{name: "captures", kind: LimitCaptures, adjust: func(limits *ParseLimits) { limits.Captures = 0 }},
		{name: "nodes", kind: LimitASTNodes, adjust: func(limits *ParseLimits) { limits.ASTNodes = 1 }},
	} {
		t.Run(test.name+" exceeded", func(t *testing.T) {
			t.Parallel()
			options := DefaultParseOptions()
			test.adjust(&options.Limits)
			_, err := Parse("(a)", options)
			var limit *LimitError
			if !errors.As(err, &limit) || limit.Kind != test.kind {
				t.Fatalf("Parse(over %s limit) error = %v", test.name, err)
			}
		})
	}
}

func TestInlineModifierBitsPreserveEverySelectedFlag(t *testing.T) {
	t.Parallel()

	pattern, err := Parse(`(?im-s:a)`, DefaultParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	root := pattern.Root()
	if root.Kind() != NodeGroup || root.enableFlags != flagIgnoreCase|flagMultiline || root.disableFlags != flagDotAll {
		t.Fatalf("modifier group = %#v", root)
	}
	for _, pattern := range []string{`(?:a)`, `(?=a)`, `(?!a)`, `(?<=a)`, `(?<!a)`, `(?<x>a)`} {
		if _, err := Parse(pattern, DefaultParseOptions()); err != nil {
			t.Errorf("Parse(%q) error = %v", pattern, err)
		}
	}
}

func TestCaptureNameRuneBoundaries(t *testing.T) {
	t.Parallel()

	for _, char := range []rune{'$', '_', 'A', 'π'} {
		if !validCaptureNameRune(0, char) {
			t.Errorf("validCaptureNameRune(0, %U) = false", char)
		}
	}
	for _, char := range []rune{'0', 0x200C, 0x200D} {
		if !validCaptureNameRune(1, char) {
			t.Errorf("validCaptureNameRune(1, %U) = false", char)
		}
	}
	for _, test := range []struct {
		index int
		char  rune
	}{{0, '0'}, {0, '😀'}, {0, 0xD800}, {1, 0xDFFF}, {1, '-'}} {
		if validCaptureNameRune(test.index, test.char) {
			t.Errorf("validCaptureNameRune(%d, %U) = true", test.index, test.char)
		}
	}
	for char, want := range map[rune]bool{0xD7FF: false, 0xD800: true, 0xDFFF: true, 0xE000: false} {
		if got := isSurrogateRune(char); got != want {
			t.Errorf("isSurrogateRune(%U) = %t; want %t", char, got, want)
		}
	}
}

func TestIdentifierEscapeAndSyntaxMessageBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		escape string
		ok     bool
	}{
		{escape: `\u{10FFFF}`, ok: true},
		{escape: `\u{110000}`},
		{escape: `\u{FFFFFFFFF}`},
	} {
		tokens, err := Tokenize(test.escape, DefaultParseOptions())
		if err != nil {
			t.Fatalf("Tokenize(%q) error = %v", test.escape, err)
		}
		p := parser{tokens: tokens}
		units, _, escapeErr := p.regexpIdentifierEscape()
		if test.ok && (escapeErr != nil || len(units) != 2) {
			t.Errorf("regexpIdentifierEscape(%q) = %v, %v", test.escape, units, escapeErr)
		}
		if !test.ok && escapeErr == nil {
			t.Errorf("regexpIdentifierEscape(%q) error = nil", test.escape)
		}
	}
	for _, length := range []int{160, 161} {
		message := strings.Repeat("x", length)
		err := (&parser{}).syntax(SyntaxUnexpectedToken, Span{}, message)
		var syntax *SyntaxError
		if !errors.As(err, &syntax) || len(syntax.Message) != min(length, 160) {
			t.Errorf("syntax(message length %d) = %v", length, err)
		}
	}
}

func TestUnicodeSetRangeRejectsAStringEndpoint(t *testing.T) {
	t.Parallel()

	_, err := Compile(`[a-\q{ab}]`, "v", DefaultCompileOptions())
	var syntax *SyntaxError
	if !errors.As(err, &syntax) || syntax.Code != SyntaxUnexpectedToken {
		t.Fatalf("Compile(string range endpoint) error = %v", err)
	}
}

func TestQuantifierBoundariesPreserveExactMinimumAndMaximum(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		minimum int
		maximum int
	}{
		{pattern: `a*`, minimum: 0, maximum: -1},
		{pattern: `a+`, minimum: 1, maximum: -1},
		{pattern: `a?`, minimum: 0, maximum: 1},
		{pattern: `a{0}`, minimum: 0, maximum: 0},
		{pattern: `a{1,1}`, minimum: 1, maximum: 1},
		{pattern: `a{1,2}`, minimum: 1, maximum: 2},
		{pattern: `a{1,}`, minimum: 1, maximum: -1},
	} {
		pattern, err := Parse(test.pattern, DefaultParseOptions())
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", test.pattern, err)
		}
		root := pattern.Root()
		if root.Kind() != NodeQuantifier || root.Min() != test.minimum || root.Max() != test.maximum {
			t.Errorf("Parse(%q) quantifier = %v %d..%d", test.pattern, root.Kind(), root.Min(), root.Max())
		}
	}
	if _, err := Parse(`a{2,1}`, DefaultParseOptions()); err == nil {
		t.Fatal("Parse(descending quantifier) error = nil")
	}
}

func TestNodeLiteralRuneDistinguishesUnitShapes(t *testing.T) {
	t.Parallel()

	if got := nodeLiteralRune(Node{literalUnits: []uint16{'a'}}); got != 'a' {
		t.Fatalf("nodeLiteralRune(BMP) = %U", got)
	}
	if got := nodeLiteralRune(Node{literalUnits: []uint16{0xD83D, 0xDE00}}); got != '😀' {
		t.Fatalf("nodeLiteralRune(pair) = %U", got)
	}
	if got := nodeLiteralRune(Node{literalUnits: []uint16{'a', 'b'}}); got != '�' {
		t.Fatalf("nodeLiteralRune(multiple) = %U", got)
	}
}
