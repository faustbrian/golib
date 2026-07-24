package serialize_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
)

func TestSerializersRejectWideValuesBeforeCopyingChildren(t *testing.T) {
	members := make([]jsonvalue.Member, 4096)
	for index := range members {
		members[index] = jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		}
	}
	wide, _ := jsonvalue.Object(members)
	for _, serializer := range []struct {
		name string
		run  func(context.Context, io.Writer, serialize.Source, serialize.Options) error
	}{
		{name: "JSON", run: serialize.JSON},
		{name: "YAML", run: serialize.YAML},
	} {
		t.Run(serializer.name, func(t *testing.T) {
			options := serialize.DefaultOptions()
			options.MaxNodes = 1
			const repetitions = 16
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			for range repetitions {
				err := serializer.run(
					context.Background(), io.Discard,
					semanticSource{wide}, options,
				)
				if !errors.Is(err, serialize.ErrLimitExceeded) {
					t.Fatalf("wide serialization error = %v", err)
				}
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
			if allocated > 64<<10 {
				t.Fatalf("wide rejected serialization allocated %d bytes per operation", allocated)
			}
		})
	}
}

func TestSerializersAcceptExactMinimumLimits(t *testing.T) {
	t.Parallel()

	one, _ := jsonvalue.Number("1")
	for _, serializer := range []struct {
		name       string
		run        func(context.Context, io.Writer, serialize.Source, serialize.Options) error
		exactBytes int
	}{
		{name: "JSON", run: serialize.JSON, exactBytes: 1},
		{name: "YAML", run: serialize.YAML, exactBytes: 2},
	} {
		t.Run(serializer.name, func(t *testing.T) {
			options := serialize.DefaultOptions()
			options.MaxDepth = 1
			options.MaxNodes = 1
			options.MaxBytes = serializer.exactBytes
			var output bytes.Buffer
			if err := serializer.run(
				context.Background(), &output, semanticSource{one}, options,
			); err != nil {
				t.Fatalf("exact minimum limits error = %v", err)
			}
		})
	}

	options := serialize.DefaultOptions()
	options.MaxBytes = 1
	var output bytes.Buffer
	err := serialize.YAML(
		context.Background(), &output, semanticSource{one}, options,
	)
	if !errors.Is(err, serialize.ErrLimitExceeded) || output.String() != "1" {
		t.Fatalf("one-byte YAML boundary = %q, %v", output.String(), err)
	}
}

type semanticSource struct {
	value jsonvalue.Value
}

func (source semanticSource) Raw() jsonvalue.Value {
	return source.value
}

