package expression_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/expression"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestTemplateEvaluatesJSONTemplateLanguageVectors(t *testing.T) {
	t.Parallel()

	context, err := expression.NewContext(map[string]jsonvalue.Value{
		"query": value(t, `{"number":1,"salads":["caesar","potato"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	template, err := expression.Parse(
		"number${query.number}salad${query.salads[1]}",
		expression.DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if template.String() != "number${query.number}salad${query.salads[1]}" {
		t.Fatalf("String() = %q", template.String())
	}
	result, err := template.Evaluate(context)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.Bytes()); got != `"number1saladpotato"` {
		t.Fatalf("Evaluate = %s", got)
	}
}

func TestWholeExpressionPreservesReferencedJSONType(t *testing.T) {
	t.Parallel()

	context, err := expression.NewContext(map[string]jsonvalue.Value{
		"result": value(t, `{"id":42,"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	template, err := expression.Parse("${result}", expression.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	result, err := template.Evaluate(context)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.Bytes()); got != `{"id":42,"ok":true}` {
		t.Fatalf("Evaluate = %s", got)
	}
}

func TestTemplateReportsMissingAndNonScalarInterpolations(t *testing.T) {
	t.Parallel()

	context, err := expression.NewContext(map[string]jsonvalue.Value{
		"result": value(t, `{"items":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		source string
		want   error
	}{
		{source: "${result.missing}", want: expression.ErrMissingValue},
		{source: "items=${result.items}", want: expression.ErrUnsupportedValue},
	} {
		template, err := expression.Parse(test.source, expression.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := template.Evaluate(context); !errors.Is(err, test.want) {
			t.Errorf("Evaluate(%q) error = %v", test.source, err)
		}
	}
}

func TestTemplateRejectsInvalidGrammarAndBounds(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"${}", "${a.}", "${a[-1]}", "${a[01]}", "${a", "hello world"} {
		if _, err := expression.Parse(source, expression.DefaultPolicy()); !errors.Is(err, expression.ErrInvalidExpression) {
			t.Errorf("Parse(%q) error = %v", source, err)
		}
	}
	policy := expression.DefaultPolicy()
	policy.MaxLength = 3
	if _, err := expression.Parse("abcd", policy); !errors.Is(err, expression.ErrExpressionLimit) {
		t.Fatalf("length error = %v", err)
	}
	if _, err := expression.Parse("x", expression.Policy{}); !errors.Is(err, expression.ErrExpressionPolicy) {
		t.Fatalf("policy error = %v", err)
	}
}

func TestContextOwnsBindings(t *testing.T) {
	t.Parallel()

	bindings := map[string]jsonvalue.Value{"result": value(t, `1`)}
	context, err := expression.NewContext(bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["result"] = value(t, `2`)
	template, err := expression.Parse("${result}", expression.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	result, err := template.Evaluate(context)
	if err != nil || string(result.Bytes()) != "1" {
		t.Fatalf("Evaluate = %s, error = %v", result.Bytes(), err)
	}
}

func TestEvaluateEnforcesNodeBudgetForSelectedPayloads(t *testing.T) {
	t.Parallel()

	contextValue, err := expression.NewContext(map[string]jsonvalue.Value{
		"payload": value(t, `{"nested":{"answer":42}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := expression.DefaultPolicy()
	policy.MaxNodes = 2
	template, err := expression.Parse("${payload.nested.answer}", policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := template.Evaluate(contextValue); !errors.Is(err, expression.ErrExpressionLimit) {
		t.Fatalf("Evaluate error = %v", err)
	}
}

func FuzzTemplateNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"${result}",
		"${result.items[0]}",
		"https://${host}:${port}/",
		"${bad[-1]}",
	} {
		f.Add(seed)
	}
	context, err := expression.NewContext(map[string]jsonvalue.Value{
		"result": value(f, `{"items":[1]}`),
		"host":   value(f, `"example.com"`),
		"port":   value(f, `443`),
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, source string) {
		policy := expression.DefaultPolicy()
		policy.MaxLength = 256
		policy.MaxExpressions = 16
		policy.MaxSegments = 16
		policy.MaxOutputBytes = 1024
		template, err := expression.Parse(source, policy)
		if err != nil {
			return
		}
		_, _ = template.Evaluate(context)
	})
}

func value(t testing.TB, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
