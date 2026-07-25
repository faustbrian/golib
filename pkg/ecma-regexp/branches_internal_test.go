package ecmascript

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestParserDiagnosticBranches(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		pattern string
		flags   string
		annexB  bool
	}{
		{pattern: "a)"},
		{pattern: "{1}", flags: "u"},
		{pattern: "}", flags: "u"},
		{pattern: "]", flags: "u"},
		{pattern: "[\\B]"},
		{pattern: "[\\1]", flags: "u"},
		{
			pattern: "\\999999999999999999999999999999999999999",
			flags:   "u",
		},
		{pattern: "\\u{41}"},
		{pattern: "\\c1", flags: "u"},
		{pattern: "(?<x>a)\\k<>", flags: "u"},
		{pattern: "(?<x>a)\\k<x", flags: "u"},
		{pattern: "[\\d-a]", flags: "u"},
		{pattern: "[a-\\d]", flags: "u"},
		{pattern: "[[a]&&]", flags: "v"},
		{pattern: "[&&[a]]", flags: "v"},
		{pattern: "[!!]", flags: "v"},
		{pattern: "[\\q]", flags: "v"},
		{pattern: "[\\q{]", flags: "v"},
		{pattern: "[\\q{(}]", flags: "v"},
		{pattern: "[\\q{\\d}]", flags: "v"},
		{pattern: "[\\q{a}-b]", flags: "v"},
		{pattern: "[z-a]", flags: "v"},
		{pattern: "[^\\q{ab}]", flags: "v"},
		{pattern: "\\p", flags: "u"},
		{pattern: "\\p{}", flags: "u"},
		{pattern: "\\p{A\\u}", flags: "u"},
		{pattern: "\\P{RGI_Emoji}", flags: "v"},
		{pattern: "\\u{}", flags: "u"},
		{pattern: "\\u{g}", flags: "u"},
		{pattern: "\\u{1234567}", flags: "u"},
		{pattern: "(?)"},
		{pattern: "(?--i:a)"},
		{pattern: "(?-:a)"},
		{pattern: "(?<\\x61>a)", flags: "u"},
		{pattern: "(?<\\u{}>a)", flags: "u"},
		{pattern: "(?<\\u{g}>a)", flags: "u"},
		{pattern: "(?<\\u{1234567}>a)", flags: "u"},
		{pattern: "(?<\\u{110000}>a)", flags: "u"},
		{pattern: "(?<\\u0>a)", flags: "u"},
		{pattern: "a{1x", flags: "u"},
		{
			pattern: "a{999999999999999999999999999999999}",
			flags:   "u",
		},
	}
	for _, test := range invalid {
		options := DefaultCompileOptions()
		options.Parse.AnnexB = test.annexB
		if _, err := Compile(test.pattern, test.flags, options); err == nil {
			t.Errorf("Compile(%q, %q) error = nil", test.pattern, test.flags)
		}
	}

	valid := []struct {
		pattern string
		flags   string
	}{
		{pattern: "[\\b]"},
		{pattern: "{x"},
		{pattern: "{1x"},
		{pattern: "{1,"},
		{pattern: "{1,2x"},
		{pattern: "\\u{1F600}", flags: "u"},
		{pattern: "(?<\\u0061>a)", flags: "u"},
		{pattern: "(?<\\u{10400}>a)", flags: "u"},
		{pattern: "[]", flags: "v"},
	}
	for _, test := range valid {
		if _, err := Compile(
			test.pattern,
			test.flags,
			DefaultCompileOptions(),
		); err != nil {
			t.Errorf("Compile(%q, %q) error = %v", test.pattern, test.flags, err)
		}
	}
}

