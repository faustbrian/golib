package jsonschema

import (
	"context"
	"errors"
	"testing"
)

func TestKeywordCompilerRejectsMalformedSchemasWithoutMetaSchema(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		dialect Dialect
		schema  string
	}{
		{name: "draft 3 boolean", dialect: Draft3, schema: `true`},
		{name: "non-object root", dialect: Draft202012, schema: `1`},
		{name: "reference type", dialect: Draft202012, schema: `{"$ref":1}`},
		{name: "missing reference", dialect: Draft202012, schema: `{"$ref":"#missing"}`},
		{name: "recursive reference type", dialect: Draft201909, schema: `{"$recursiveRef":1}`},
		{name: "missing recursive reference", dialect: Draft201909, schema: `{"$recursiveRef":"#missing"}`},
		{name: "dynamic reference type", dialect: Draft202012, schema: `{"$dynamicRef":1}`},
		{name: "missing dynamic reference", dialect: Draft202012, schema: `{"$dynamicRef":"#missing"}`},
		{name: "type", dialect: Draft202012, schema: `{"type":1}`},
		{name: "draft 3 disallow", dialect: Draft3, schema: `{"disallow":1}`},
		{name: "enum", dialect: Draft202012, schema: `{"enum":1}`},
		{name: "draft 3 required", dialect: Draft3, schema: `{"required":1}`},
		{name: "required", dialect: Draft202012, schema: `{"required":1}`},
		{name: "minimum", dialect: Draft202012, schema: `{"minimum":"x"}`},
		{name: "min length", dialect: Draft202012, schema: `{"minLength":-1}`},
		{name: "pattern type", dialect: Draft202012, schema: `{"pattern":1}`},
		{name: "pattern syntax", dialect: Draft202012, schema: `{"pattern":"("}`},
		{name: "format", dialect: Draft202012, schema: `{"format":1}`},
		{name: "content encoding", dialect: Draft202012, schema: `{"contentEncoding":1}`},
		{name: "content media type", dialect: Draft202012, schema: `{"contentMediaType":1}`},
		{name: "items", dialect: Draft202012, schema: `{"items":1}`},
		{name: "unevaluated items", dialect: Draft202012, schema: `{"unevaluatedItems":1}`},
		{name: "unevaluated properties", dialect: Draft202012, schema: `{"unevaluatedProperties":1}`},
		{name: "dependent required type", dialect: Draft202012, schema: `{"dependentRequired":1}`},
		{name: "dependent required entry", dialect: Draft202012, schema: `{"dependentRequired":{"x":1}}`},
		{name: "properties", dialect: Draft202012, schema: `{"properties":1}`},
		{name: "property schema", dialect: Draft202012, schema: `{"properties":{"x":1}}`},
		{name: "pattern properties", dialect: Draft202012, schema: `{"patternProperties":1}`},
		{name: "pattern property syntax", dialect: Draft202012, schema: `{"patternProperties":{"(":true}}`},
		{name: "pattern property schema", dialect: Draft202012, schema: `{"patternProperties":{"x":1}}`},
		{name: "additional properties", dialect: Draft202012, schema: `{"additionalProperties":1}`},
		{name: "property names", dialect: Draft202012, schema: `{"propertyNames":1}`},
		{name: "dependent schemas type", dialect: Draft202012, schema: `{"dependentSchemas":1}`},
		{name: "dependent schema", dialect: Draft202012, schema: `{"dependentSchemas":{"x":1}}`},
		{name: "dependencies type", dialect: Draft7, schema: `{"dependencies":1}`},
		{name: "dependency entry", dialect: Draft7, schema: `{"dependencies":{"x":1}}`},
		{name: "dependency item", dialect: Draft7, schema: `{"dependencies":{"x":[1]}}`},
		{name: "duplicate dependency", dialect: Draft7, schema: `{"dependencies":{"x":["a","a"]}}`},
		{name: "draft 3 extends array", dialect: Draft3, schema: `{"extends":[1]}`},
		{name: "draft 3 extends schema", dialect: Draft3, schema: `{"extends":1}`},
		{name: "empty all of", dialect: Draft202012, schema: `{"allOf":[]}`},
		{name: "all of schema", dialect: Draft202012, schema: `{"allOf":[1]}`},
		{name: "not schema", dialect: Draft202012, schema: `{"not":1}`},
		{name: "if schema", dialect: Draft202012, schema: `{"if":1}`},
		{name: "then schema", dialect: Draft202012, schema: `{"then":1}`},
		{name: "else schema", dialect: Draft202012, schema: `{"else":1}`},
		{name: "unique items", dialect: Draft202012, schema: `{"uniqueItems":1}`},
		{name: "contains schema", dialect: Draft202012, schema: `{"contains":1}`},
		{name: "minimum contains", dialect: Draft202012, schema: `{"minContains":-1}`},
		{name: "prefix item schema", dialect: Draft202012, schema: `{"prefixItems":[1]}`},
		{name: "draft 7 tuple item", dialect: Draft7, schema: `{"items":[1]}`},
		{name: "draft 7 additional items", dialect: Draft7, schema: `{"items":[true],"additionalItems":1}`},
		{name: "draft 7 items schema", dialect: Draft7, schema: `{"items":1}`},
		{name: "maximum", dialect: Draft202012, schema: `{"maximum":"x"}`},
		{name: "legacy exclusive type", dialect: Draft4, schema: `{"minimum":1,"exclusiveMinimum":1}`},
		{name: "legacy exclusive without bound", dialect: Draft4, schema: `{"exclusiveMaximum":true}`},
		{name: "exclusive minimum", dialect: Draft202012, schema: `{"exclusiveMinimum":"x"}`},
		{name: "exclusive maximum", dialect: Draft202012, schema: `{"exclusiveMaximum":"x"}`},
		{name: "multiple type", dialect: Draft202012, schema: `{"multipleOf":"x"}`},
		{name: "multiple zero", dialect: Draft202012, schema: `{"multipleOf":0}`},
		{name: "draft 4 empty required", dialect: Draft4, schema: `{"required":[]}`},
		{name: "required entry", dialect: Draft202012, schema: `{"required":[1]}`},
		{name: "duplicate required", dialect: Draft202012, schema: `{"required":["x","x"]}`},
		{name: "unknown type", dialect: Draft202012, schema: `{"type":"unknown"}`},
		{name: "empty type array", dialect: Draft202012, schema: `{"type":[]}`},
		{name: "unknown array type", dialect: Draft202012, schema: `{"type":["unknown"]}`},
		{name: "duplicate array type", dialect: Draft202012, schema: `{"type":["string","string"]}`},
		{name: "non-string array type", dialect: Draft202012, schema: `{"type":[{}]}`},
		{name: "draft 3 invalid schema type", dialect: Draft3, schema: `{"type":[1]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := compilerWithoutMetaSchema(test.dialect)
			if _, err := compiler.Compile(context.Background(), []byte(test.schema)); !errors.Is(err, ErrInvalidSchema) && !errors.Is(err, ErrResourceUnavailable) {
				t.Fatalf("got %v, want invalid schema", err)
			}
		})
	}
}

func compilerWithoutMetaSchema(dialect Dialect) *Compiler {
	return &Compiler{
		dialect:      dialect,
		limits:       DefaultLimits(),
		formats:      standardFormats(),
		vocabularies: make(map[string]registeredVocabulary),
	}
}

func TestKeywordCompilerPropagatesNestedReferenceFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		dialect Dialect
		schema  string
	}{
		{
			name:    "reference target",
			dialect: Draft202012,
			schema:  `{"$defs":{"bad":{"$ref":1}},"$ref":"#/$defs/bad"}`,
		},
		{
			name:    "recursive reference target",
			dialect: Draft201909,
			schema:  `{"definitions":{"bad":{"$ref":1}},"$recursiveRef":"#/definitions/bad"}`,
		},
		{
			name:    "dynamic reference target",
			dialect: Draft202012,
			schema:  `{"$defs":{"bad":{"$ref":1}},"$dynamicRef":"#/$defs/bad"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := compilerWithoutMetaSchema(test.dialect)
			if _, err := compiler.Compile(context.Background(), []byte(test.schema)); !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
		})
	}
}

