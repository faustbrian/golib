package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

type stagedCancelContext struct {
	done   chan struct{}
	calls  int
	closed bool
}

func newStagedCancelContext() *stagedCancelContext {
	return &stagedCancelContext{done: make(chan struct{})}
}

func (*stagedCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (ctx *stagedCancelContext) Done() <-chan struct{} {
	ctx.calls++
	if ctx.calls == 3 {
		close(ctx.done)
		ctx.closed = true
	}
	return ctx.done
}

func (ctx *stagedCancelContext) Err() error {
	if ctx.closed {
		return context.Canceled
	}
	return nil
}

func (*stagedCancelContext) Value(any) any { return nil }

type countingErrContext struct {
	done     chan struct{}
	errCalls int
	cancelAt int
}

func newCountingErrContext(cancelAt int) *countingErrContext {
	return &countingErrContext{done: make(chan struct{}), cancelAt: cancelAt}
}

func (*countingErrContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (ctx *countingErrContext) Done() <-chan struct{} { return ctx.done }

func (ctx *countingErrContext) Err() error {
	ctx.errCalls++
	if ctx.errCalls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func (*countingErrContext) Value(any) any { return nil }

func TestYAMLDecoderObservesCancellationWhileBuildingSyntaxTree(t *testing.T) {
	t.Parallel()

	ctx := newStagedCancelContext()
	if _, err := YAML(
		ctx,
		strings.NewReader("value: true\n"),
		DefaultLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("YAML decode cancellation error = %v", err)
	}
}

func TestYAMLChecksCancellationAtEveryDecodeBoundary(t *testing.T) {
	t.Parallel()

	for _, cancelAt := range []int{2, 3, 4} {
		ctx := newCountingErrContext(cancelAt)
		if _, err := YAML(
			ctx,
			strings.NewReader("value: true\n"),
			DefaultLimits(),
		); !errors.Is(err, context.Canceled) {
			t.Errorf("boundary %d cancellation error = %v", cancelAt, err)
		}
	}
}

func TestContextReaderAndJSONParserObserveMidOperationCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := contextReader{ctx: ctx, reader: strings.NewReader("null")}
	if _, err := reader.Read(make([]byte, 4)); !errors.Is(err, context.Canceled) {
		t.Fatalf("context reader error = %v", err)
	}
	decoder := json.NewDecoder(bytes.NewBufferString("null"))
	parser := jsonParser{ctx: ctx, decoder: decoder, limits: DefaultLimits()}
	if _, err := parser.value(1); !errors.Is(err, context.Canceled) {
		t.Fatalf("value cancellation error = %v", err)
	}
	if _, err := parser.token(); !errors.Is(err, context.Canceled) {
		t.Fatalf("token cancellation error = %v", err)
	}
}

func TestJSONParserRejectsUnexpectedClosingDelimiter(t *testing.T) {
	t.Parallel()

	decoder := json.NewDecoder(bytes.NewBufferString("]"))
	parser := jsonParser{
		ctx: context.Background(), decoder: decoder, limits: DefaultLimits(),
	}
	if _, err := parser.value(1); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("unexpected delimiter error = %v", err)
	}
}

func TestUnpairedSurrogateScannerCoversEscapeBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw     string
		offset  int
		invalid bool
	}{
		{raw: `{}`},
		{raw: `""`},
		{raw: `"\`},
		{raw: `"\n"`},
		{raw: `"\\\ud800"`, offset: 3, invalid: true},
		{raw: `"\\\\ud800"`},
		{raw: `"\u0061"`},
		{raw: `"\ud7ff"`},
		{raw: `"\ud800"`, offset: 1, invalid: true},
		{raw: `"\ud800x"`, offset: 1, invalid: true},
		{raw: `"\ud800/udc00"`, offset: 1, invalid: true},
		{raw: `"\ud800\x0000"`, offset: 1, invalid: true},
		{raw: `"\ud800\u0000"`, offset: 1, invalid: true},
		{raw: `"\ud800\udbff"`, offset: 1, invalid: true},
		{raw: `"\ud800\udc00"`},
		{raw: `"\ud800\udc00`},
		{raw: `"prefix\ud800`, offset: 7, invalid: true},
		{raw: `"\udbff\udfff"`},
		{raw: `"\udbff\ue000"`, offset: 1, invalid: true},
		{raw: `"\udc00"`, offset: 1, invalid: true},
		{raw: `"\udfff"`, offset: 1, invalid: true},
		{raw: `"\ue000"`},
	} {
		offset, invalid := unpairedSurrogateEscape([]byte(test.raw))
		if offset != test.offset || invalid != test.invalid {
			t.Errorf("unpairedSurrogateEscape(%q) = %d, %t", test.raw, offset, invalid)
		}
	}
}

func TestJSONHexQuadCoversDigitBoundaries(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]uint16{
		"0000": 0x0000,
		"9999": 0x9999,
		"aaaa": 0xaaaa,
		"ffff": 0xffff,
		"AAAA": 0xaaaa,
		"FFFF": 0xffff,
	} {
		actual, valid := jsonHexQuad([]byte(raw))
		if !valid || actual != want {
			t.Errorf("jsonHexQuad(%q) = %#x, %t", raw, actual, valid)
		}
	}
	for _, raw := range []string{"", "000", "/000", ":000", "`000", "g000", "@000", "G000"} {
		if _, valid := jsonHexQuad([]byte(raw)); valid {
			t.Errorf("jsonHexQuad(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestYAMLParserRejectsDefensiveNodeBoundaries(t *testing.T) {
	t.Parallel()

	parser := yamlParser{ctx: context.Background(), limits: DefaultLimits()}
	if _, err := parser.value(nil, 1); !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("nil node error = %v", err)
	}
	if _, err := parser.value(&yaml.Node{Kind: yaml.DocumentNode}, 1); !errors.Is(err, ErrUnsupportedYAMLFeature) {
		t.Fatalf("document node error = %v", err)
	}
	if _, err := parser.scalar(&yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!str", Value: string([]byte{0xff}),
	}); !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("invalid string error = %v", err)
	}
	if _, err := parser.mapping(&yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "key"}},
	}, 1); !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("odd mapping error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	parser = yamlParser{ctx: ctx, limits: DefaultLimits()}
	if _, err := parser.value(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("value cancellation error = %v", err)
	}
	if _, err := parser.mapping(&yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "key"},
			{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"},
		},
	}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("mapping cancellation error = %v", err)
	}
}
