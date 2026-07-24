package jsonschema_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestCompileRejectsInvalidAndAmbiguousJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "malformed", raw: []byte(`{"type":}`)},
		{name: "trailing", raw: []byte(`{} {}`)},
		{name: "duplicate member", raw: []byte(`{"type":"null","type":"string"}`)},
		{name: "invalid UTF-8", raw: []byte{'"', 0xff, '"'}},
	}

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := compiler.Compile(context.Background(), test.raw)
			if !errors.Is(err, jsonschema.ErrInvalidJSON) {
				t.Fatalf("got %v, want ErrInvalidJSON", err)
			}
		})
	}
}

func TestCompileValidatesSchemaAgainstSelectedMetaSchema(t *testing.T) {
	t.Parallel()

	for _, dialect := range []jsonschema.Dialect{
		jsonschema.Draft3,
		jsonschema.Draft4,
		jsonschema.Draft6,
		jsonschema.Draft7,
		jsonschema.Draft201909,
		jsonschema.Draft202012,
	} {
		dialect := dialect
		t.Run(string(dialect), func(t *testing.T) {
			t.Parallel()
			compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(dialect))
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), []byte(`{"title":7}`))
			if !errors.Is(err, jsonschema.ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
		})
	}
}

func TestCompileResolvesBundledOfficialMetaSchemaResources(t *testing.T) {
	t.Parallel()

	for _, dialect := range []jsonschema.Dialect{
		jsonschema.Draft3,
		jsonschema.Draft4,
		jsonschema.Draft6,
		jsonschema.Draft7,
		jsonschema.Draft201909,
		jsonschema.Draft202012,
	} {
		dialect := dialect
		t.Run(string(dialect), func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(dialect))
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(context.Background(), []byte(`{
				"$ref":"`+string(dialect)+`"
			}`))
			if err != nil {
				t.Fatal(err)
			}
			result, err := schema.Validate(context.Background(), []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid {
				t.Fatal("valid schema did not satisfy the bundled meta-schema")
			}
		})
	}
}

func TestCompileEnforcesInputLimits(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxInputBytes = 1
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}

	_, err = compiler.Compile(context.Background(), []byte(`{}`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestValidateUsesExactNumberSemantics(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft202012),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"type":"integer"}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	instances := []struct {
		raw   string
		valid bool
	}{
		{raw: `1234567890123456789012345678901234567890`, valid: true},
		{raw: `1e1000`, valid: true},
		{raw: `100e-2`, valid: true},
		{raw: `1e-1000`, valid: false},
		{raw: `0e-1000`, valid: true},
	}

	for _, instance := range instances {
		result, err := schema.Validate(
			context.Background(),
			[]byte(instance.raw),
		)
		if err != nil {
			t.Fatalf("%s: %v", instance.raw, err)
		}
		if result.Valid != instance.valid {
			t.Errorf(
				"%s: got valid=%t, want %t",
				instance.raw,
				result.Valid,
				instance.valid,
			)
		}
	}
}

func TestBooleanSchemaAvailabilityIsDialectSpecific(t *testing.T) {
	t.Parallel()

	for _, dialect := range []jsonschema.Dialect{
		jsonschema.Draft3,
		jsonschema.Draft4,
	} {
		compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(dialect))
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiler.Compile(context.Background(), []byte(`true`))
		if !errors.Is(err, jsonschema.ErrInvalidSchema) {
			t.Errorf("%s: got %v, want ErrInvalidSchema", dialect, err)
		}
	}

	for _, dialect := range []jsonschema.Dialect{
		jsonschema.Draft6,
		jsonschema.Draft7,
		jsonschema.Draft201909,
		jsonschema.Draft202012,
	} {
		compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(dialect))
		if err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(context.Background(), []byte(`false`))
		if err != nil {
			t.Fatal(err)
		}
		result, err := schema.Validate(context.Background(), []byte(`null`))
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid {
			t.Errorf("%s: false schema accepted instance", dialect)
		}
	}
}

func TestModernNumericBoundsRemainIndependent(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		schema   string
		instance string
	}{
		{schema: `{"minimum":10,"exclusiveMinimum":1}`, instance: `5`},
		{schema: `{"maximum":1,"exclusiveMaximum":10}`, instance: `5`},
	}

	for _, test := range tests {
		schema, err := compiler.Compile(
			context.Background(),
			[]byte(test.schema),
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
		if result.Valid {
			t.Errorf("schema %s accepted %s", test.schema, test.instance)
		}
	}
}