func TestParserInternalDefensiveBranches(t *testing.T) {
	t.Parallel()

	eof := Token{kind: TokenEOF}
	parser := parser{
		options: DefaultParseOptions(),
		tokens:  []Token{eof},
	}
	if _, err := parser.atom(0); err == nil {
		t.Fatal("atom(EOF) error = nil")
	}
	if parser.peek().kind != TokenEOF {
		t.Fatal("peek() did not retain EOF")
	}
	if got := parser.syntax(
		SyntaxUnexpectedToken,
		Span{},
		strings.Repeat("x", 200),
	).(*SyntaxError).Message; len(got) != 160 {
		t.Fatalf("bounded diagnostic length = %d", len(got))
	}

	if got := nodeLiteralRune(Node{
		literalUnits: utf16.Encode([]rune{'😀'}),
	}); got != '😀' {
		t.Fatalf("nodeLiteralRune(astral) = %U", got)
	}
	if got := nodeLiteralRune(Node{
		literalUnits: []uint16{'a', 'b'},
	}); got != utf8.RuneError {
		t.Fatalf("nodeLiteralRune(multiple) = %U", got)
	}

	root := Node{
		kind: NodeGroup,
		children: []Node{{
			kind:    NodeBackreference,
			capture: 2,
		}},
	}
	if err := resolveBackreferences(&root, 1, nil); err == nil {
		t.Fatal("nested invalid backreference error = nil")
	}
}

func TestClassStringAndCharacterBranches(t *testing.T) {
	t.Parallel()

	a := Node{
		kind:         NodeCharacterClass,
		classStrings: [][]uint16{{'a'}},
		class:        []classTerm{{start: 'a', end: 'z'}},
	}
	b := Node{
		kind:         NodeCharacterClass,
		classStrings: [][]uint16{{'b'}},
		class:        []classTerm{{start: 'b', end: 'b'}},
	}
	tests := []struct {
		node  Node
		value []uint16
		want  bool
	}{
		{
			node: Node{
				classOp:  classOperationUnion,
				children: []Node{a, b},
			},
			value: []uint16{'b'},
			want:  true,
		},
		{
			node: Node{
				classOp:  classOperationIntersection,
				children: []Node{a, b},
			},
			value: []uint16{'b'},
			want:  true,
		},
		{
			node: Node{
				classOp:  classOperationComplement,
				children: []Node{a},
			},
			value: []uint16{'a'},
		},
		{node: a, value: []uint16{'c'}, want: true},
		{node: a, value: []uint16{0xD800}},
	}
	for index, test := range tests {
		if got := classSequenceMatches(
			test.node,
			test.value,
			Flags{},
		); got != test.want {
			t.Errorf("classSequenceMatches(%d) = %t", index, got)
		}
	}

	vOptions := DefaultParseOptions()
	vOptions.Flags, _ = ParseFlags("v")
	pattern, err := Parse("[\\q{ab|cd}]", vOptions)
	if err != nil {
		t.Fatalf("Parse(class strings) error = %v", err)
	}
	if len(pattern.Root().ClassStrings()) == 0 {
		t.Fatal("ClassStrings() = empty")
	}

	if !classNodeMatches(a, 'a', Flags{}) {
		t.Fatal("class string did not match one code point")
	}
}

func TestMatcherBacktrackFailureBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		flags   string
		input   string
		start   int
	}{
		{pattern: "(?:😀|x)", flags: "u", input: "x"},
		{pattern: "(?:.|)", input: "\n"},
		{pattern: "(?:^|x)", input: "xx", start: 1},
		{pattern: "(?:$|x)", input: "x"},
		{pattern: "(?:\\b|.)", input: ""},
		{pattern: "(?:[a]|x)", input: "x"},
		{pattern: "(?:(a)\\1|ab)", input: "ab"},
		{pattern: "(?:(?=a)|x)", input: "x"},
	}
	for _, test := range tests {
		program, err := Compile(test.pattern, test.flags, DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		options := DefaultMatchOptions()
		options.StartUTF16 = test.start
		options.Limits.Backtracks = 0
		_, _, err = program.Match(context.Background(), test.input, options)
		if !isLimitKind(err, LimitBacktracks) {
			t.Errorf("Match(%q) error = %v", test.pattern, err)
		}
	}
}

