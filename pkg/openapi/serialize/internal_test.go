package serialize

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestJSONEmitterPropagatesFailuresAtEveryContainerBoundary(t *testing.T) {
	t.Parallel()

	null := jsonvalue.Null()
	array, _ := jsonvalue.Array([]jsonvalue.Value{null, null})
	object, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "a", Value: null},
		{Name: "b", Value: null},
	})
	for _, test := range []struct {
		name  string
		value jsonvalue.Value
		calls int
	}{
		{name: "array", value: array, calls: 5},
		{name: "object", value: object, calls: 9},
	} {
		for failure := 1; failure <= test.calls; failure++ {
			writer := &callFailureWriter{failure: failure}
			emitter := jsonEmitter{
				ctx: context.Background(), writer: writer,
				remaining: 100, maxDepth: 10, remainingNodes: 10,
			}
			if err := emitter.value(test.value, 0); !errors.Is(err, errInjectedWrite) {
				t.Fatalf("%s failure %d error = %v", test.name, failure, err)
			}
		}
	}
}

func TestSerializationChildrenFitExactBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		children  int
		depth     int
		remaining int
		want      bool
	}{
		{name: "leaf at depth limit", depth: 3, remaining: 2, want: true},
		{name: "exact remaining nodes", children: 2, depth: 2, remaining: 2, want: true},
		{name: "node overflow", children: 3, depth: 2, remaining: 2},
		{name: "nodes exhausted", children: 1, depth: 2},
		{name: "exact depth", children: 1, depth: 3, remaining: 2},
	} {
		if got := serializationChildrenFit(
			test.children, test.depth, test.remaining, 3,
		); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestEmitterAndYAMLBuilderDefensiveBoundaries(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	emitter := jsonEmitter{ctx: ctx, writer: io.Discard, remaining: 10, maxDepth: 1, remainingNodes: 1}
	if err := emitter.value(jsonvalue.Null(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled emitter error = %v", err)
	}
	emitter = jsonEmitter{ctx: context.Background(), writer: io.Discard, remaining: 10, maxDepth: 1, remainingNodes: 1}
	if err := emitter.value(jsonvalue.Value{}, 0); !errors.Is(err, jsonvalue.ErrInvalidValue) {
		t.Fatalf("invalid emitter value error = %v", err)
	}
	emitter = jsonEmitter{ctx: ctx, writer: io.Discard, remaining: 10}
	if err := emitter.writeString("value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write error = %v", err)
	}
	emitter = jsonEmitter{
		ctx: context.Background(), writer: &callFailureWriter{failure: 1},
		remaining: 2,
	}
	if err := emitter.writeString("value"); !errors.Is(err, errInjectedWrite) {
		t.Fatalf("bounded partial write error = %v", err)
	}

	builder := yamlBuilder{ctx: ctx, options: DefaultOptions(), remainingNodes: 1}
	if _, err := builder.node(jsonvalue.Null(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled builder error = %v", err)
	}
	invalidChild, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "child", Value: jsonvalue.Null()}})
	options := DefaultOptions()
	options.MaxDepth = 0
	builder = yamlBuilder{ctx: context.Background(), options: options, remainingNodes: 10}
	if _, err := builder.node(invalidChild, 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("object child depth error = %v", err)
	}
}

func TestSerializerTraversalPropagatesNestedLimits(t *testing.T) {
	t.Parallel()

	childArray, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	nodeExhaustionArray, _ := jsonvalue.Array([]jsonvalue.Value{
		childArray, jsonvalue.Null(),
	})
	nodeExhaustionObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "first", Value: childArray},
		{Name: "second", Value: jsonvalue.Null()},
	})
	leafObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "leaf", Value: jsonvalue.Null()},
	})
	depthObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "nested", Value: leafObject},
	})
	for _, value := range []jsonvalue.Value{
		nodeExhaustionArray, nodeExhaustionObject,
	} {
		emitter := jsonEmitter{
			ctx: context.Background(), writer: io.Discard,
			remaining: 1_000, maxDepth: 4, remainingNodes: 3,
		}
		if err := emitter.value(value, 0); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("JSON nested node error = %v", err)
		}
		builder := yamlBuilder{
			ctx: context.Background(), options: Options{MaxDepth: 4},
			remainingNodes: 3,
		}
		if _, err := builder.node(value, 0); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("YAML nested node error = %v", err)
		}
	}
	for _, run := range []func() error{
		func() error {
			emitter := jsonEmitter{
				ctx: context.Background(), writer: io.Discard,
				remaining: 1_000, maxDepth: 1, remainingNodes: 10,
			}
			return emitter.value(depthObject, 0)
		},
		func() error {
			builder := yamlBuilder{
				ctx: context.Background(), options: Options{MaxDepth: 1},
				remainingNodes: 10,
			}
			_, err := builder.node(depthObject, 0)
			return err
		},
	} {
		if err := run(); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested object depth error = %v", err)
		}
	}
}

func TestBoundedContextWriterTracksCancellationAndUnderlyingFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &boundedContextWriter{ctx: ctx, writer: io.Discard, remaining: 10}
	if _, err := writer.Write([]byte("value")); !errors.Is(err, context.Canceled) || !errors.Is(writer.failure, context.Canceled) {
		t.Fatalf("canceled writer = %v, %v", err, writer.failure)
	}
	writer = &boundedContextWriter{
		ctx: context.Background(), writer: &callFailureWriter{failure: 1}, remaining: 2,
	}
	if _, err := writer.Write([]byte("value")); !errors.Is(err, errInjectedWrite) || !errors.Is(writer.failure, errInjectedWrite) {
		t.Fatalf("bounded writer = %v, %v", err, writer.failure)
	}
	writer = &boundedContextWriter{
		ctx: context.Background(), writer: &callFailureWriter{failure: 1}, remaining: 10,
	}
	if _, err := writer.Write([]byte("value")); !errors.Is(err, errInjectedWrite) || !errors.Is(writer.failure, errInjectedWrite) {
		t.Fatalf("underlying writer = %v, %v", err, writer.failure)
	}
	var output bytes.Buffer
	writer = &boundedContextWriter{ctx: context.Background(), writer: &output, remaining: 10}
	written, err := writer.Write([]byte("value"))
	if err != nil || written != 5 || output.String() != "value" || writer.remaining != 5 {
		t.Fatalf("successful writer = %d, %v, %q, %d", written, err, output.String(), writer.remaining)
	}
}

var errInjectedWrite = errors.New("injected write failure")

type callFailureWriter struct {
	calls   int
	failure int
}

func (writer *callFailureWriter) Write(value []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failure {
		return 0, errInjectedWrite
	}
	return len(value), nil
}
