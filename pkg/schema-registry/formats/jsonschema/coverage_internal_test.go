package jsonschema

import (
	"context"
	"errors"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type identityCanonicalizer struct{}

func (identityCanonicalizer) Canonicalize(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
	return definition.Content, nil
}

type steppedContext struct {
	calls       int
	cancelAfter int
}

func (*steppedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*steppedContext) Done() <-chan struct{}       { return nil }
func (*steppedContext) Value(any) any               { return nil }
func (ctx *steppedContext) Err() error {
	ctx.calls++
	if ctx.cancelAfter > 0 && ctx.calls >= ctx.cancelAfter {
		return context.Canceled
	}
	return nil
}

func TestAdapterConfigurationBoundaries(t *testing.T) {
	t.Parallel()

	invalid := []Config{
		{},
		{Dialect: "invalid", MaxSchemaBytes: 8, MaxTotalSchemaBytes: 8, MaxPayloadBytes: 8, MaxResources: 1},
		{MaxSchemaBytes: 8, MaxTotalSchemaBytes: 8, MaxPayloadBytes: 8, MaxResources: 1, Resources: map[string][]byte{"": []byte(`true`)}},
		{MaxSchemaBytes: 8, MaxTotalSchemaBytes: 8, MaxPayloadBytes: 8, MaxResources: 1, Resources: map[string][]byte{"x": nil}},
		{MaxSchemaBytes: 2, MaxTotalSchemaBytes: 8, MaxPayloadBytes: 8, MaxResources: 1, Resources: map[string][]byte{"x": []byte(`true`)}},
		{MaxSchemaBytes: 8, MaxTotalSchemaBytes: 3, MaxPayloadBytes: 8, MaxResources: 1, Resources: map[string][]byte{"x": []byte(`true`)}},
		{MaxSchemaBytes: 8, MaxTotalSchemaBytes: 16, MaxPayloadBytes: 8, MaxResources: 1, Resources: map[string][]byte{"x": []byte(`true`), "y": []byte(`true`)}},
		{MaxSchemaBytes: 8, MaxTotalSchemaBytes: 7, MaxPayloadBytes: 8, MaxResources: 2, Resources: map[string][]byte{"x": []byte(`true`), "y": []byte(`true`)}},
	}
	for _, config := range invalid {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) error = nil", config)
		}
	}
}

func TestAdapterCanonicalAndPayloadErrorBoundaries(t *testing.T) {
	t.Parallel()

	adapter, err := New(Config{
		MaxSchemaBytes: 64, MaxTotalSchemaBytes: 128, MaxPayloadBytes: 8, MaxResources: 2,
		Resources: map[string][]byte{"https://example.test/ref": []byte(`{"$id":"https://example.test/ref","type":"string"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	validSchema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`true`),
	}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Canonicalize(canceled, validSchema.Definition()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Canonicalize(canceled) error = %v", err)
	}
	wrong := validSchema.Definition()
	wrong.Format = schemaregistry.FormatAvro
	if _, err := adapter.Canonicalize(context.Background(), wrong); err == nil {
		t.Fatal("Canonicalize(wrong format) error = nil")
	}
	oversized := validSchema.Definition()
	oversized.Content = make([]byte, 65)
	if _, err := adapter.Canonicalize(context.Background(), oversized); err == nil {
		t.Fatal("Canonicalize(oversized) error = nil")
	}
	if _, err := adapter.Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":`),
	}); err == nil {
		t.Fatal("Canonicalize(invalid JSON) error = nil")
	}
	if _, err := adapter.Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"const":1e400}`),
	}); err == nil {
		t.Fatal("Canonicalize(non-I-JSON) error = nil")
	}
	if _, err := adapter.Encode(canceled, validSchema, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Encode(canceled) error = %v", err)
	}
	if _, err := adapter.Encode(context.Background(), validSchema, make(chan int)); err == nil {
		t.Fatal("Encode(unmarshalable) error = nil")
	}
	if _, err := adapter.Encode(context.Background(), validSchema, "12345678"); !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Encode(oversized) error = %v", err)
	}
	falseSchema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`false`),
	}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Encode(context.Background(), falseSchema, nil); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("Encode(invalid) error = %v", err)
	}
	if err := adapter.Decode(canceled, validSchema, []byte(`null`), new(any)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Decode(canceled) error = %v", err)
	}
	if err := adapter.Decode(context.Background(), validSchema, []byte(`"12345678"`), new(any)); !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Decode(oversized) error = %v", err)
	}
	if err := adapter.Decode(context.Background(), validSchema, []byte(`null`), make(chan int)); err == nil {
		t.Fatal("Decode(invalid target) error = nil")
	}
	var decoded any
	if err := adapter.Decode(context.Background(), validSchema, []byte(`null`), &decoded); err != nil {
		t.Fatalf("Decode(valid) error = %v", err)
	}
	for threshold := 2; threshold < 80; threshold++ {
		_ = adapter.validate(&steppedContext{cancelAfter: threshold}, validSchema, []byte(`null`))
	}
	if err := adapter.validate(context.Background(), schemaregistry.Schema{}, []byte(`null`)); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("validate(empty schema) error = %v", err)
	}
	wrongFormat, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(`true`),
	}, identityCanonicalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.validate(context.Background(), wrongFormat, []byte(`null`)); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("validate(wrong format) error = %v", err)
	}
	malformed, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatJSONSchema, Content: []byte(`not-json`),
	}, identityCanonicalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.validate(context.Background(), malformed, []byte(`null`)); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("validate(malformed schema) error = %v", err)
	}
}

func TestLocalResourceLoaderHonorsCancellation(t *testing.T) {
	t.Parallel()

	adapter, err := New(Config{
		MaxSchemaBytes: 128, MaxTotalSchemaBytes: 256, MaxPayloadBytes: 16, MaxResources: 2,
		Resources: map[string][]byte{"https://example.test/ref": []byte(`{"$id":"https://example.test/ref","type":"string"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := schemaregistry.Definition{
		Format:  schemaregistry.FormatJSONSchema,
		Content: []byte(`{"$ref":"https://example.test/ref"}`),
	}
	for threshold := 2; threshold < 40; threshold++ {
		_, _ = adapter.Canonicalize(&steppedContext{cancelAfter: threshold}, definition)
	}
}
