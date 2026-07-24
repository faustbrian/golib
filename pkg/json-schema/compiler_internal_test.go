package jsonschema

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

type structLoader struct{}

func (structLoader) Load(context.Context, string) ([]byte, error) {
	return []byte(`true`), nil
}

type structFormat struct{}

func (structFormat) Valid(context.Context, string) (bool, error) {
	return true, nil
}

func TestCompilerInternalOptionAndLimitEdges(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxInputBytes = 0
	if _, err := NewCompiler(WithLimits(limits)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	if !resourceLoaderIsNil(nil) || resourceLoaderIsNil(structLoader{}) {
		t.Fatal("unexpected resource loader nil classification")
	}
	if !formatCheckerIsNil(nil) || formatCheckerIsNil(structFormat{}) {
		t.Fatal("unexpected format checker nil classification")
	}
	if !interfaceIsNil(nil) || interfaceIsNil(structLoader{}) {
		t.Fatal("unexpected interface nil classification")
	}

	var compiler *Compiler
	if _, err := compiler.Compile(context.Background(), []byte(`true`)); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
	compiler, err := NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	compiler.limits.MaxTotalSchemaBytes = 1
	if _, err := compiler.Compile(context.Background(), []byte(`true`)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}

	_, err = newCompiler(compilerConfig{
		dialect:      Draft202012,
		limits:       DefaultLimits(),
		formats:      standardFormats(),
		vocabularies: make(map[string]registeredVocabulary),
	}, func(Dialect) (*Schema, error) {
		return nil, fs.ErrNotExist
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want meta-schema preparation failure", err)
	}

	metaFailure := errors.New("meta-schema evaluator failed")
	compiler = &Compiler{
		dialect: Draft202012,
		limits:  DefaultLimits(),
		metaSchema: &Schema{plan: &schemaPlan{custom: []compiledKeyword{{
			name: "failure",
			evaluator: KeywordEvaluatorFunc(func(
				context.Context, Value,
			) (KeywordResult, error) {
				return KeywordResult{}, metaFailure
			}),
		}}}},
	}
	if _, err := compiler.Compile(context.Background(), []byte(`true`)); !errors.Is(err, metaFailure) {
		t.Fatalf("got %v, want meta-schema evaluation failure", err)
	}
}

func TestOfficialMetaSchemaLoaderEdges(t *testing.T) {
	t.Parallel()

	if _, err := compileOfficialMetaSchema("unsupported"); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("got %v, want ErrUnsupportedDialect", err)
	}
	if _, err := compileOfficialMetaSchemaFrom(
		Draft202012,
		fstest.MapFS{},
		map[string]string{string(Draft202012): "missing.json"},
	); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want embedded read failure", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadOfficialMetaSchema(ctx, string(Draft202012)); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	for _, identifier := range []string{
		"%",
		"https://example.test/?%",
		"https://example.test/unknown",
	} {
		if _, err := loadOfficialMetaSchema(context.Background(), identifier); err == nil {
			t.Fatalf("expected loader error for %q", identifier)
		}
	}
	if raw, err := loadOfficialMetaSchema(
		context.Background(), string(Draft202012)+"#fragment",
	); err != nil || len(raw) == 0 {
		t.Fatalf("got %d bytes, err=%v", len(raw), err)
	}
}
