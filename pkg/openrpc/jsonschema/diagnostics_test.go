package jsonschema

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestDiagnosticHelpersHandleEmptyInputs(t *testing.T) {
	t.Parallel()

	if key := validationErrorKey(nil); key != "" {
		t.Fatalf("validationErrorKey(nil) = %q", key)
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader([]byte(`false`)))
	if err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	compiler.DefaultDraft(validator.Draft7)
	if err := compiler.AddResource("https://example.com/schema.json", document); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("https://example.com/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var validationErr *validator.ValidationError
	if err := compiled.Validate(nil); !errors.As(err, &validationErr) {
		t.Fatalf("validation error = %#v", err)
	}
	nested := &validator.ValidationError{Causes: []*validator.ValidationError{validationErr}}
	if key := validationErrorKey(nested); key != "#\x00#" {
		t.Fatalf("validationErrorKey(nested) = %q", key)
	}
	issues := []Issue{}
	bounded := &validator.ValidationError{Causes: []*validator.ValidationError{
		validationErr, validationErr, validationErr,
	}}
	if total := collectIssues(bounded, 2, &issues); total != 2 || len(issues) != 2 {
		t.Fatalf("collectIssues bounded = %d, %#v", total, issues)
	}
	compiledValidator, err := Compile(
		Schema{value: mustJSONValue(t, `true`), boolean: true, boolValue: true},
		DefaultValidationOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiledValidator.maxIssues = 0
	if report := compiledValidator.Validate(context.Background(), mustJSONValue(t, `null`)); !errors.Is(report.Err(), ErrValidationPolicy) {
		t.Fatalf("zero max issues error = %v", report.Err())
	}
	issues = nil
	if total := collectIssues(nil, 1, &issues); total != 0 || len(issues) != 0 {
		t.Fatalf("collectIssues(nil) = %d, %#v", total, issues)
	}
	for _, input := range [][]byte{
		{},
		[]byte(`{`),
		[]byte(`{"$schema":1}`),
	} {
		if declaresDraft7(input) {
			t.Fatalf("declaresDraft7(%q) = true", input)
		}
	}
}

func mustJSONValue(t *testing.T, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