func TestVocabularyPolicyRejectsMalformedCustomMetaSchemas(t *testing.T) {
	t.Parallel()

	const identifier = "https://schemas.example.test/custom-meta"
	for _, test := range []struct {
		name       string
		metaSchema string
	}{
		{name: "boolean meta-schema", metaSchema: `true`},
		{name: "vocabulary type", metaSchema: `{"$vocabulary":1}`},
		{name: "requirement type", metaSchema: `{"$vocabulary":{"https://example.test/v":"yes"}}`},
		{name: "unknown required vocabulary", metaSchema: `{"$vocabulary":{"https://example.test/v":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			loader, err := NewMapLoader(map[string][]byte{
				identifier: []byte(test.metaSchema),
			})
			if err != nil {
				t.Fatal(err)
			}
			compiler := compilerWithoutMetaSchema(Draft202012)
			compiler.loader = loader
			if _, err := compiler.Compile(
				context.Background(), []byte(`{"$schema":"`+identifier+`"}`),
			); err == nil {
				t.Fatal("expected vocabulary policy error")
			}
		})
	}
}

func TestVocabularyPolicyHandlesOptionalAndPartialDeclarations(t *testing.T) {
	t.Parallel()

	const identifier = "https://schemas.example.test/custom-meta"
	for _, metaSchema := range []string{
		`{}`,
		`{"$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/unevaluated":true}}`,
		`{"$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/unknown":false}}`,
	} {
		loader, err := NewMapLoader(map[string][]byte{identifier: []byte(metaSchema)})
		if err != nil {
			t.Fatal(err)
		}
		compiler := compilerWithoutMetaSchema(Draft202012)
		compiler.loader = loader
		if _, err := compiler.Compile(
			context.Background(), []byte(`{"$schema":"`+identifier+`"}`),
		); err != nil {
			t.Fatalf("%s: %v", metaSchema, err)
		}
	}

	compiler := compilerWithoutMetaSchema(Draft202012)
	if _, err := compiler.Compile(
		context.Background(), []byte(`{"$schema":"https://example.test/missing"}`),
	); !errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("got %v, want ErrResourceUnavailable", err)
	}
}

func TestSchemaPlanCompilationPropagatesVocabularyPolicyFailure(t *testing.T) {
	t.Parallel()

	root, err := decodeJSON(
		context.Background(),
		[]byte(`{"$schema":"https://example.test/missing"}`),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil,
		len(`{"$schema":"https://example.test/missing"}`), false, false,
		standardFormats(), nil,
	)
	if _, err := compiler.compile(root); !errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("got %v, want ErrResourceUnavailable", err)
	}
}

func TestDynamicAnchorCompilationPropagatesOtherResourceFailure(t *testing.T) {
	t.Parallel()

	compiler := compilerWithoutMetaSchema(Draft202012)
	_, err := compiler.Compile(context.Background(), []byte(`{
		"$id":"https://example.test/root",
		"$dynamicRef":"#node",
		"$defs":{
			"target":{"$dynamicAnchor":"node"},
			"bad":{
				"$id":"other",
				"$dynamicAnchor":"node",
				"$ref":1
			}
		}
	}`))
	if !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
}

func TestCustomKeywordCompilationFailuresAreBoundedAndClassified(t *testing.T) {
	t.Parallel()

	compileFailure := errors.New("compile callback failed")
	for _, test := range []struct {
		name     string
		compiler KeywordCompiler
		limit    int
	}{
		{
			name: "budget",
			compiler: KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				return KeywordEvaluatorFunc(func(
					context.Context, Value,
				) (KeywordResult, error) {
					return KeywordResult{Valid: true}, nil
				}), nil
			}),
			limit: 0,
		},
		{
			name: "callback error",
			compiler: KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				return nil, compileFailure
			}),
			limit: 1,
		},
		{
			name: "nil evaluator",
			compiler: KeywordCompilerFunc(func(
				context.Context, Dialect, Value,
			) (KeywordEvaluator, error) {
				return KeywordEvaluatorFunc(nil), nil
			}),
			limit: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := compilerWithoutMetaSchema(Draft7)
			compiler.limits.MaxCustomKeywordCompiles = test.limit
			compiler.vocabularies["https://example.test/v"] = registeredVocabulary{
				keywords: map[string]KeywordCompiler{"custom": test.compiler},
			}
			if _, err := compiler.Compile(
				context.Background(), []byte(`{"custom":true}`),
			); err == nil {
				t.Fatal("expected custom compilation error")
			}
		})
	}

	compiler := compilerWithoutMetaSchema(Draft202012)
	compiler.limits.MaxRegexCount = 1
	_, err := compiler.Compile(
		context.Background(),
		[]byte(`{"pattern":"x","patternProperties":{"y":true}}`),
	)
	if err == nil {
		t.Fatal("expected regular expression count error")
	}
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}