func TestMultipleOfHandlesLargeExponentsWithoutExpansion(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		divisor string
		valid   bool
	}{
		{divisor: `2`, valid: true},
		{divisor: `3`, valid: false},
	}

	for _, test := range tests {
		schema, err := compiler.Compile(
			context.Background(),
			[]byte(`{"multipleOf":`+test.divisor+`}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		result, err := schema.Validate(context.Background(), []byte(`1e10000`))
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid != test.valid {
			t.Errorf(
				"multipleOf %s: got valid=%t, want %t",
				test.divisor,
				result.Valid,
				test.valid,
			)
		}
	}
}

func TestValidateReturnsTypedEvaluationLimitErrors(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxUniqueComparisons = 1
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"uniqueItems":true}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = schema.Validate(context.Background(), []byte(`[1,2,3]`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	var limitError *jsonschema.LimitError
	if !errors.As(err, &limitError) {
		t.Fatalf("got %T, want *LimitError", err)
	}
	if limitError.Resource != "unique item comparisons" {
		t.Errorf("got resource %q", limitError.Resource)
	}
}

func TestValidateUniqueItemsUsesLinearDistinctItemWork(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxUniqueComparisons = 100
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{"uniqueItems":true}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(
		context.Background(),
		[]byte("["+integerSequence(100)+"]"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("distinct items unexpectedly failed uniqueItems")
	}
}

func TestValidateBoundsTotalEvaluationOperations(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxEvaluationOps = 1
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"properties":{"value":{"type":"string"}}}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = schema.Validate(
		context.Background(),
		[]byte(`{"value":"bounded"}`),
	)
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestCompilerBoundsSchemaStructureAndRegexWork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits func(*jsonschema.Limits)
		schema string
	}{
		{
			name:   "schema nodes",
			limits: func(limits *jsonschema.Limits) { limits.MaxSchemaNodes = 1 },
			schema: `{"properties":{"value":{"type":"string"}}}`,
		},
		{
			name:   "combinator branches",
			limits: func(limits *jsonschema.Limits) { limits.MaxCombinatorBranches = 1 },
			schema: `{"allOf":[true,true]}`,
		},
		{
			name:   "regular expression bytes",
			limits: func(limits *jsonschema.Limits) { limits.MaxRegexBytes = 3 },
			schema: `{"pattern":"abcd"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := jsonschema.DefaultLimits()
			test.limits(&limits)
			compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(context.Background(), []byte(test.schema))
			if !errors.Is(err, jsonschema.ErrLimitExceeded) {
				t.Fatalf("got %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestValidationBoundsReferenceDepthAndOutputUnits(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxReferenceDepth = 4
	compiler, err := jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{"$ref":"#"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.Validate(context.Background(), []byte(`null`))
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}

	limits = jsonschema.DefaultLimits()
	limits.MaxOutputUnits = 1
	compiler, err = jsonschema.NewCompiler(jsonschema.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	schema, err = compiler.Compile(
		context.Background(),
		[]byte(`{"title":"one","description":"two"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.ValidateOutput(
		context.Background(),
		[]byte(`null`),
		jsonschema.OutputVerbose,
	)
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestCompilerLoadsOnlyExplicitSchemaResources(t *testing.T) {
	t.Parallel()

	const identifier = "https://schemas.example.test/integer"
	compiler, err := jsonschema.NewCompiler(jsonschema.WithResourceLoader(
		jsonschema.ResourceLoaderFunc(func(ctx context.Context, requested string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if requested != identifier {
				t.Fatalf("got resource %q, want %q", requested, identifier)
			}
			return []byte(`{"$id":"` + identifier + `","type":"integer"}`), nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"$ref":"`+identifier+`"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`"invalid"`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("string unexpectedly satisfied loaded integer schema")
	}
}

func TestCompileClassifiesUnavailableSchemaResources(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(
		context.Background(),
		[]byte(`{"$ref":"https://schemas.example.test/missing"}`),
	)
	if !errors.Is(err, jsonschema.ErrResourceUnavailable) {
		t.Fatalf("got %v, want ErrResourceUnavailable", err)
	}
}

func TestCompilerRejectsTypedNilResourceLoader(t *testing.T) {
	t.Parallel()

	var loader jsonschema.ResourceLoaderFunc
	_, err := jsonschema.NewCompiler(jsonschema.WithResourceLoader(loader))
	if !errors.Is(err, jsonschema.ErrResourceUnavailable) {
		t.Fatalf("got %v, want ErrResourceUnavailable", err)
	}
}

func TestContentKeywordsAreAnnotationsByDefault(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(jsonschema.Draft7))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"contentMediaType":"application/json"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`"not JSON"`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("contentMediaType unexpectedly asserted without explicit policy")
	}
}

func TestCompilerBoundsLoadedSchemaResources(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxSchemaResources = 1
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithLimits(limits),
		jsonschema.WithResourceLoader(jsonschema.ResourceLoaderFunc(
			func(context.Context, string) ([]byte, error) {
				return []byte(`true`), nil
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(
		context.Background(),
		[]byte(`{"$ref":"https://schemas.example.test/remote"}`),
	)
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestCompileRejectsUnknownRequiredVocabulary(t *testing.T) {
	t.Parallel()

	const metaSchema = "https://schemas.example.test/custom-meta"
	compiler, err := jsonschema.NewCompiler(jsonschema.WithResourceLoader(
		jsonschema.ResourceLoaderFunc(func(context.Context, string) ([]byte, error) {
			return []byte(`{
				"$id":"` + metaSchema + `",
				"$vocabulary":{"https://vocab.example.test/required":true}
			}`), nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(
		context.Background(),
		[]byte(`{"$schema":"`+metaSchema+`"}`),
	)
	if !errors.Is(err, jsonschema.ErrUnsupportedVocabulary) {
		t.Fatalf("got %v, want ErrUnsupportedVocabulary", err)
	}
}

func TestFormatAssertionIsExplicitAndCompilerOwned(t *testing.T) {
	t.Parallel()

	annotationCompiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	annotationSchema, err := annotationCompiler.Compile(
		context.Background(),
		[]byte(`{"format":"uuid"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	annotationResult, err := annotationSchema.Validate(
		context.Background(),
		[]byte(`"not-a-uuid"`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !annotationResult.Valid {
		t.Fatal("default format annotation unexpectedly asserted")
	}

	assertionCompiler, err := jsonschema.NewCompiler(
		jsonschema.WithFormatAssertion(),
		jsonschema.WithFormat("project-code", jsonschema.FormatFunc(
			func(ctx context.Context, value string) (bool, error) {
				if err := ctx.Err(); err != nil {
					return false, err
				}
				return value == "valid", nil
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertionSchema, err := assertionCompiler.Compile(
		context.Background(),
		[]byte(`{"format":"project-code"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertionResult, err := assertionSchema.Validate(
		context.Background(),
		[]byte(`"invalid"`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if assertionResult.Valid {
		t.Fatal("custom asserted format accepted invalid value")
	}
}

func TestFormatChecksAreBoundedAndPreserveErrors(t *testing.T) {
	t.Parallel()

	limits := jsonschema.DefaultLimits()
	limits.MaxFormatChecks = 1
	checkerError := errors.New("format checker failed")
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithLimits(limits),
		jsonschema.WithFormatAssertion(),
		jsonschema.WithFormat("failing", jsonschema.FormatFunc(
			func(context.Context, string) (bool, error) {
				return false, checkerError
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"format":"failing"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.Validate(context.Background(), []byte(`"value"`))
	if !errors.Is(err, checkerError) {
		t.Fatalf("got %v, want checker error", err)
	}

	compiler, err = jsonschema.NewCompiler(
		jsonschema.WithLimits(limits),
		jsonschema.WithFormatAssertion(),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err = compiler.Compile(
		context.Background(),
		[]byte(`{"allOf":[{"format":"uuid"},{"format":"uuid"}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = schema.Validate(
		context.Background(),
		[]byte(`"123e4567-e89b-12d3-a456-426614174000"`),
	)
	if !errors.Is(err, jsonschema.ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestExtensionCallbackErrorsAreRedactedAndPreserved(t *testing.T) {
	t.Parallel()

	const secret = "sensitive callback error"
	callbackError := errors.New(secret)
	for _, test := range []struct {
		name    string
		options []jsonschema.Option
		schema  string
		value   string
	}{
		{
			name: "keyword compiler",
			options: []jsonschema.Option{
				jsonschema.WithDialect(jsonschema.Draft7),
				jsonschema.WithVocabulary(
					"https://vocab.example.test/error",
					map[string]jsonschema.KeywordCompiler{
						"failing": jsonschema.KeywordCompilerFunc(func(
							context.Context,
							jsonschema.Dialect,
							jsonschema.Value,
						) (jsonschema.KeywordEvaluator, error) {
							return nil, callbackError
						}),
					},
				),
			},
			schema: `{"failing":true}`,
		},
		{
			name: "keyword evaluator",
			options: []jsonschema.Option{
				jsonschema.WithDialect(jsonschema.Draft7),
				jsonschema.WithVocabulary(
					"https://vocab.example.test/error",
					map[string]jsonschema.KeywordCompiler{
						"failing": jsonschema.KeywordCompilerFunc(func(
							context.Context,
							jsonschema.Dialect,
							jsonschema.Value,
						) (jsonschema.KeywordEvaluator, error) {
							return jsonschema.KeywordEvaluatorFunc(func(
								context.Context,
								jsonschema.Value,
							) (jsonschema.KeywordResult, error) {
								return jsonschema.KeywordResult{}, callbackError
							}), nil
						}),
					},
				),
			},
			schema: `{"failing":true}`,
			value:  `null`,
		},
		{
			name: "format checker",
			options: []jsonschema.Option{
				jsonschema.WithFormatAssertion(),
				jsonschema.WithFormat("failing", jsonschema.FormatFunc(func(
					context.Context,
					string,
				) (bool, error) {
					return false, callbackError
				})),
			},
			schema: `{"format":"failing"}`,
			value:  `"value"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, err := jsonschema.NewCompiler(test.options...)
			if err != nil {
				t.Fatal(err)
			}
			requireRedactedCallbackError(t, secret, callbackError, func() error {
				compiled, err := compiler.Compile(
					context.Background(),
					[]byte(test.schema),
				)
				if err != nil || test.value == "" {
					return err
				}
				_, err = compiled.Validate(context.Background(), []byte(test.value))
				return err
			})
		})
	}
}

func TestCompilerRejectsTypedNilFormatChecker(t *testing.T) {
	t.Parallel()

	var checker jsonschema.FormatFunc
	_, err := jsonschema.NewCompiler(jsonschema.WithFormat("nil", checker))
	if !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
}

func TestCustomCallbackPanicsAreContainedAndRedacted(t *testing.T) {
	t.Parallel()

	const panicValue = "sensitive callback panic"
	panicCompiler := jsonschema.KeywordCompilerFunc(func(
		context.Context,
		jsonschema.Dialect,
		jsonschema.Value,
	) (jsonschema.KeywordEvaluator, error) {
		panic(panicValue)
	})
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft7),
		jsonschema.WithVocabulary(
			"https://vocab.example.test/panic",
			map[string]jsonschema.KeywordCompiler{"panicCompile": panicCompiler},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("keyword compilation", func(t *testing.T) {
		requireContainedCallbackPanic(t, panicValue, func() error {
			_, err := compiler.Compile(
				context.Background(),
				[]byte(`{"panicCompile":true}`),
			)
			return err
		})
	})

	panicEvaluator := jsonschema.KeywordCompilerFunc(func(
		context.Context,
		jsonschema.Dialect,
		jsonschema.Value,
	) (jsonschema.KeywordEvaluator, error) {
		return jsonschema.KeywordEvaluatorFunc(func(
			context.Context,
			jsonschema.Value,
		) (jsonschema.KeywordResult, error) {
			panic(panicValue)
		}), nil
	})
	compiler, err = jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft7),
		jsonschema.WithVocabulary(
			"https://vocab.example.test/panic",
			map[string]jsonschema.KeywordCompiler{"panicEvaluate": panicEvaluator},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		context.Background(),
		[]byte(`{"panicEvaluate":true}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func() error
	}{
		{
			name: "flag evaluation",
			call: func() error {
				_, err := schema.Validate(context.Background(), []byte(`null`))
				return err
			},
		},
		{
			name: "standard output",
			call: func() error {
				_, err := schema.ValidateOutput(
					context.Background(),
					[]byte(`null`),
					jsonschema.OutputBasic,
				)
				return err
			},
		},
		{
			name: "annotation collection",
			call: func() error {
				_, err := schema.CollectAnnotations(
					context.Background(),
					[]byte(`null`),
				)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireContainedCallbackPanic(t, panicValue, test.call)
		})
	}

	compiler, err = jsonschema.NewCompiler(
		jsonschema.WithFormatAssertion(),
		jsonschema.WithFormat("panic", jsonschema.FormatFunc(func(
			context.Context,
			string,
		) (bool, error) {
			panic(panicValue)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err = compiler.Compile(
		context.Background(),
		[]byte(`{"format":"panic"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func() error
	}{
		{
			name: "format flag evaluation",
			call: func() error {
				_, err := schema.Validate(context.Background(), []byte(`"value"`))
				return err
			},
		},
		{
			name: "format standard output",
			call: func() error {
				_, err := schema.ValidateOutput(
					context.Background(),
					[]byte(`"value"`),
					jsonschema.OutputBasic,
				)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireContainedCallbackPanic(t, panicValue, test.call)
		})
	}
}

func requireContainedCallbackPanic(
	t *testing.T,
	panicValue string,
	call func() error,
) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("callback panic escaped: %v", recovered)
		}
	}()

	err := call()
	if !errors.Is(err, jsonschema.ErrCallbackPanic) {
		t.Errorf("got %v, want ErrCallbackPanic", err)
	}
	if err != nil && strings.Contains(err.Error(), panicValue) {
		t.Errorf("callback panic value leaked: %v", err)
	}
}

func requireRedactedCallbackError(
	t *testing.T,
	secret string,
	cause error,
	call func() error,
) {
	t.Helper()
	err := call()
	if !errors.Is(err, cause) {
		t.Errorf("got %v, want callback cause", err)
	}
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Errorf("callback error leaked: %v", err)
	}
}
