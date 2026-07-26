package jsonschema_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestCustomKeywordValuesExposeExactReadOnlyViews(t *testing.T) {
	t.Parallel()

	probe := func(value jsonschema.Value) error {
		if value.Kind() != jsonschema.ObjectKind || value.Len() != 5 {
			return errors.New("expected five-member object")
		}
		wantNames := []string{"array", "boolean", "null", "number", "string"}
		names := value.Names()
		if len(names) != len(wantNames) {
			return errors.New("unexpected object names")
		}
		for index := range names {
			if names[index] != wantNames[index] {
				return errors.New("object names are not sorted")
			}
		}
		boolean, ok := value.Lookup("boolean")
		if !ok {
			return errors.New("boolean is missing")
		}
		if actual, matched := boolean.Bool(); !matched || !actual {
			return errors.New("boolean accessor failed")
		}
		number, _ := value.Lookup("number")
		if actual, matched := number.Number(); !matched || actual != "1e100" {
			return errors.New("number accessor failed")
		}
		text, _ := value.Lookup("string")
		if actual, matched := text.String(); !matched || actual != "value" {
			return errors.New("string accessor failed")
		}
		array, _ := value.Lookup("array")
		item, ok := array.Index(0)
		if !ok || item.Kind() != jsonschema.NullKind || array.Len() != 1 {
			return errors.New("array accessor failed")
		}
		if _, ok := array.Index(-1); ok {
			return errors.New("negative array index succeeded")
		}
		if _, ok := array.Index(1); ok {
			return errors.New("out-of-range array index succeeded")
		}
		if _, ok := value.Lookup("missing"); ok {
			return errors.New("missing object member succeeded")
		}
		for _, scalar := range []jsonschema.Value{boolean, number, text, item, {}} {
			if scalar.Len() != 0 || scalar.Names() != nil {
				return errors.New("scalar collection accessor succeeded")
			}
			if _, ok := scalar.Index(0); ok {
				return errors.New("scalar index succeeded")
			}
			if _, ok := scalar.Lookup("x"); ok {
				return errors.New("scalar lookup succeeded")
			}
		}
		if _, ok := number.Bool(); ok {
			return errors.New("mismatched boolean accessor succeeded")
		}
		if _, ok := boolean.Number(); ok {
			return errors.New("mismatched number accessor succeeded")
		}
		if _, ok := number.String(); ok {
			return errors.New("mismatched string accessor succeeded")
		}
		return nil
	}

	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(jsonschema.Draft7),
		jsonschema.WithVocabulary(
			"https://vocab.example.test/probe",
			map[string]jsonschema.KeywordCompiler{
				"probe": jsonschema.KeywordCompilerFunc(func(
					_ context.Context,
					_ jsonschema.Dialect,
					value jsonschema.Value,
				) (jsonschema.KeywordEvaluator, error) {
					if err := probe(value); err != nil {
						return nil, err
					}
					return jsonschema.KeywordEvaluatorFunc(func(
						_ context.Context,
						instance jsonschema.Value,
					) (jsonschema.KeywordResult, error) {
						return jsonschema.KeywordResult{Valid: probe(instance) == nil}, nil
					}), nil
				}),
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	document := []byte(`{"probe":{
		"boolean":true,"number":1e100,"string":"value","array":[null],"null":null
	}}`)
	schema, err := compiler.Compile(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(
		context.Background(),
		[]byte(`{"boolean":true,"number":1e100,"string":"value","array":[null],"null":null}`),
	)
	if err != nil || !result.Valid {
		t.Fatalf("got valid=%v, err=%v", result.Valid, err)
	}
}

func TestCompilerOptionsRejectInvalidRegistrations(t *testing.T) {
	t.Parallel()

	typedNilCompiler := jsonschema.KeywordCompilerFunc(nil)
	validCompiler := jsonschema.KeywordCompilerFunc(func(
		context.Context, jsonschema.Dialect, jsonschema.Value,
	) (jsonschema.KeywordEvaluator, error) {
		return jsonschema.KeywordEvaluatorFunc(func(
			context.Context, jsonschema.Value,
		) (jsonschema.KeywordResult, error) {
			return jsonschema.KeywordResult{Valid: true}, nil
		}), nil
	})
	for _, test := range []struct {
		name    string
		options []jsonschema.Option
	}{
		{name: "nil option", options: []jsonschema.Option{nil}},
		{name: "unsupported dialect", options: []jsonschema.Option{
			jsonschema.WithDialect("unsupported"),
		}},
		{name: "empty vocabulary identifier", options: []jsonschema.Option{
			jsonschema.WithVocabulary("", nil),
		}},
		{name: "relative vocabulary identifier", options: []jsonschema.Option{
			jsonschema.WithVocabulary("relative", nil),
		}},
		{name: "fragment vocabulary identifier", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/v#fragment", nil),
		}},
		{name: "empty custom keyword", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/v", map[string]jsonschema.KeywordCompiler{"": validCompiler}),
		}},
		{name: "built-in custom keyword", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/v", map[string]jsonschema.KeywordCompiler{"type": validCompiler}),
		}},
		{name: "nil custom compiler", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/v", map[string]jsonschema.KeywordCompiler{"custom": typedNilCompiler}),
		}},
		{name: "duplicate vocabulary", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/v", nil),
			jsonschema.WithVocabulary("https://example.test/v", nil),
		}},
		{name: "duplicate keyword", options: []jsonschema.Option{
			jsonschema.WithVocabulary("https://example.test/a", map[string]jsonschema.KeywordCompiler{"custom": validCompiler}),
			jsonschema.WithVocabulary("https://example.test/b", map[string]jsonschema.KeywordCompiler{"custom": validCompiler}),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := jsonschema.NewCompiler(test.options...); err == nil {
				t.Fatal("expected registration error")
			}
		})
	}
}

