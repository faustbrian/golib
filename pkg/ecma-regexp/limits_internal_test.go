package ecmascript

import (
	"context"
	"errors"
	"testing"
)

func TestParserLimitContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		mutate  func(*ParseOptions)
		kind    LimitKind
	}{
		{
			name:    "pattern bytes",
			pattern: "a",
			mutate: func(options *ParseOptions) {
				options.Limits.PatternBytes = 0
			},
			kind: LimitPatternBytes,
		},
		{
			name:    "AST depth",
			pattern: "(a)",
			mutate: func(options *ParseOptions) {
				options.Limits.ASTDepth = 0
			},
			kind: LimitASTDepth,
		},
		{
			name:    "captures",
			pattern: "(a)",
			mutate: func(options *ParseOptions) {
				options.Limits.Captures = 0
			},
			kind: LimitCaptures,
		},
		{
			name:    "classes",
			pattern: "[a]",
			mutate: func(options *ParseOptions) {
				options.Limits.CharacterClasses = 0
			},
			kind: LimitCharacterClasses,
		},
		{
			name:    "nodes",
			pattern: "a",
			mutate: func(options *ParseOptions) {
				options.Limits.ASTNodes = 0
			},
			kind: LimitASTNodes,
		},
	}
	for _, test := range tests {
		options := DefaultParseOptions()
		test.mutate(&options)
		_, err := Parse(test.pattern, options)
		var limitError *LimitError
		if !errors.As(err, &limitError) || limitError.Kind != test.kind {
			t.Errorf("%s error = %v, want %v", test.name, err, test.kind)
		}
	}

	options := DefaultParseOptions()
	options.Edition = 0
	if _, err := Tokenize("", options); err == nil {
		t.Fatal("Tokenize() accepted an unsupported edition")
	}
}

func TestCompilerLimitAtEveryInstructionBoundary(t *testing.T) {
	t.Parallel()

	patterns := []struct {
		pattern string
		flags   string
	}{
		{pattern: "abc|def"},
		{pattern: "(a)?(b)+(c)*(d){2,4}"},
		{pattern: "(?=a)(?!b)(?<=c)(?<!d)"},
		{pattern: "^.$\\b[^a-z]\\1(a)"},
		{pattern: "[\\q{ab|cd}--\\q{cd}]", flags: "v"},
	}
	for _, test := range patterns {
		success := false
		for limit := uint64(0); limit < 128; limit++ {
			options := DefaultCompileOptions()
			options.Limits.ProgramInstructions = limit
			_, err := Compile(test.pattern, test.flags, options)
			if err == nil {
				success = true
				break
			}
			var limitError *LimitError
			if !errors.As(err, &limitError) {
				t.Fatalf(
					"Compile(%q) limit %d error = %v",
					test.pattern,
					limit,
					err,
				)
			}
		}
		if !success {
			t.Fatalf("Compile(%q) never succeeded", test.pattern)
		}
	}
}

func TestExecutionLimitsAtBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		input   string
		mutate  func(*MatchOptions)
		kind    LimitKind
	}{
		{
			name:    "input runes",
			pattern: "a",
			input:   "a",
			mutate: func(options *MatchOptions) {
				options.Limits.InputRunes = 0
			},
			kind: LimitInputRunes,
		},
		{
			name:    "steps",
			pattern: "a",
			input:   "a",
			mutate: func(options *MatchOptions) {
				options.Limits.Steps = 0
			},
			kind: LimitMatchSteps,
		},
		{
			name:    "allocations",
			pattern: "a",
			input:   "a",
			mutate: func(options *MatchOptions) {
				options.Limits.Allocations = 0
			},
			kind: LimitAllocations,
		},
		{
			name:    "stack",
			pattern: "a|b",
			input:   "a",
			mutate: func(options *MatchOptions) {
				options.Limits.StackDepth = 0
			},
			kind: LimitStackDepth,
		},
		{
			name:    "recursion",
			pattern: "(?=a)",
			input:   "a",
			mutate: func(options *MatchOptions) {
				options.Limits.RecursionDepth = 0
			},
			kind: LimitRecursionDepth,
		},
	}
	for _, test := range tests {
		program, err := Compile(test.pattern, "", DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		options := DefaultMatchOptions()
		test.mutate(&options)
		_, _, err = program.Match(context.Background(), test.input, options)
		var limitError *LimitError
		if !errors.As(err, &limitError) || limitError.Kind != test.kind {
			t.Errorf("%s error = %v, want %v", test.name, err, test.kind)
		}
	}

	program, err := Compile("a", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.StartUTF16 = 2
	if _, _, err := program.Match(
		context.Background(),
		"a",
		options,
	); err == nil {
		t.Fatal("Match() accepted an out-of-range start")
	}
}

func TestOperationLimitBoundaries(t *testing.T) {
	t.Parallel()

	global, err := Compile("a", "g", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.Results = 0
	if _, err := global.FindAll(
		context.Background(),
		"a",
		options,
	); !isLimitKind(err, LimitResults) {
		t.Fatalf("FindAll() error = %v", err)
	}
	if _, err := global.Replace(
		context.Background(),
		"a",
		UTF16FromString("x"),
		options,
	); !isLimitKind(err, LimitResults) {
		t.Fatalf("Replace() error = %v", err)
	}

	separator, err := Compile("(,)", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(separator) error = %v", err)
	}
	if _, err := separator.Split(
		context.Background(),
		"a,b",
		options,
	); !isLimitKind(err, LimitResults) {
		t.Fatalf("Split() result error = %v", err)
	}

	options = DefaultMatchOptions()
	options.Limits.OutputUTF16 = 0
	if _, err := separator.Split(
		context.Background(),
		"a,b",
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Split() output error = %v", err)
	}
	if _, err := global.Replace(
		context.Background(),
		"ba",
		UTF16FromString("x"),
		options,
	); !isLimitKind(err, LimitOutputUTF16) {
		t.Fatalf("Replace() prefix error = %v", err)
	}
	if _, err := global.Replace(
		context.Background(),
		"a",
		UTF16String{},
		options,
	); err != nil {
		t.Fatalf("Replace(empty) error = %v", err)
	}
}

func isLimitKind(err error, kind LimitKind) bool {
	var limitError *LimitError
	return errors.As(err, &limitError) && limitError.Kind == kind
}

func TestSessionStateBoundaries(t *testing.T) {
	t.Parallel()

	global, err := Compile("a", "g", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	session := NewSession(global)
	session.SetLastIndex(-2)
	if session.LastIndex() != 0 {
		t.Fatalf("negative lastIndex = %d", session.LastIndex())
	}
	session.SetLastIndex(2)
	if _, matched, err := session.Exec(
		context.Background(),
		"a",
		DefaultMatchOptions().Limits,
	); err != nil || matched || session.LastIndex() != 0 {
		t.Fatalf("Exec(out of range) = _, %t, %v", matched, err)
	}

	plain, err := Compile("z", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(plain) error = %v", err)
	}
	if _, matched, err := NewSession(plain).Exec(
		context.Background(),
		"a",
		DefaultMatchOptions().Limits,
	); err != nil || matched {
		t.Fatalf("Exec(non-stateful) = _, %t, %v", matched, err)
	}
}

func TestUTF16AndUnicodeHelperBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		units    []uint16
		position int
		ok       bool
		width    int
	}{
		{units: nil, position: 0},
		{units: []uint16{'a'}, position: 0},
		{units: []uint16{'a'}, position: 1, ok: true, width: 1},
		{
			units:    []uint16{0xD83D, 0xDE00},
			position: 2,
			ok:       true,
			width:    2,
		},
	} {
		_, width, ok := codePointBeforeUnits(test.units, test.position)
		if ok != test.ok || width != test.width {
			t.Errorf(
				"codePointBeforeUnits(%04X, %d) = _, %d, %t",
				test.units,
				test.position,
				width,
				ok,
			)
		}
	}

	if _, ok := lookupUnicodeProperty("SuchProperty"); ok {
		t.Fatal("lookupUnicodeProperty() accepted an unknown name")
	}
	if unicodeIdentifierStart('0') {
		t.Fatal("digit is not an identifier start")
	}
	if variants := unicodeFoldVariants(-1); len(variants) != 1 {
		t.Fatalf("unicodeFoldVariants(-1) = %v", variants)
	}

	if _, err := UTF16FromUnits([]uint16{0xDC00}).GoString(); !errors.Is(
		err,
		ErrUnpairedSurrogate,
	) {
		t.Fatalf("GoString(low surrogate) error = %v", err)
	}
}
