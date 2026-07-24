package jsonschema_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

type panickingFS struct{}

func (panickingFS) Open(string) (fs.File, error) {
	panic("sensitive filesystem panic")
}

func TestMapLoaderCopiesResourcesAndReturnedBytes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"type":"integer"}`)
	loader, err := jsonschema.NewMapLoader(map[string][]byte{
		"https://schemas.example.test/value": raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw[2] = 'X'
	loaded, err := loader.Load(
		context.Background(),
		"https://schemas.example.test/value",
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded[2] = 'X'
	again, err := loader.Load(
		context.Background(),
		"https://schemas.example.test/value",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != `{"type":"integer"}` {
		t.Fatalf("loader bytes were aliased: %s", again)
	}
}

func TestMapLoaderUsesNormalizedResourceIdentity(t *testing.T) {
	t.Parallel()

	loader, err := jsonschema.NewMapLoader(map[string][]byte{
		"HTTPS://EXAMPLE.TEST:443/a/../%7Eschema": []byte(`true`),
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(
		context.Background(),
		"https://example.test/~schema",
	)
	if err != nil || string(loaded) != "true" {
		t.Fatalf("got %s, %v", loaded, err)
	}

	_, err = jsonschema.NewMapLoader(map[string][]byte{
		"HTTPS://EXAMPLE.TEST:443/%7Eschema": []byte(`true`),
		"https://example.test/~schema":       []byte(`false`),
	})
	if !errors.Is(err, jsonschema.ErrResourceUnavailable) {
		t.Fatalf("got %v, want duplicate normalized identifier error", err)
	}
	if _, err := loader.Load(
		context.Background(),
		"https://example.test/?%",
	); !errors.Is(err, jsonschema.ErrResourceNotFound) {
		t.Fatalf("got %v, want invalid identifier error", err)
	}
}

func TestLoaderPanicsAreContainedAndRedacted(t *testing.T) {
	t.Parallel()

	const panicValue = "sensitive loader panic"
	panickingLoader := jsonschema.ResourceLoaderFunc(func(
		context.Context,
		string,
	) ([]byte, error) {
		panic(panicValue)
	})
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithResourceLoader(panickingLoader),
	)
	if err != nil {
		t.Fatal(err)
	}
	requireContainedCallbackPanic(t, panicValue, func() error {
		_, err := compiler.Compile(
			context.Background(),
			[]byte(`{"$ref":"https://schemas.example.test/panic"}`),
		)
		return err
	})

	composite, err := jsonschema.NewCompositeLoader(panickingLoader)
	if err != nil {
		t.Fatal(err)
	}
	requireContainedCallbackPanic(t, panicValue, func() error {
		_, err := composite.Load(
			context.Background(),
			"https://schemas.example.test/panic",
		)
		return err
	})

	filesystem, err := jsonschema.NewFSLoader(
		"https://schemas.example.test/",
		panickingFS{},
	)
	if err != nil {
		t.Fatal(err)
	}
	requireContainedCallbackPanic(t, "sensitive filesystem panic", func() error {
		_, err := filesystem.Load(
			context.Background(),
			"https://schemas.example.test/panic",
		)
		return err
	})
}

func TestLoaderErrorsAreRedactedAndPreserved(t *testing.T) {
	t.Parallel()

	const secret = "sensitive loader error"
	loaderError := errors.New(secret)
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithResourceLoader(jsonschema.ResourceLoaderFunc(func(
			context.Context,
			string,
		) ([]byte, error) {
			return nil, loaderError
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	requireRedactedCallbackError(t, secret, loaderError, func() error {
		_, err := compiler.Compile(
			context.Background(),
			[]byte(`{"$ref":"https://schemas.example.test/failure"}`),
		)
		return err
	})
}

func TestFSLoaderConfinesResourcesToItsBase(t *testing.T) {
	t.Parallel()

	loader, err := jsonschema.NewFSLoader(
		"HTTPS://SCHEMAS.EXAMPLE.TEST:443/base/",
		fstest.MapFS{
			"nested/schema.json": &fstest.MapFile{Data: []byte(`true`)},
			"secret.json":        &fstest.MapFile{Data: []byte(`false`)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(
		context.Background(),
		"https://schemas.example.test/base/nested/schema.json",
	)
	if err != nil || string(loaded) != "true" {
		t.Fatalf("got %s, %v", loaded, err)
	}
	for _, identifier := range []string{
		"https://other.example.test/base/nested/schema.json",
		"https://schemas.example.test/base/../secret.json",
		"https://schemas.example.test/base/%2e%2e/secret.json",
	} {
		_, err := loader.Load(context.Background(), identifier)
		if !errors.Is(err, jsonschema.ErrResourceNotFound) {
			t.Errorf("%q: got %v, want ErrResourceNotFound", identifier, err)
		}
	}
}

func TestCompositeLoaderFallsThroughOnlyForMissingResources(t *testing.T) {
	t.Parallel()

	missing, err := jsonschema.NewMapLoader(nil)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := jsonschema.NewMapLoader(map[string][]byte{"urn:test": []byte(`true`)})
	if err != nil {
		t.Fatal(err)
	}
	loader, err := jsonschema.NewCompositeLoader(missing, fallback)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(context.Background(), "urn:test")
	if err != nil || string(loaded) != "true" {
		t.Fatalf("got %s, %v", loaded, err)
	}

	denied := jsonschema.ResourceLoaderFunc(
		func(context.Context, string) ([]byte, error) { return nil, fs.ErrPermission },
	)
	loader, err = jsonschema.NewCompositeLoader(denied, fallback)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loader.Load(context.Background(), "urn:test")
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("got %v, want permission error", err)
	}
}

func TestResolutionErrorsRedactURISecrets(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler(jsonschema.WithResourceLoader(
		jsonschema.ResourceLoaderFunc(func(context.Context, string) ([]byte, error) {
			return nil, fs.ErrNotExist
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(
		context.Background(),
		[]byte(`{"$ref":"https://user:secret@example.test/schema?token=secret"}`),
	)
	if !errors.Is(err, jsonschema.ErrResourceUnavailable) {
		t.Fatalf("got %v, want ErrResourceUnavailable", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") {
		t.Fatalf("resolution error leaked URI credentials: %v", err)
	}
}