func TestLoaderConstructorsAndCancellationEdges(t *testing.T) {
	t.Parallel()

	if _, err := jsonschema.NewMapLoader(map[string][]byte{"": []byte(`true`)}); err == nil {
		t.Fatal("expected empty map identifier error")
	}
	if _, err := jsonschema.NewMapLoader(map[string][]byte{"%": []byte(`true`)}); err == nil {
		t.Fatal("expected malformed map identifier error")
	}
	var nilMap *jsonschema.MapLoader
	if _, err := nilMap.Load(context.Background(), "urn:test"); err == nil {
		t.Fatal("expected nil map loader error")
	}

	for _, base := range []string{
		"", "relative/", "file:///schemas/", "https://user@example.test/base/",
		"https://example.test/base", "https://example.test/base/?query",
		"https://example.test/base/#fragment",
	} {
		if _, err := jsonschema.NewFSLoader(base, fstest.MapFS{}); err == nil {
			t.Fatalf("expected invalid base error for %q", base)
		}
	}
	if _, err := jsonschema.NewFSLoader("https://example.test/base/", nil); err == nil {
		t.Fatal("expected nil filesystem error")
	}
	var nilFS *jsonschema.FSLoader
	if _, err := nilFS.Load(context.Background(), "https://example.test/base/x"); err == nil {
		t.Fatal("expected nil filesystem loader error")
	}
	loader, err := jsonschema.NewFSLoader(
		"https://example.test/base/",
		fstest.MapFS{
			"denied": &fstest.MapFile{Mode: fs.ModeDir},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, identifier := range []string{
		"%", "http://example.test/base/x", "https://user@example.test/base/x",
		"https://example.test/base/x?query", "https://example.test/base/x#fragment",
		"https://example.test/base/", "https://example.test/base/missing",
		"https://example.test/base/denied",
	} {
		if _, err := loader.Load(context.Background(), identifier); err == nil {
			t.Fatalf("expected load error for %q", identifier)
		}
	}

	if _, err := jsonschema.NewCompositeLoader(jsonschema.ResourceLoaderFunc(nil)); err == nil {
		t.Fatal("expected nil composite member error")
	}
	var nilComposite *jsonschema.CompositeLoader
	if _, err := nilComposite.Load(context.Background(), "urn:test"); err == nil {
		t.Fatal("expected nil composite loader error")
	}
	empty, err := jsonschema.NewCompositeLoader()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Load(context.Background(), "urn:user:secret@example.test?token=x"); !errors.Is(err, jsonschema.ErrResourceNotFound) {
		t.Fatalf("got %v, want ErrResourceNotFound", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mapLoader, err := jsonschema.NewMapLoader(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []jsonschema.ResourceLoader{mapLoader, loader} {
		if _, err := candidate.Load(ctx, "https://example.test/base/x"); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want cancellation", err)
		}
	}
}

func TestValidationEntryPointsRejectInvalidCallState(t *testing.T) {
	t.Parallel()

	var nilSchema *jsonschema.Schema
	for _, validate := range []func() error{
		func() error {
			_, err := nilSchema.ValidateValue(context.Background(), nil)
			return err
		},
		func() error {
			_, err := nilSchema.ValidateValueOutput(
				context.Background(), nil, jsonschema.OutputBasic,
			)
			return err
		},
		func() error {
			_, err := nilSchema.ValidateOutput(
				context.Background(), []byte(`null`), jsonschema.OutputBasic,
			)
			return err
		},
	} {
		if err := validate(); !errors.Is(err, jsonschema.ErrInvalidSchema) {
			t.Fatalf("got %v, want ErrInvalidSchema", err)
		}
	}

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`true`))
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if _, err := schema.ValidateValue(nilContext, nil); !errors.Is(err, jsonschema.ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := schema.ValidateValue(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if _, err := schema.ValidateValue(context.Background(), make(chan int)); !errors.Is(err, jsonschema.ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}
	if _, err := schema.ValidateOutput(
		context.Background(), []byte(`null`), jsonschema.OutputFormat("unknown"),
	); !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
	if _, err := schema.ValidateValueOutput(
		context.Background(), make(chan int), jsonschema.OutputBasic,
	); !errors.Is(err, jsonschema.ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}
	if (jsonschema.Value{}).Kind() != jsonschema.NullKind {
		t.Fatal("zero Value must be null")
	}
}
