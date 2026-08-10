package protobuf

import (
	"context"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type countingContext struct {
	calls       int
	cancelAfter int
}

func (*countingContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*countingContext) Done() <-chan struct{}       { return nil }
func (*countingContext) Value(any) any               { return nil }
func (ctx *countingContext) Err() error {
	ctx.calls++
	if ctx.cancelAfter > 0 && ctx.calls >= ctx.cancelAfter {
		return context.Canceled
	}
	return nil
}

func TestConfigurationAndCanonicalizationBoundaries(t *testing.T) {
	t.Parallel()

	invalidConfigs := []Config{
		{},
		{Filename: "root.proto", MaxSchemaBytes: 16, MaxImports: 1, Imports: map[string]string{"a.proto": "", "b.proto": ""}},
		{Filename: "root.proto", MaxSchemaBytes: 16, MaxImports: 1, Imports: map[string]string{"": "x"}},
		{Filename: "root.proto", MaxSchemaBytes: 16, MaxImports: 1, Imports: map[string]string{"root.proto": "x"}},
		{Filename: "root.proto", MaxSchemaBytes: 1, MaxImports: 1, Imports: map[string]string{"a.proto": "xx"}},
	}
	for _, config := range invalidConfigs {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) error = nil", config)
		}
	}
	adapter, err := New(Config{Filename: "root.proto", MaxSchemaBytes: 128, MaxImports: 1})
	if err != nil {
		t.Fatal(err)
	}
	definition := schemaregistry.Definition{
		Format:  schemaregistry.FormatProtobuf,
		Content: []byte(`syntax = "proto3"; message M {}`),
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Canonicalize(canceled, definition); err == nil {
		t.Fatal("Canonicalize(canceled) error = nil")
	}
	wrong := definition
	wrong.Format = schemaregistry.FormatAvro
	if _, err := adapter.Canonicalize(context.Background(), wrong); err == nil {
		t.Fatal("Canonicalize(wrong format) error = nil")
	}
	oversized := definition
	oversized.Content = make([]byte, 129)
	if _, err := adapter.Canonicalize(context.Background(), oversized); err == nil {
		t.Fatal("Canonicalize(oversized) error = nil")
	}

	counting := &countingContext{}
	if _, err := adapter.Canonicalize(counting, definition); err != nil {
		t.Fatalf("Canonicalize(counting) error = %v", err)
	}
	lateCanceled := &countingContext{cancelAfter: counting.calls}
	if _, err := adapter.Canonicalize(lateCanceled, definition); err == nil {
		t.Fatal("Canonicalize(late canceled) error = nil")
	}
}
