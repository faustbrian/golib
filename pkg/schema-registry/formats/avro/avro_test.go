package avro_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
)

func TestCanonicalizerUsesAvroParsingCanonicalForm(t *testing.T) {
	t.Parallel()

	canonicalizer := registryavro.New(4096)
	left, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record","name":"Event","doc":"ignored","fields":[{"name":"id","type":"long"}]}`)},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(left) error = %v", err)
	}
	right, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"name":"Event","type":"record","fields":[{"type":"long","name":"id"}]}`)},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(right) error = %v", err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatalf("canonical fingerprints differ: %s != %s", left.Fingerprint(), right.Fingerprint())
	}

	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record"}`)},
		canonicalizer,
	)
	if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Compile(invalid) error = %v, want ErrInvalidSchema", err)
	}
}

func TestCanonicalizerRejectsExcessiveNestingBeforeAvroParsing(t *testing.T) {
	t.Parallel()

	canonicalizer := registryavro.New(4096)
	definition := schemaregistry.Definition{
		Format:  schemaregistry.FormatAvro,
		Content: []byte(strings.Repeat("[", 257) + `"string"` + strings.Repeat("]", 257)),
	}
	_, err := canonicalizer.Canonicalize(context.Background(), definition)
	if err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("Canonicalize(excessive nesting) error = %v, want nesting limit", err)
	}
}

func TestCanonicalizerAcceptsExactDepthAndWideSchemas(t *testing.T) {
	t.Parallel()

	const maxDepth = 256
	nested := strings.Repeat(`{"type":"array","items":`, maxDepth) + `"string"` +
		strings.Repeat("}", maxDepth)
	canonicalizer := registryavro.New(len(nested) + 1)
	if _, err := canonicalizer.Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(nested),
	}); err != nil {
		t.Fatalf("Canonicalize(exact nesting) error = %v", err)
	}

	var wide strings.Builder
	wide.WriteString(`{"type":"record","name":"Wide","fields":[`)
	for index := range 130 {
		if index != 0 {
			wide.WriteByte(',')
		}
		wide.WriteString(`{"name":"f`)
		wide.WriteString(strconv.Itoa(index))
		wide.WriteString(`","type":"string"}`)
	}
	wide.WriteString(`]}`)
	canonicalizer = registryavro.New(wide.Len() + 1)
	if _, err := canonicalizer.Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(wide.String()),
	}); err != nil {
		t.Fatalf("Canonicalize(wide schema) error = %v", err)
	}
}

func TestCanonicalizerRejectsMalformedJSONAndLateCancellation(t *testing.T) {
	t.Parallel()

	canonicalizer := registryavro.New(4096)
	_, err := canonicalizer.Canonicalize(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(`{"type":@}`),
	})
	if err == nil || !strings.Contains(err.Error(), "schema JSON") {
		t.Fatalf("Canonicalize(malformed JSON) error = %v", err)
	}

	ctx := &cancelAfterContext{cancelAt: 4}
	_, err = canonicalizer.Canonicalize(ctx, schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(`"string"`),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Canonicalize(late cancellation) error = %v, want context.Canceled", err)
	}
}

type cancelAfterContext struct {
	calls    atomic.Int32
	cancelAt int32
}

func (ctx *cancelAfterContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelAfterContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterContext) Value(any) any               { return nil }
func (ctx *cancelAfterContext) Err() error {
	if ctx.calls.Add(1) >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
