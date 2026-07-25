package ecmascript_test

import (
	"errors"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestTokenizePreservesByteSpans(t *testing.T) {
	t.Parallel()

	tokens, err := ecmascript.Tokenize("é|.", ecmascript.DefaultParseOptions())
	if err != nil {
		t.Fatalf("Tokenize() error = %v", err)
	}

	want := []struct {
		kind ecmascript.TokenKind
		text string
		span ecmascript.Span
	}{
		{kind: ecmascript.TokenCharacter, text: "é", span: ecmascript.Span{Start: 0, End: 2}},
		{kind: ecmascript.TokenAlternation, text: "|", span: ecmascript.Span{Start: 2, End: 3}},
		{kind: ecmascript.TokenDot, text: ".", span: ecmascript.Span{Start: 3, End: 4}},
		{kind: ecmascript.TokenEOF, text: "", span: ecmascript.Span{Start: 4, End: 4}},
	}
	if len(tokens) != len(want) {
		t.Fatalf("len(Tokenize()) = %d, want %d", len(tokens), len(want))
	}
	for index := range want {
		if tokens[index].Kind() != want[index].kind ||
			tokens[index].Text() != want[index].text ||
			tokens[index].Span() != want[index].span {
			t.Errorf("token %d = (%v, %q, %+v), want (%v, %q, %+v)",
				index, tokens[index].Kind(), tokens[index].Text(), tokens[index].Span(),
				want[index].kind, want[index].text, want[index].span)
		}
	}
}

func TestParseProducesImmutableTypedTree(t *testing.T) {
	t.Parallel()

	pattern, err := ecmascript.Parse("a|(?:b)+", ecmascript.DefaultParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	root := pattern.Root()
	if root.Kind() != ecmascript.NodeAlternation || root.Span() != (ecmascript.Span{Start: 0, End: 8}) {
		t.Fatalf("root = %v %+v", root.Kind(), root.Span())
	}
	branches := root.Children()
	if len(branches) != 2 || branches[0].Kind() != ecmascript.NodeLiteral || branches[1].Kind() != ecmascript.NodeQuantifier {
		t.Fatalf("branches = %#v", branches)
	}
	branches[0] = ecmascript.Node{}
	if pattern.Root().Children()[0].Kind() != ecmascript.NodeLiteral {
		t.Fatal("Children() exposed mutable AST storage")
	}
	quantifier := branches[1]
	if quantifier.Min() != 1 || quantifier.Max() != -1 || !quantifier.Greedy() {
		t.Fatalf("quantifier = min %d max %d greedy %t", quantifier.Min(), quantifier.Max(), quantifier.Greedy())
	}
	group := quantifier.Children()[0]
	if group.Kind() != ecmascript.NodeGroup || group.Capturing() {
		t.Fatalf("group = %v capturing=%t", group.Kind(), group.Capturing())
	}
}

func TestParseRejectsMalformedAndUnsupportedSyntaxExplicitly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		code    ecmascript.SyntaxCode
	}{
		{name: "dangling escape", pattern: `a\`, code: ecmascript.SyntaxUnexpectedEOF},
		{name: "unclosed group", pattern: "(a", code: ecmascript.SyntaxUnclosedGroup},
		{name: "invalid quantifier", pattern: "a{3,2}", code: ecmascript.SyntaxInvalidQuantifier},
		{name: "property escape without unicode mode", pattern: `\p{Script=Greek}`, code: ecmascript.SyntaxInvalidEscape},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ecmascript.Parse(test.pattern, ecmascript.DefaultParseOptions())
			var syntaxError *ecmascript.SyntaxError
			if !errors.As(err, &syntaxError) || syntaxError.Code != test.code {
				t.Fatalf("Parse(%q) error = %v, want syntax code %v", test.pattern, err, test.code)
			}
		})
	}
}

func TestParseEnforcesStructuralLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		adjust  func(*ecmascript.ParseLimits)
		kind    ecmascript.LimitKind
	}{
		{name: "bytes", pattern: "abc", adjust: func(l *ecmascript.ParseLimits) { l.PatternBytes = 2 }, kind: ecmascript.LimitPatternBytes},
		{name: "depth", pattern: "((a))", adjust: func(l *ecmascript.ParseLimits) { l.ASTDepth = 1 }, kind: ecmascript.LimitASTDepth},
		{name: "captures", pattern: "(a)(b)", adjust: func(l *ecmascript.ParseLimits) { l.Captures = 1 }, kind: ecmascript.LimitCaptures},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options := ecmascript.DefaultParseOptions()
			test.adjust(&options.Limits)
			_, err := ecmascript.Parse(test.pattern, options)
			var limitError *ecmascript.LimitError
			if !errors.As(err, &limitError) || limitError.Kind != test.kind {
				t.Fatalf("Parse(%q) error = %v, want limit %v", test.pattern, err, test.kind)
			}
		})
	}
}
