package avro

import (
	"context"
	"errors"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type stepContext struct{ calls int }

func (*stepContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stepContext) Done() <-chan struct{}       { return nil }
func (*stepContext) Value(any) any               { return nil }
func (ctx *stepContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestCanonicalizerErrorBoundaries(t *testing.T) {
	t.Parallel()

	definition := schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name       string
		ctx        context.Context
		adapter    *Canonicalizer
		definition schemaregistry.Definition
	}{
		{"canceled", canceled, New(16), definition},
		{"nil adapter", context.Background(), nil, definition},
		{"invalid limit", context.Background(), New(0), definition},
		{"wrong format", context.Background(), New(16), schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: definition.Content}},
		{"oversized", context.Background(), New(1), definition},
		{"canceled after parse", &stepContext{}, New(16), definition},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.adapter.Canonicalize(test.ctx, test.definition); err == nil {
				t.Fatal("Canonicalize() error = nil")
			}
		})
	}
	if _, err := New(16).Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record"}`),
	}); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Canonicalize(invalid) error = %v", err)
	}
	if _, err := New(0).Canonicalize(context.Background(), definition); err == nil ||
		err.Error() != "invalid Avro canonicalizer" {
		t.Fatalf("Canonicalize(invalid limit) error = %v", err)
	}
}
