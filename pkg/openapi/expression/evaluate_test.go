package expression_test

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/expression"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestEvaluatePreservesRuntimeValueTypes(t *testing.T) {
	t.Parallel()

	body := expressionObject(t,
		jsonvalue.Member{Name: "id", Value: expressionNumber(t, "42")},
		jsonvalue.Member{Name: "active", Value: jsonvalue.Boolean(true)},
	)
	context := expression.Context{
		URL:        "https://example.test/widgets/42?verbose=true",
		Method:     "POST",
		StatusCode: 201,
		Request: expression.Message{
			Headers: map[string]string{"Content-Type": "application/json"},
			Query:   map[string]jsonvalue.Value{"verbose": jsonvalue.Boolean(true)},
			Path:    map[string]jsonvalue.Value{"id": expressionNumber(t, "42")},
			Body:    body,
		},
		Response: expression.Message{
			Headers: map[string]string{"Location": "/widgets/42"},
			Body:    expressionObject(t, jsonvalue.Member{Name: "saved", Value: jsonvalue.Boolean(true)}),
		},
	}
	tests := []struct {
		raw  string
		want jsonvalue.Value
	}{
		{raw: "$url", want: expressionString(t, context.URL)},
		{raw: "$method", want: expressionString(t, "POST")},
		{raw: "$statusCode", want: expressionNumber(t, "201")},
		{raw: "$request.header.content-type", want: expressionString(t, "application/json")},
		{raw: "$request.query.verbose", want: jsonvalue.Boolean(true)},
		{raw: "$request.path.id", want: expressionNumber(t, "42")},
		{raw: "$request.body", want: body},
		{raw: "$request.body#/id", want: expressionNumber(t, "42")},
		{raw: "$response.header.location", want: expressionString(t, "/widgets/42")},
		{raw: "$response.body#/saved", want: jsonvalue.Boolean(true)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			parsed, err := expression.Parse(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parsed.Evaluate(context)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Evaluate() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestEvaluateReportsUnavailableAndAmbiguousContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		context expression.Context
		also    error
	}{
		{name: "url", raw: "$url"},
		{name: "method", raw: "$method"},
		{name: "status", raw: "$statusCode"},
		{name: "header", raw: "$request.header.missing"},
		{name: "query", raw: "$request.query.missing"},
		{name: "path", raw: "$request.path.missing"},
		{name: "body", raw: "$request.body"},
		{
			name: "pointer", raw: "$request.body#/missing",
			context: expression.Context{Request: expression.Message{
				Body: expressionObject(t),
			}},
			also: reference.ErrTargetNotFound,
		},
		{
			name: "ambiguous header", raw: "$request.header.x-id",
			context: expression.Context{Request: expression.Message{Headers: map[string]string{
				"X-Id": "one", "x-id": "two",
			}}},
			also: expression.ErrAmbiguousContext,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := expression.Parse(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parsed.Evaluate(test.context); !errors.Is(err, expression.ErrUnavailable) {
				t.Fatalf("Evaluate() error = %v", err)
			} else if test.also != nil && !errors.Is(err, test.also) {
				t.Fatalf("Evaluate() error = %v, want %v", err, test.also)
			}
		})
	}

	if _, err := (expression.Expression{}).Evaluate(expression.Context{}); !errors.Is(err, expression.ErrInvalid) {
		t.Fatalf("zero expression error = %v", err)
	}
	status, _ := expression.Parse("$statusCode")
	if _, err := status.Evaluate(expression.Context{StatusCode: 1000}); !errors.Is(err, expression.ErrInvalidContext) {
		t.Fatalf("invalid status error = %v", err)
	}
	url, _ := expression.Parse("$url")
	if _, err := url.Evaluate(expression.Context{URL: string([]byte{0xff})}); !errors.Is(err, expression.ErrInvalidContext) {
		t.Fatalf("invalid URL error = %v", err)
	}
	header, _ := expression.Parse("$request.header.x-value")
	if _, err := header.Evaluate(expression.Context{Request: expression.Message{
		Headers: map[string]string{"X-Value": string([]byte{0xff})},
	}}); !errors.Is(err, expression.ErrInvalidContext) {
		t.Fatalf("invalid header error = %v", err)
	}
	query, _ := expression.Parse("$request.query.value")
	if _, err := query.Evaluate(expression.Context{Request: expression.Message{
		Query: map[string]jsonvalue.Value{"value": {}},
	}}); !errors.Is(err, expression.ErrInvalidContext) {
		t.Fatalf("invalid query error = %v", err)
	}
}

func TestEvaluateTemplateRendersSemanticValuesWithBounds(t *testing.T) {
	t.Parallel()

	template, err := expression.ParseTemplate(
		"https://example.test/{$request.path.id}?active={$request.query.active}",
	)
	if err != nil {
		t.Fatal(err)
	}
	context := expression.Context{Request: expression.Message{
		Path:  map[string]jsonvalue.Value{"id": expressionNumber(t, "42")},
		Query: map[string]jsonvalue.Value{"active": jsonvalue.Boolean(true)},
	}}
	got, err := template.Evaluate(context, expression.EvaluationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test/42?active=true" {
		t.Fatalf("Evaluate() = %q", got)
	}
	stringTemplate, _ := expression.ParseTemplate("{$request.header.location}")
	got, err = stringTemplate.Evaluate(expression.Context{Request: expression.Message{
		Headers: map[string]string{"Location": "/widgets/42"},
	}}, expression.EvaluationOptions{})
	if err != nil || got != "/widgets/42" {
		t.Fatalf("string template = %q, %v", got, err)
	}
	missingTemplate, _ := expression.ParseTemplate("{$request.path.missing}")
	if _, err := missingTemplate.Evaluate(expression.Context{}, expression.EvaluationOptions{}); !errors.Is(err, expression.ErrUnavailable) {
		t.Fatalf("missing template error = %v", err)
	}
	if _, err := template.Evaluate(context, expression.EvaluationOptions{
		MaxOutputBytes: 12,
	}); !errors.Is(err, expression.ErrLimitExceeded) {
		t.Fatalf("bounded Evaluate() error = %v", err)
	}
	if _, err := template.Evaluate(context, expression.EvaluationOptions{
		MaxOutputBytes: -1,
	}); !errors.Is(err, expression.ErrInvalidContext) {
		t.Fatalf("invalid options error = %v", err)
	}
}

func TestEvaluateAcceptsExactStatusAndOutputBoundaries(t *testing.T) {
	t.Parallel()

	status, err := expression.Parse("$statusCode")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{100, 999} {
		value, err := status.Evaluate(expression.Context{StatusCode: code})
		if err != nil {
			t.Fatalf("status %d error = %v", code, err)
		}
		text, _ := value.NumberText()
		if text != strconv.Itoa(code) {
			t.Fatalf("status %d value = %q", code, text)
		}
	}
	template, err := expression.ParseTemplate("aa{$method}")
	if err != nil {
		t.Fatal(err)
	}
	context := expression.Context{Method: "aa"}
	if got, err := template.Evaluate(context, expression.EvaluationOptions{
		MaxOutputBytes: 4,
	}); err != nil || got != "aaaa" {
		t.Fatalf("exact template output = %q, %v", got, err)
	}
	if _, err := template.Evaluate(context, expression.EvaluationOptions{
		MaxOutputBytes: 3,
	}); !errors.Is(err, expression.ErrLimitExceeded) {
		t.Fatalf("cumulative template limit error = %v", err)
	}
}

func expressionString(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func expressionNumber(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Number(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func expressionObject(t *testing.T, members ...jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
