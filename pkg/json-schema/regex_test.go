package jsonschema_test

import (
	"context"
	"errors"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestPatternUsesECMAScriptLookaroundAndBackreferences(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		pattern  string
		instance string
		valid    bool
	}{
		{name: "positive lookahead", pattern: `a(?=b)`, instance: `"ab"`, valid: true},
		{name: "negative lookahead", pattern: `a(?!b)`, instance: `"ac"`, valid: true},
		{name: "positive lookbehind", pattern: `(?<=a)b`, instance: `"ab"`, valid: true},
		{name: "numbered backreference", pattern: `(a)\1`, instance: `"aa"`, valid: true},
		{name: "backreference mismatch", pattern: `(a)\1`, instance: `"ab"`, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, err := jsonschema.NewCompiler()
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(
				context.Background(),
				[]byte(`{"type":"string","pattern":`+quoteJSON(test.pattern)+`}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			result, err := schema.Validate(
				context.Background(),
				[]byte(test.instance),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid != test.valid {
				t.Fatalf("got %t, want %t", result.Valid, test.valid)
			}
		})
	}
}

func TestPatternBacktrackingIsBounded(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxRegexBacktracking = 32
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"type":"string","pattern":"(?:^){100}"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.Validate(context.Background(), []byte(`""`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	var limitError *jsonschema.LimitError
	if !errors.As(err, &limitError) || limitError.Resource != "regular expression backtracking" {
		t.Fatalf("got %#v, want regular expression backtracking limit", err)
	}
}

func TestRegexFormatCompilationUsesConfiguredByteLimit(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxRegexBytes = 3
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithLimits(limits),
		jsonschema.WithFormatAssertion(),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"format":"regex"}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = schema.Validate(context.Background(), []byte(`"abcd"`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	var limitError *jsonschema.LimitError
	if !errors.As(err, &limitError) || limitError.Resource != "regular expression bytes" {
		t.Fatalf("got %#v, want regular expression byte limit", err)
	}

	override, err := jsonschema.NewCompiler(
		jsonschema.WithLimits(limits),
		jsonschema.WithFormatAssertion(),
		jsonschema.WithFormat("regex", jsonschema.FormatFunc(func(
			context.Context,
			string,
		) (bool, error) {
			return true, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err = override.Compile(
		context.Background(),
		[]byte(`{"format":"regex"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`"abcd"`))
	if err != nil || !result.Valid {
		t.Fatalf("custom regex format replacement got valid=%t, err=%v", result.Valid, err)
	}
}

func quoteJSON(value string) string {
	result := `"`
	for _, character := range value {
		switch character {
		case '\\', '"':
			result += `\` + string(character)
		default:
			result += string(character)
		}
	}
	return result + `"`
}