func TestMatcherInternalBoundaries(t *testing.T) {
	t.Parallel()

	program, err := Compile("(😀)\\1", "u", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	view, err := makeInputView("😀😀", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	var nilContext context.Context
	executor := newExecutor(
		nilContext,
		program,
		view,
		DefaultMatchOptions().Limits,
	)
	if width, matched := executor.unicodeBackreference(
		4,
		0,
		2,
		-1,
		program.flags,
	); !matched || width != 2 {
		t.Fatalf("reverse backreference = %d, %t", width, matched)
	}
	if _, matched := executor.unicodeBackreference(
		0,
		0,
		2,
		-1,
		program.flags,
	); matched {
		t.Fatal("reverse backreference unexpectedly matched itself")
	}

	if _, _, ok := codePointAtUnits(nil, 0); ok {
		t.Fatal("codePointAtUnits(nil) succeeded")
	}
	if width := executor.anyWidth(2, -1, program.flags); width != 2 {
		t.Fatalf("reverse anyWidth = %d", width)
	}
	if executor.wordAt(1, true, program.flags) {
		t.Fatal("unexpected word result")
	}

	captureThread := thread{captures: []int{-1, -1, -1, -1}}
	if width, matched := executor.backreference(
		captureThread,
		[]int{1},
		1,
		program.flags,
	); !matched || width != 0 {
		t.Fatalf("unmatched backreference = %d, %t", width, matched)
	}
}

func TestReplacementOutputFailureBranches(t *testing.T) {
	t.Parallel()

	program, err := Compile("(?<x>a)", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, replacement := range []string{
		"$$",
		"$&",
		"$\x60",
		"$'",
		"$<x>",
		"$1",
		"$3",
		"$x",
	} {
		options := DefaultMatchOptions()
		options.Limits.OutputUTF16 = 0
		_, err := program.Replace(
			context.Background(),
			"zaq",
			UTF16FromString(replacement),
			options,
		)
		var limitError *LimitError
		if !errors.As(err, &limitError) {
			t.Errorf("Replace(%q) error = %v", replacement, err)
		}
	}

	separator, err := Compile(",", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(separator) error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.Results = 0
	options.Limits.OutputUTF16 = 0
	if _, err := separator.Split(
		context.Background(),
		"",
		options,
	); !isLimitKind(err, LimitResults) {
		t.Fatalf("Split(empty) error = %v", err)
	}
	options.Limits.Results = DefaultMatchOptions().Limits.Results
	if _, err := separator.Split(
		context.Background(),
		"abc",
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Split(final) error = %v", err)
	}
}

func TestRemainingParserAndCompilerBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := Compile("a", "x", DefaultCompileOptions()); err == nil {
		t.Fatal("Compile() accepted an unknown flag")
	}
	compiler := compiler{limit: 1}
	if err := compiler.compile(Node{}, false); err == nil {
		t.Fatal("compiler accepted an unknown node kind")
	}

	for _, test := range []struct {
		pattern string
		flags   string
	}{
		{pattern: "[[a][b][c]]", flags: "v"},
		{pattern: "[^a]", flags: "v"},
		{pattern: "[\\q{ab|cd}]", flags: "v"},
		{pattern: "\\p{Letter}", flags: "u"},
		{pattern: "\\p{RGI_Emoji}", flags: "v"},
	} {
		succeeded := false
		for nodes := uint64(0); nodes < 32; nodes++ {
			options := DefaultCompileOptions()
			options.Parse.Limits.ASTNodes = nodes
			_, err := Compile(test.pattern, test.flags, options)
			if err == nil {
				succeeded = true
				break
			}
			var limitError *LimitError
			if !errors.As(err, &limitError) {
				t.Fatalf(
					"Compile(%q) nodes %d error = %v",
					test.pattern,
					nodes,
					err,
				)
			}
		}
		if !succeeded {
			t.Fatalf("Compile(%q) never succeeded", test.pattern)
		}
	}

	for _, test := range []struct {
		pattern string
		flags   string
	}{
		{pattern: "\\p{Letter}", flags: "u"},
		{pattern: "\\p{RGI_Emoji}", flags: "v"},
	} {
		options := DefaultCompileOptions()
		options.Parse.Limits.CharacterClasses = 0
		if _, err := Compile(test.pattern, test.flags, options); !isLimitKind(
			err,
			LimitCharacterClasses,
		) {
			t.Errorf("Compile(%q) error = %v", test.pattern, err)
		}
	}

	options := DefaultCompileOptions()
	options.Parse.Limits.ASTDepth = 0
	if _, err := Compile("[a]", "v", options); !isLimitKind(
		err,
		LimitASTDepth,
	) {
		t.Fatalf("Compile(v depth) error = %v", err)
	}

	for pattern, want := range map[string]bool{
		"{1}":   true,
		"{1,}":  true,
		"{1,2}": true,
		"{1x":   false,
		"{x":    false,
	} {
		tokens, err := Tokenize(pattern, DefaultParseOptions())
		if err != nil {
			t.Fatalf("Tokenize(%q) error = %v", pattern, err)
		}
		parser := parser{
			options: DefaultParseOptions(),
			tokens:  tokens,
		}
		if got := parser.looksLikeInvalidBracedQuantifier(); got != want {
			t.Errorf(
				"looksLikeInvalidBracedQuantifier(%q) = %t",
				pattern,
				got,
			)
		}
	}

	for _, pattern := range []string{
		"(?*:a)",
		"(?x:a)",
		"(?i*:a)",
		"(?ix:a)",
		"[a-\\B]",
		"[a-(]",
	} {
		if _, err := Compile(
			pattern,
			"",
			DefaultCompileOptions(),
		); err == nil {
			t.Errorf("Compile(%q) error = nil", pattern)
		}
	}
	if _, err := Compile(
		"(?<\\u{61}>a)",
		"",
		DefaultCompileOptions(),
	); err != nil {
		t.Fatalf("Compile(braced legacy capture name) error = %v", err)
	}
	for _, pattern := range []string{
		`^[\uD800\uDC00]$`,
		`[\uD800\u0061]`,
	} {
		if _, err := Compile(pattern, "u", DefaultCompileOptions()); err != nil {
			t.Fatalf("Compile(%q, u) error = %v", pattern, err)
		}
	}
	if _, err := Compile(
		`[\uD800\u00G0]`,
		"u",
		DefaultCompileOptions(),
	); err == nil {
		t.Fatal("Compile(malformed low-surrogate escape) error = nil")
	}
	for _, pattern := range []string{
		"[\\a]",
		"[\\q{\\a}]",
		"[[a]&&[(]]",
		"[a-\\a]",
		"[a",
	} {
		if _, err := Compile(
			pattern,
			"v",
			DefaultCompileOptions(),
		); err == nil {
			t.Errorf("Compile(%q, v) error = nil", pattern)
		}
	}
	if _, err := Compile(
		"[\\q{\\-}]",
		"v",
		DefaultCompileOptions(),
	); err != nil {
		t.Fatalf("Compile(escaped class string) error = %v", err)
	}

	strict := DefaultCompileOptions()
	strict.Parse.AnnexB = false
	if _, err := Compile("\\k<x>", "", strict); err == nil {
		t.Fatal("strict named backreference error = nil")
	}

	vClassLimit := DefaultCompileOptions()
	vClassLimit.Parse.Limits.CharacterClasses = 0
	if _, err := Compile("[a]", "v", vClassLimit); !isLimitKind(
		err,
		LimitCharacterClasses,
	) {
		t.Fatalf("Compile(v class limit) error = %v", err)
	}

	for _, pattern := range []string{"[]", "[a&&b]"} {
		succeeded := false
		for nodes := uint64(0); nodes < 8; nodes++ {
			nodeOptions := DefaultCompileOptions()
			nodeOptions.Parse.Limits.ASTNodes = nodes
			_, err := Compile(pattern, "v", nodeOptions)
			if err == nil {
				succeeded = true
				break
			}
			var limitError *LimitError
			if !errors.As(err, &limitError) {
				t.Fatalf(
					"Compile(%q) nodes %d error = %v",
					pattern,
					nodes,
					err,
				)
			}
		}
		if !succeeded {
			t.Fatalf("Compile(%q) never succeeded", pattern)
		}
	}

	vFlags, _ := ParseFlags("v")
	vParser := parser{
		options: ParseOptions{
			Edition: Edition2025,
			Flags:   vFlags,
			Limits:  DefaultParseOptions().Limits,
		},
		tokens: []Token{{kind: TokenEOF}},
	}
	if _, err := vParser.unicodeSetAtom(0); err == nil {
		t.Fatal("unicodeSetAtom(EOF) error = nil")
	}
	if _, _, err := vParser.classItem(); err == nil {
		t.Fatal("classItem(EOF) error = nil")
	}

	if !classHasStrings(Node{
		children: []Node{{classStrings: [][]uint16{{'a'}}}},
	}) {
		t.Fatal("classHasStrings() missed a nested string")
	}

	qTokens, err := Tokenize("\\q{", DefaultParseOptions())
	if err != nil {
		t.Fatalf("Tokenize(class string) error = %v", err)
	}
	qParser := parser{
		options: ParseOptions{
			Edition: Edition2025,
			Flags:   vFlags,
			Limits:  DefaultParseOptions().Limits,
		},
		tokens: qTokens,
	}
	if _, err := qParser.classStringDisjunction(); err == nil {
		t.Fatal("classStringDisjunction(EOF) error = nil")
	}

	lookbehindStrings := "(?<=[\\q{ab|cd}])x"
	if _, err := Compile(
		lookbehindStrings,
		"v",
		DefaultCompileOptions(),
	); err != nil {
		t.Fatalf("Compile(%q) error = %v", lookbehindStrings, err)
	}
}

func TestRemainingMatcherBranches(t *testing.T) {
	t.Parallel()

	unmatched, err := Compile("(?<x>a)?", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := unmatched.Match(
		context.Background(),
		"",
		DefaultMatchOptions(),
	)
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	capture, exists := result.Named("x")
	if !exists || capture.Participated() {
		t.Fatalf("Named(x) = %#v, %t", capture, exists)
	}

	for _, test := range []struct {
		pattern string
		flags   string
		input   string
		start   int
	}{
		{pattern: "(?:.|)", input: "\n"},
		{pattern: "(?:^|x)", input: "xx", start: 1},
		{pattern: "(?:$|x)", input: "x"},
		{pattern: "(?:\\b|x)", input: "xx", start: 1},
		{pattern: "(?:(a)\\1|ab)", input: "ab"},
		{pattern: "(?:(?=a)|x)", input: "x"},
	} {
		program, compileErr := Compile(
			test.pattern,
			test.flags,
			DefaultCompileOptions(),
		)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, compileErr)
		}
		matchOptions := DefaultMatchOptions()
		matchOptions.StartUTF16 = test.start
		if _, matched, matchErr := program.Match(
			context.Background(),
			test.input,
			matchOptions,
		); matchErr != nil || !matched {
			t.Errorf(
				"Match(%q) = _, %t, %v",
				test.pattern,
				matched,
				matchErr,
			)
		}
	}

	look, err := Compile("(?=a)", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(look) error = %v", err)
	}
	matchOptions := DefaultMatchOptions()
	matchOptions.Limits.Allocations = 2
	if _, _, err := look.Match(
		context.Background(),
		"a",
		matchOptions,
	); !isLimitKind(err, LimitAllocations) {
		t.Fatalf("look allocation error = %v", err)
	}

	alternation, err := Compile("a|b", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(alternation) error = %v", err)
	}
	if _, _, err := alternation.Match(
		context.Background(),
		"a",
		matchOptions,
	); !isLimitKind(err, LimitAllocations) {
		t.Fatalf("split allocation error = %v", err)
	}

	flags, _ := ParseFlags("iu")
	letterTable, ok := lookupUnicodeProperty("Letter")
	if !ok {
		t.Fatal("Letter property is missing")
	}
	if unicodePropertyMatches(letterTable, '1', true) {
		t.Fatal("digit matched Letter under folding")
	}
	if builtinMatches(classBuiltinNone, 'a', false, false) {
		t.Fatal("unknown builtin matched")
	}
	if rangeMatches('a', 'z', '1', flags) {
		t.Fatal("digit matched Unicode folded range")
	}
	legacyFlags, _ := ParseFlags("i")
	if rangeMatches('a', 'z', '1', legacyFlags) {
		t.Fatal("digit matched legacy folded range")
	}
	if !rangeMatches('A', 'Z', 'a', legacyFlags) {
		t.Fatal("legacy canonical range did not match")
	}

	lowercaseTable, ok := lookupUnicodeProperty("Lowercase_Letter")
	if !ok || !unicodePropertyMatches(lowercaseTable, 'A', true) {
		t.Fatal("case-folded Unicode property did not match")
	}

	nonUnicode, err := Compile("(a)", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(non-Unicode) error = %v", err)
	}
	view, err := makeInputView("aa", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	executor := newExecutor(
		context.Background(),
		nonUnicode,
		view,
		DefaultMatchOptions().Limits,
	)
	if width, matched := executor.backreference(
		thread{
			position: 2,
			captures: []int{0, 2, 0, 1},
		},
		[]int{1},
		-1,
		Flags{},
	); !matched || width != 1 {
		t.Fatalf("reverse legacy backreference = %d, %t", width, matched)
	}

	unicodeProgram, err := Compile("(😀)", "u", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(Unicode) error = %v", err)
	}
	unicodeView, err := makeInputView("😀😀", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView(Unicode) error = %v", err)
	}
	unicodeExecutor := newExecutor(
		context.Background(),
		unicodeProgram,
		unicodeView,
		DefaultMatchOptions().Limits,
	)
	if _, matched := unicodeExecutor.unicodeBackreference(
		2,
		0,
		1,
		1,
		unicodeProgram.flags,
	); matched {
		t.Fatal("partial forward surrogate capture matched")
	}
	if _, matched := unicodeExecutor.unicodeBackreference(
		2,
		1,
		2,
		-1,
		unicodeProgram.flags,
	); matched {
		t.Fatal("partial reverse surrogate capture matched")
	}

	backreference, err := Compile("(a)\\1", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(backreference) error = %v", err)
	}
	if _, matched, err := backreference.Match(
		context.Background(),
		"ab",
		DefaultMatchOptions(),
	); err != nil || matched {
		t.Fatalf("backreference mismatch = _, %t, %v", matched, err)
	}

	guardProgram := &Program{
		code: []instruction{
			{op: opGuard, slot: 0},
			{op: opJump, x: 0},
		},
		guardCount: 1,
	}
	emptyView, err := makeInputView("", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView(empty) error = %v", err)
	}
	if _, matched, err := newExecutor(
		context.Background(),
		guardProgram,
		emptyView,
		DefaultMatchOptions().Limits,
	).at(0); err != nil || matched {
		t.Fatalf("guard loop = _, %t, %v", matched, err)
	}

	guardBacktrackProgram := &Program{
		code: []instruction{
			{op: opSplit, x: 1, y: 3},
			{op: opGuard, slot: 0},
			{op: opJump, x: 1},
			{op: opMatch},
		},
		guardCount: 1,
	}
	guardLimits := DefaultMatchOptions().Limits
	guardLimits.Backtracks = 0
	if _, _, err := newExecutor(
		context.Background(),
		guardBacktrackProgram,
		emptyView,
		guardLimits,
	).at(0); !isLimitKind(err, LimitBacktracks) {
		t.Fatalf("guard backtrack error = %v", err)
	}
}

func TestRemainingOperationBranches(t *testing.T) {
	t.Parallel()

	program, err := Compile("a", "g", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.Steps = 0
	if _, err := program.FindAll(
		context.Background(),
		"a",
		options,
	); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("FindAll(step) error = %v", err)
	}
	if _, err := program.Replace(
		context.Background(),
		"a",
		UTF16String{},
		options,
	); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("Replace(step) error = %v", err)
	}

	options = DefaultMatchOptions()
	options.Limits.Allocations = 2
	if _, err := program.FindAll(
		context.Background(),
		"a",
		options,
	); !isLimitKind(err, LimitAllocations) {
		t.Fatalf("FindAll(allocation) error = %v", err)
	}

	options = DefaultMatchOptions()
	options.Limits.OutputUTF16 = 0
	noMatch, err := Compile("z", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(no match) error = %v", err)
	}
	if _, err := noMatch.Replace(
		context.Background(),
		"a",
		UTF16String{},
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Replace(suffix) error = %v", err)
	}

	for _, replacement := range []string{
		"$$",
		"$&",
		"$<x>",
		"$1",
		"$3",
		"$x",
	} {
		named, compileErr := Compile(
			"(?<x>a)",
			"",
			DefaultCompileOptions(),
		)
		if compileErr != nil {
			t.Fatalf("Compile(named) error = %v", compileErr)
		}
		if _, replaceErr := named.Replace(
			context.Background(),
			"a",
			UTF16FromString(replacement),
			options,
		); !isLimitKind(replaceErr, LimitOutputUTF16) {
			t.Errorf("Replace(%q) error = %v", replacement, replaceErr)
		}
	}

	prefixOptions := DefaultMatchOptions()
	prefixOptions.Limits.OutputUTF16 = 1
	if _, err := program.Replace(
		context.Background(),
		"za",
		UTF16FromString("$\x60"),
		prefixOptions,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Replace(prefix token) error = %v", err)
	}
	if _, err := program.Replace(
		context.Background(),
		"az",
		UTF16FromString("$'"),
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Replace(suffix token) error = %v", err)
	}

	separator, err := Compile("(,)", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(separator) error = %v", err)
	}
	options = DefaultMatchOptions()
	options.Limits.Steps = 0
	if _, err := separator.Split(
		context.Background(),
		"a,b",
		options,
	); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("Split(step) error = %v", err)
	}
	options = DefaultMatchOptions()
	options.Limits.Results = 1
	if _, err := separator.Split(
		context.Background(),
		"a,b",
		options,
	); !isLimitKind(err, LimitResults) {
		t.Fatalf("Split(capture) error = %v", err)
	}

	options = DefaultMatchOptions()
	options.Limits.Steps = 0
	if _, err := separator.Split(
		context.Background(),
		"",
		options,
	); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("Split(empty step) error = %v", err)
	}

	unnamed, err := Compile("a", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(unnamed) error = %v", err)
	}
	options = DefaultMatchOptions()
	options.Limits.OutputUTF16 = 0
	if _, err := unnamed.Replace(
		context.Background(),
		"a",
		UTF16FromString("$<x>"),
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Replace(unnamed token) error = %v", err)
	}
}

func TestRemainingInputSessionAndUnicodeBranches(t *testing.T) {
	t.Parallel()

	input := UTF16FromString("a")
	limits := DefaultMatchOptions().Limits
	limits.InputRunes = 0
	if _, err := makeUTF16InputView(input, limits); !isLimitKind(
		err,
		LimitInputRunes,
	) {
		t.Fatalf("makeUTF16InputView() error = %v", err)
	}

	program, err := Compile("a", "g", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	limits = DefaultMatchOptions().Limits
	limits.Steps = 0
	if _, _, err := NewSession(program).Exec(
		context.Background(),
		"a",
		limits,
	); !isLimitKind(err, LimitMatchSteps) {
		t.Fatalf("Session.Exec() error = %v", err)
	}

	if _, ok := lookupUnicodeProperty("bad=value"); ok {
		t.Fatal("unknown property prefix succeeded")
	}
	if !unicodeIdentifierStart('$') || !unicodeIdentifierStart('_') {
		t.Fatal("ECMAScript identifier punctuation was rejected")
	}
}