func TestJSONSerializesEverySemanticKind(t *testing.T) {
	t.Parallel()

	text, _ := jsonvalue.String("line\nvalue")
	number, _ := jsonvalue.Number("-0.25e+2")
	array, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null(), jsonvalue.Boolean(false), text})
	object, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "values", Value: array}})
	tests := []struct {
		name  string
		value jsonvalue.Value
		want  string
	}{
		{name: "null", value: jsonvalue.Null(), want: "null"},
		{name: "false", value: jsonvalue.Boolean(false), want: "false"},
		{name: "true", value: jsonvalue.Boolean(true), want: "true"},
		{name: "number", value: number, want: "-0.25e+2"},
		{name: "string", value: text, want: `"line\nvalue"`},
		{name: "array", value: array, want: `[null,false,"line\nvalue"]`},
		{name: "object", value: object, want: `{"values":[null,false,"line\nvalue"]}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := serialize.JSON(context.Background(), &output, semanticSource{test.value}, serialize.DefaultOptions()); err != nil {
				t.Fatal(err)
			}
			if output.String() != test.want {
				t.Fatalf("JSON() = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestSerializersRejectInvalidPolicyAndSemanticRoots(t *testing.T) {
	t.Parallel()

	serializers := []struct {
		name string
		run  func(context.Context, io.Writer, serialize.Source, serialize.Options) error
	}{
		{name: "JSON", run: serialize.JSON},
		{name: "YAML", run: serialize.YAML},
	}
	valid := semanticSource{jsonvalue.Null()}
	for _, serializer := range serializers {
		serializer := serializer
		t.Run(serializer.name, func(t *testing.T) {
			t.Parallel()
			options := serialize.DefaultOptions()
			options.Mode = 99
			if err := serializer.run(context.Background(), io.Discard, valid, options); err == nil {
				t.Fatal("invalid mode was accepted")
			}
			for _, mutate := range []func(*serialize.Options){
				func(value *serialize.Options) { value.MaxBytes = 0 },
				func(value *serialize.Options) { value.MaxDepth = 0 },
				func(value *serialize.Options) { value.MaxNodes = 0 },
			} {
				options = serialize.DefaultOptions()
				mutate(&options)
				if err := serializer.run(context.Background(), io.Discard, valid, options); !errors.Is(err, serialize.ErrLimitExceeded) {
					t.Fatalf("invalid limits error = %v", err)
				}
			}
			if err := serializer.run(context.Background(), io.Discard, semanticSource{}, serialize.DefaultOptions()); err == nil {
				t.Fatal("invalid semantic root was accepted")
			}
			var writer *bytes.Buffer
			if err := serializer.run(context.Background(), writer, valid, serialize.DefaultOptions()); err == nil {
				t.Fatal("typed nil writer was accepted")
			}
			var source *pointerSource
			if err := serializer.run(context.Background(), io.Discard, source, serialize.DefaultOptions()); err == nil {
				t.Fatal("typed nil source was accepted")
			}
		})
	}
}

func TestYAMLRejectsNilContextAndSerializesScalarBranches(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if err := serialize.YAML(nil, io.Discard, semanticSource{jsonvalue.Null()}, serialize.DefaultOptions()); err == nil {
		t.Fatal("nil context was accepted")
	}
	integer, _ := jsonvalue.Number("42")
	for _, test := range []struct {
		value jsonvalue.Value
		want  string
	}{
		{value: jsonvalue.Boolean(false), want: "false\n"},
		{value: integer, want: "42\n"},
	} {
		var output bytes.Buffer
		if err := serialize.YAML(context.Background(), &output, semanticSource{test.value}, serialize.DefaultOptions()); err != nil {
			t.Fatal(err)
		}
		if output.String() != test.want {
			t.Fatalf("YAML() = %q, want %q", output.String(), test.want)
		}
	}
}

type pointerSource struct{}

func (*pointerSource) Raw() jsonvalue.Value {
	return jsonvalue.Null()
}

func TestSerializersEnforceDepthNodesCancellationAndWriterFailures(t *testing.T) {
	t.Parallel()

	child, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	nested, _ := jsonvalue.Array([]jsonvalue.Value{child})
	serializers := []struct {
		name string
		run  func(context.Context, io.Writer, serialize.Source, serialize.Options) error
	}{
		{name: "JSON", run: serialize.JSON},
		{name: "YAML", run: serialize.YAML},
	}
	for _, serializer := range serializers {
		serializer := serializer
		t.Run(serializer.name, func(t *testing.T) {
			t.Parallel()
			options := serialize.DefaultOptions()
			options.MaxDepth = 1
			if err := serializer.run(context.Background(), io.Discard, semanticSource{nested}, options); !errors.Is(err, serialize.ErrLimitExceeded) {
				t.Fatalf("depth limit error = %v", err)
			}
			options = serialize.DefaultOptions()
			options.MaxNodes = 1
			if err := serializer.run(context.Background(), io.Discard, semanticSource{nested}, options); !errors.Is(err, serialize.ErrLimitExceeded) {
				t.Fatalf("node limit error = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := serializer.run(ctx, io.Discard, semanticSource{nested}, serialize.DefaultOptions()); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			if err := serializer.run(context.Background(), failingWriter{err: io.ErrClosedPipe}, semanticSource{nested}, serialize.DefaultOptions()); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("writer error = %v", err)
			}
		})
	}
}

func TestJSONCompletesPartialWritesAndRejectsBrokenWriters(t *testing.T) {
	t.Parallel()

	value := semanticSource{jsonvalue.Boolean(true)}
	partial := &partialWriter{maximum: 1}
	if err := serialize.JSON(context.Background(), partial, value, serialize.DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	if partial.output.String() != "true" {
		t.Fatalf("partial output = %q", partial.output.String())
	}
	for _, writer := range []io.Writer{
		invalidCountWriter{count: -1},
		invalidCountWriter{count: 100},
		invalidCountWriter{count: 0},
	} {
		if err := serialize.JSON(context.Background(), writer, value, serialize.DefaultOptions()); !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("broken writer error = %v", err)
		}
	}
}

type partialWriter struct {
	maximum int
	output  bytes.Buffer
}

func (writer *partialWriter) Write(value []byte) (int, error) {
	if len(value) > writer.maximum {
		value = value[:writer.maximum]
	}
	return writer.output.Write(value)
}

type invalidCountWriter struct {
	count int
}

func (writer invalidCountWriter) Write([]byte) (int, error) {
	return writer.count, nil
}
