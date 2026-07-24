package jsonschema_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

type panickingJSONValue struct{}

func (panickingJSONValue) MarshalJSON() ([]byte, error) {
	panic("sensitive JSON marshaler panic")
}

type failingJSONValue struct {
	err error
}

func (value failingJSONValue) MarshalJSON() ([]byte, error) {
	return nil, value.err
}

func TestValidateValuePreservesExactNumbersAndInputOwnership(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"properties":{"number":{"const":123456789012345678901234567890}}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{
		"number": json.Number("123456789012345678901234567890"),
	}
	result, err := schema.ValidateValue(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("exact caller-provided number was rejected")
	}
	if value["number"] != json.Number("123456789012345678901234567890") {
		t.Fatal("validation mutated caller-owned input")
	}
}

func TestValidateValueContainsJSONMarshalerPanics(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`true`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func() error
	}{
		{
			name: "flag",
			call: func() error {
				_, err := schema.ValidateValue(context.Background(), panickingJSONValue{})
				return err
			},
		},
		{
			name: "standard output",
			call: func() error {
				_, err := schema.ValidateValueOutput(
					context.Background(),
					panickingJSONValue{},
					jsonschema.OutputBasic,
				)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireContainedCallbackPanic(
				t,
				"sensitive JSON marshaler panic",
				test.call,
			)
		})
	}
}

func TestValidateValueRedactsJSONMarshalerErrors(t *testing.T) {
	t.Parallel()

	const secret = "sensitive JSON marshaler error"
	marshalError := errors.New(secret)
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`true`))
	if err != nil {
		t.Fatal(err)
	}
	requireRedactedCallbackError(t, secret, marshalError, func() error {
		_, err := schema.ValidateValue(
			context.Background(),
			failingJSONValue{err: marshalError},
		)
		return err
	})
}

func TestValidateValueBoundsEncodingAndClassifiesFailures(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxInputBytes = 8
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`true`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.ValidateValue(context.Background(), "a value larger than the limit")
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	_, err = schema.ValidateValue(context.Background(), make(chan int))
	if !errors.Is(err, jsonschema.ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}
}

func TestValidateValueOutputUsesSelectedForm(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateValueOutput(
		context.Background(),
		42,
		jsonschema.OutputBasic,
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.Valid || len(output.Errors) == 0 {
		t.Fatal("value output did not report the type failure")
	}
}
