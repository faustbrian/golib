package jsonschema_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestRegisteredVocabularyCompilesAndEvaluatesCustomKeyword(t *testing.T) {
	t.Parallel()

	const (
		metaSchema = "https://schemas.example.test/even-meta"
		vocabulary = "https://vocabularies.example.test/even"
	)
	loader, err := jsonschema.NewMapLoader(map[string][]byte{
		metaSchema: []byte(`{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$id":"https://schemas.example.test/even-meta",
			"$vocabulary":{
				"https://json-schema.org/draft/2020-12/vocab/core":true,
				"https://json-schema.org/draft/2020-12/vocab/applicator":true,
				"https://json-schema.org/draft/2020-12/vocab/validation":true,
				"https://vocabularies.example.test/even":true
			}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithResourceLoader(loader),
		jsonschema.WithVocabulary(vocabulary, map[string]jsonschema.KeywordCompiler{
			"even": jsonschema.KeywordCompilerFunc(
				func(
					ctx context.Context,
					_ jsonschema.Dialect,
					schemaValue jsonschema.Value,
				) (jsonschema.KeywordEvaluator, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					enabled, ok := schemaValue.Bool()
					if !ok || !enabled {
						return nil, errors.New("even must be true")
					}
					return jsonschema.KeywordEvaluatorFunc(
						func(ctx context.Context, instance jsonschema.Value) (jsonschema.KeywordResult, error) {
							if err := ctx.Err(); err != nil {
								return jsonschema.KeywordResult{}, err
							}
							number, ok := instance.Number()
							if !ok {
								return jsonschema.KeywordResult{Valid: true}, nil
							}
							parsed, err := strconv.Atoi(number)
							valid := err == nil && parsed%2 == 0
							result := jsonschema.KeywordResult{Valid: valid}
							if valid {
								result.Annotation = json.RawMessage(`"even"`)
							}
							return result, nil
						},
					), nil
				},
			),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"$schema":"`+metaSchema+`","even":true}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		instance string
		valid    bool
	}{
		{instance: "2", valid: true},
		{instance: "3", valid: false},
		{instance: `"not a number"`, valid: true},
	} {
		result, err := schema.Validate(context.Background(), []byte(test.instance))
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid != test.valid {
			t.Errorf("%s: got %t, want %t", test.instance, result.Valid, test.valid)
		}
	}
	output, err := schema.ValidateOutput(
		context.Background(),
		[]byte(`2`),
		jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Annotations) != 1 || output.Annotations[0].Annotation != "even" {
		t.Fatalf("got custom annotations %#v", output.Annotations)
	}
}

func TestCustomKeywordCallsAreBounded(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxCustomKeywordCalls = 1
	// Custom keywords are active without vocabulary declarations in historical
	// drafts, which have no vocabulary negotiation mechanism.
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft7),
		jsonschema.WithLimits(limits),
		jsonschema.WithVocabulary(
			"https://vocabularies.example.test/always",
			map[string]jsonschema.KeywordCompiler{
				"always": jsonschema.KeywordCompilerFunc(
					func(context.Context, jsonschema.Dialect, jsonschema.Value) (jsonschema.KeywordEvaluator, error) {
						return jsonschema.KeywordEvaluatorFunc(
							func(context.Context, jsonschema.Value) (jsonschema.KeywordResult, error) {
								return jsonschema.KeywordResult{Valid: true}, nil
							},
						), nil
					},
				),
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"allOf":[{"always":true},{"always":true}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.Validate(context.Background(), []byte(`null`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestCompoundResourcesUseTheirOwnVocabulary(t *testing.T) {
	t.Parallel()

	const (
		metaSchema = "https://schemas.example.test/even-meta"
		coreMeta   = "https://schemas.example.test/core-meta"
		vocabulary = "https://vocabularies.example.test/even"
	)
	loader, err := jsonschema.NewMapLoader(map[string][]byte{
		metaSchema: []byte(`{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$id":"https://schemas.example.test/even-meta",
			"$vocabulary":{
				"https://json-schema.org/draft/2020-12/vocab/core":true,
				"https://json-schema.org/draft/2020-12/vocab/applicator":true,
				"https://json-schema.org/draft/2020-12/vocab/validation":true,
				"https://vocabularies.example.test/even":true
			}
		}`),
		coreMeta: []byte(`{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$id":"https://schemas.example.test/core-meta",
			"$vocabulary":{
				"https://json-schema.org/draft/2020-12/vocab/core":true
			}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	evenCompiler := jsonschema.KeywordCompilerFunc(
		func(
			_ context.Context,
			_ jsonschema.Dialect,
			_ jsonschema.Value,
		) (jsonschema.KeywordEvaluator, error) {
			return jsonschema.KeywordEvaluatorFunc(
				func(_ context.Context, value jsonschema.Value) (jsonschema.KeywordResult, error) {
					number, ok := value.Number()
					return jsonschema.KeywordResult{Valid: !ok || number == "2"}, nil
				},
			), nil
		},
	)
	newCompiler := func(t *testing.T) *jsonschema.Compiler {
		t.Helper()
		compiler, err := jsonschema.NewCompiler(
			jsonschema.WithResourceLoader(loader),
			jsonschema.WithVocabulary(
				vocabulary,
				map[string]jsonschema.KeywordCompiler{"even": evenCompiler},
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		return compiler
	}

	t.Run("custom vocabulary in embedded resource", func(t *testing.T) {
		schema, err := newCompiler(t).Compile(context.Background(), []byte(`{
			"$defs":{"custom":{
				"$id":"https://schemas.example.test/custom",
				"$schema":"https://schemas.example.test/even-meta",
				"even":true
			}},
			"$ref":"https://schemas.example.test/custom"
		}`))
		if err != nil {
			t.Fatal(err)
		}
		result, err := schema.Validate(context.Background(), []byte(`3`))
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid {
			t.Fatal("embedded custom vocabulary was ignored")
		}
	})

	t.Run("standard vocabulary in embedded resource", func(t *testing.T) {
		schema, err := newCompiler(t).Compile(context.Background(), []byte(`{
			"$schema":"https://schemas.example.test/even-meta",
			"$defs":{"standard":{
				"$id":"https://schemas.example.test/standard",
				"$schema":"https://json-schema.org/draft/2020-12/schema",
				"even":true
			}},
			"$ref":"https://schemas.example.test/standard"
		}`))
		if err != nil {
			t.Fatal(err)
		}
		result, err := schema.Validate(context.Background(), []byte(`3`))
		if err != nil {
			t.Fatal(err)
		}
		if !result.Valid {
			t.Fatal("custom vocabulary leaked into a standard resource")
		}
	})

	t.Run("built-in vocabularies are resource scoped", func(t *testing.T) {
		schema, err := newCompiler(t).Compile(context.Background(), []byte(`{
			"$defs":{"core":{
				"$id":"https://schemas.example.test/core",
				"$schema":"https://schemas.example.test/core-meta",
				"type":"integer"
			}},
			"$ref":"https://schemas.example.test/core"
		}`))
		if err != nil {
			t.Fatal(err)
		}
		result, err := schema.Validate(context.Background(), []byte(`"not an integer"`))
		if err != nil {
			t.Fatal(err)
		}
		if !result.Valid {
			t.Fatal("validation vocabulary leaked into a core-only resource")
		}

		schema, err = newCompiler(t).Compile(context.Background(), []byte(`{
			"$schema":"https://schemas.example.test/core-meta",
			"$defs":{"standard":{
				"$id":"https://schemas.example.test/standard-validation",
				"$schema":"https://json-schema.org/draft/2020-12/schema",
				"type":"integer"
			}},
			"$ref":"https://schemas.example.test/standard-validation"
		}`))
		if err != nil {
			t.Fatal(err)
		}
		result, err = schema.Validate(context.Background(), []byte(`"not an integer"`))
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid {
			t.Fatal("core-only policy leaked into a standard resource")
		}
	})
}
