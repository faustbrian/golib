package jsonschema

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompilerAndMetaSchemaBoundariesAreExact(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	compiler.limits.MaxTotalSchemaBytes = len(`true`)
	if _, err := compiler.Compile(context.Background(), []byte(`true`)); err != nil {
		t.Fatalf("schema at byte limit: %v", err)
	}

	state := schemaEvaluationState(context.Background(), &schemaPlan{}, DefaultLimits())
	if len(state.dynamicScope) != 0 {
		t.Fatalf("nil resource added to dynamic scope: %#v", state.dynamicScope)
	}
	resource := &schemaResource{}
	state = schemaEvaluationState(
		context.Background(), &schemaPlan{resource: resource}, DefaultLimits(),
	)
	if len(state.dynamicScope) != 1 || state.dynamicScope[0] != resource {
		t.Fatalf("resource missing from dynamic scope: %#v", state.dynamicScope)
	}
}

func TestStructuredErrorFormattingDistinguishesKnownOffsets(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	withOffset := (&JSONError{Offset: 1, Kind: ErrInvalidJSON, Cause: cause}).Error()
	withoutOffset := (&JSONError{Kind: ErrInvalidJSON, Cause: cause}).Error()
	if withOffset != "invalid JSON at byte 1: cause" {
		t.Fatalf("unexpected offset error %q", withOffset)
	}
	if withoutOffset != "invalid JSON: cause" {
		t.Fatalf("unexpected error %q", withoutOffset)
	}
}

func TestJSONParserLimitsIncludeTheirExactBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		raw    string
		limits func(*Limits)
	}{
		{name: "input bytes", raw: `null`, limits: func(limits *Limits) {
			limits.MaxInputBytes = len(`null`)
		}},
		{name: "nesting depth", raw: `[null]`, limits: func(limits *Limits) {
			limits.MaxNestingDepth = 2
		}},
		{name: "total values", raw: `[null]`, limits: func(limits *Limits) {
			limits.MaxTotalValues = 2
		}},
		{name: "number bytes", raw: `123`, limits: func(limits *Limits) {
			limits.MaxNumberBytes = len(`123`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			test.limits(&limits)
			if _, err := decodeJSON(context.Background(), []byte(test.raw), limits); err != nil {
				t.Fatalf("exact boundary rejected: %v", err)
			}
		})
	}

	limits := DefaultLimits()
	limits.MaxNestingDepth = 1
	if _, err := decodeJSON(
		context.Background(), []byte(`{"value":null}`), limits,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("object child depth: got %v, want limit", err)
	}
	if _, err := decodeJSON(
		context.Background(), []byte(`true false`), DefaultLimits(),
	); err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("unexpected trailing-value error: %v", err)
	}
}

func TestValueEncodingAndBufferLimitsAreExact(t *testing.T) {
	t.Parallel()

	schema := &Schema{plan: &schemaPlan{}, limits: DefaultLimits()}
	schema.limits.MaxInputBytes = len(`"a"`)
	raw, err := schema.encodeValue(context.Background(), "a")
	if err != nil || string(raw) != `"a"` {
		t.Fatalf("got %q, %v", raw, err)
	}

	buffer := &limitedJSONBuffer{limit: 4}
	if count, err := buffer.Write([]byte("ab")); err != nil || count != 2 {
		t.Fatalf("initial write: count=%d err=%v", count, err)
	}
	if count, err := buffer.Write([]byte("cd")); err != nil || count != 2 {
		t.Fatalf("exact write: count=%d err=%v", count, err)
	}
	buffer = &limitedJSONBuffer{limit: 4}
	if _, err := buffer.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("cde")); err == nil {
		t.Fatal("write larger than remaining capacity succeeded")
	}
	buffer = &limitedJSONBuffer{limit: 4}
	if _, err := buffer.Write([]byte("abcde")); err == nil {
		t.Fatal("oversized write succeeded")
	} else {
		var limitError *LimitError
		if !errors.As(err, &limitError) || limitError.Limit != 3 {
			t.Fatalf("unexpected buffer limit error: %#v", err)
		}
	}
}

func TestRegexDeadlineUsesMilliseconds(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRegexMatchMilliseconds = 7
	pattern, err := compilePatternWithLimits("x", limits)
	if err != nil {
		t.Fatal(err)
	}
	if pattern.compiled.MatchTimeout != 7*time.Millisecond {
		t.Fatalf("got %s, want 7ms", pattern.compiled.MatchTimeout)
	}
}
