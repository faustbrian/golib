package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type canonicalizerFunc func(context.Context, schemaregistry.Definition) ([]byte, error)

type pointerCanonicalizer struct{}

func (*pointerCanonicalizer) Canonicalize(context.Context, schemaregistry.Definition) ([]byte, error) {
	return []byte("canonical"), nil
}

func (fn canonicalizerFunc) Canonicalize(
	ctx context.Context,
	definition schemaregistry.Definition,
) ([]byte, error) {
	return fn(ctx, definition)
}

func TestCompileCreatesPortableIdentityWithoutAliasingCallerData(t *testing.T) {
	t.Parallel()

	content := []byte(`{"type":"string"}`)
	metadata := map[string]string{"owner": "orders"}
	definition := schemaregistry.Definition{
		Format:   schemaregistry.FormatJSONSchema,
		Content:  content,
		Metadata: metadata,
	}

	schema, err := schemaregistry.Compile(
		context.Background(),
		definition,
		canonicalizerFunc(func(_ context.Context, got schemaregistry.Definition) ([]byte, error) {
			if string(got.Content) != string(content) {
				t.Fatalf("canonicalizer content = %q, want %q", got.Content, content)
			}
			return []byte(`{"type":"string"}`), nil
		}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	content[0] = 'x'
	metadata["owner"] = "payments"
	got := schema.Definition()
	got.Content[0] = 'y'
	got.Metadata["owner"] = "shipping"

	wantFingerprint := "sha256:82b5ce4e6a57d9d1707cab819781e7ace6a920b1da6e1def3de0fcd2bf91cd64"
	if schema.Fingerprint().String() != wantFingerprint {
		t.Fatalf("Fingerprint() = %q, want %q", schema.Fingerprint(), wantFingerprint)
	}
	if string(schema.Definition().Content) != `{"type":"string"}` {
		t.Fatalf("Definition().Content was aliased: %q", schema.Definition().Content)
	}
	if schema.Definition().Metadata["owner"] != "orders" {
		t.Fatalf("Definition().Metadata was aliased: %q", schema.Definition().Metadata["owner"])
	}
}

func TestCompileRejectsInvalidDefinitionAndCanonicalizerFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		definition    schemaregistry.Definition
		canonicalizer schemaregistry.Canonicalizer
		want          error
	}{
		"unknown format": {
			definition:    schemaregistry.Definition{Format: "yaml", Content: []byte("type: string")},
			canonicalizer: canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) { return []byte("x"), nil }),
			want:          schemaregistry.ErrUnsupportedFormat,
		},
		"empty content": {
			definition:    schemaregistry.Definition{Format: schemaregistry.FormatAvro},
			canonicalizer: canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) { return []byte("x"), nil }),
			want:          schemaregistry.ErrInvalidSchema,
		},
		"nil canonicalizer": {
			definition: schemaregistry.Definition{Format: schemaregistry.FormatProtobuf, Content: []byte("message A {}")},
			want:       schemaregistry.ErrUnsupportedFormat,
		},
		"canonicalization failure": {
			definition: schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
			canonicalizer: canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) {
				return nil, errors.New("parse failed")
			}),
			want: schemaregistry.ErrInvalidSchema,
		},
		"empty canonical form": {
			definition:    schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
			canonicalizer: canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, nil }),
			want:          schemaregistry.ErrInvalidSchema,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := schemaregistry.Compile(context.Background(), test.definition, test.canonicalizer)
			if !errors.Is(err, test.want) {
				t.Fatalf("Compile() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCompileHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := schemaregistry.Compile(
		ctx,
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`true`)},
		canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) {
			called = true
			return []byte("true"), nil
		}),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("canonicalizer called after cancellation")
	}
}

func TestCompileWithLimitsRejectsSchemaBombsBeforeIdentity(t *testing.T) {
	t.Parallel()

	called := false
	_, err := schemaregistry.CompileWithLimits(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
		canonicalizerFunc(func(context.Context, schemaregistry.Definition) ([]byte, error) {
			called = true
			return []byte(`"string"`), nil
		}),
		schemaregistry.CompileLimits{MaxSchemaBytes: 4, MaxCanonicalBytes: 4, MaxReferences: 1, MaxMetadata: 1},
	)
	if !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("CompileWithLimits() error = %v, want ErrLimitExceeded", err)
	}
	if called {
		t.Fatal("canonicalizer called after input size limit")
	}
}

func TestCompileRejectsTypedNilCanonicalizer(t *testing.T) {
	t.Parallel()

	var canonicalizer *pointerCanonicalizer
	_, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
		canonicalizer,
	)
	if !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("Compile() error = %v, want ErrUnsupportedFormat", err)
	}
}
