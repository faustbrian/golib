package expression_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/expression"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestParseRuntimeExpressionKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw     string
		kind    expression.Kind
		source  expression.Source
		name    string
		pointer string
	}{
		{raw: "$url", kind: expression.URL},
		{raw: "$method", kind: expression.Method},
		{raw: "$statusCode", kind: expression.StatusCode},
		{raw: "$request.header.content-type", kind: expression.Request, source: expression.Header, name: "content-type"},
		{raw: "$request.query.queryUrl", kind: expression.Request, source: expression.Query, name: "queryUrl"},
		{raw: "$request.path.id", kind: expression.Request, source: expression.Path, name: "id"},
		{raw: "$request.body", kind: expression.Request, source: expression.Body},
		{raw: "$request.body#", kind: expression.Request, source: expression.Body},
		{raw: "$response.body#/status~1code", kind: expression.Response, source: expression.Body, pointer: "/status~1code"},
	}
	for _, test := range tests {
		parsed, err := expression.Parse(test.raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.raw, err)
		}
		if parsed.Kind() != test.kind || parsed.Source() != test.source ||
			parsed.Name() != test.name || parsed.Pointer().String() != test.pointer {
			t.Fatalf("Parse(%q) = %#v", test.raw, parsed)
		}
		if parsed.String() != test.raw {
			t.Fatalf("String() = %q, want %q", parsed.String(), test.raw)
		}
	}
}

func TestParseRejectsExpressionsOutsideNormativeGrammar(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"", "$URL", "$statuscode", "$request", "$request.cookie.id",
		"$request.header.", "$request.header.bad value", "$request.header.ümlaut",
		"$request.query." + string([]byte{0xff}), "$request.query.bad\x1fname",
		"$request.body/no-fragment", "$response.body#/bad~2escape",
	} {
		if _, err := expression.Parse(raw); !errors.Is(err, expression.ErrInvalid) {
			t.Fatalf("Parse(%q) error = %v", raw, err)
		}
	}
}

func TestParseTemplateSeparatesLiteralAndExpressionParts(t *testing.T) {
	t.Parallel()

	template, err := expression.ParseTemplate(
		"https://callback.example.test?id={$request.body#/id}&status={$statusCode}",
	)
	if err != nil {
		t.Fatal(err)
	}
	parts := template.Parts()
	if len(parts) != 4 {
		t.Fatalf("got %d parts: %#v", len(parts), parts)
	}
	if parts[0].Literal() != "https://callback.example.test?id=" ||
		parts[0].Dynamic() || !parts[1].Dynamic() ||
		parts[1].Expression().Pointer().String() != "/id" ||
		parts[2].Literal() != "&status=" ||
		parts[3].Expression().Kind() != expression.StatusCode {
		t.Fatalf("unexpected parts: %#v", parts)
	}
	parts[0] = expression.Part{}
	if template.Parts()[0].Literal() == "" {
		t.Fatal("template exposed mutable part storage")
	}
}

func TestParseTemplateAcceptsLiteralOnlyText(t *testing.T) {
	t.Parallel()

	template, err := expression.ParseTemplate("literal")
	if err != nil {
		t.Fatal(err)
	}
	parts := template.Parts()
	if len(parts) != 1 || parts[0].Literal() != "literal" || parts[0].Dynamic() {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseAcceptsEveryGrammarBoundaryCharacter(t *testing.T) {
	t.Parallel()

	for _, character := range []byte{'0', '9', 'A', 'Z', 'a', 'z'} {
		raw := "$request.header." + string(character)
		if _, err := expression.Parse(raw); err != nil {
			t.Fatalf("Parse(%q) error = %v", raw, err)
		}
	}
	if _, err := expression.Parse("$request.query. "); err != nil {
		t.Fatalf("space parameter name error = %v", err)
	}
}

func TestParseTemplateAcceptsAnExpressionAtOffsetZero(t *testing.T) {
	t.Parallel()

	template, err := expression.ParseTemplate("{$url}")
	if err != nil {
		t.Fatal(err)
	}
	parts := template.Parts()
	if len(parts) != 1 || !parts[0].Dynamic() {
		t.Fatalf("zero-offset expression parts = %#v", parts)
	}
}

func TestParseTemplateRejectsMalformedEmbedding(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"{$request.body#/id", "{}", "{{ $url }}", "{$request.bad}", "literal}", "}",
	} {
		if _, err := expression.ParseTemplate(raw); !errors.Is(err, expression.ErrInvalid) {
			t.Fatalf("ParseTemplate(%q) error = %v", raw, err)
		}
	}
}

var _ reference.Pointer = expression.Expression{}.Pointer()
