package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestExactJSONParserRejectsEveryBoundedFailureClass(t *testing.T) {
	t.Parallel()

	defaults := DefaultLimits()
	var nilContext context.Context
	if _, err := decodeJSON(nilContext, []byte(`null`), defaults); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("nil context: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeJSON(ctx, []byte(`null`), defaults); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: got %v", err)
	}

	for _, test := range []struct {
		name   string
		raw    string
		limits func(*Limits)
	}{
		{
			name: "nesting depth",
			raw:  `[[null]]`,
			limits: func(limits *Limits) {
				limits.MaxNestingDepth = 2
			},
		},
		{
			name: "total values",
			raw:  `[null]`,
			limits: func(limits *Limits) {
				limits.MaxTotalValues = 1
			},
		},
		{
			name: "number bytes",
			raw:  `123`,
			limits: func(limits *Limits) {
				limits.MaxNumberBytes = 2
			},
		},
		{
			name: "object members",
			raw:  `{"a":1,"b":2}`,
			limits: func(limits *Limits) {
				limits.MaxObjectMembers = 1
			},
		},
		{
			name: "array items",
			raw:  `[1,2]`,
			limits: func(limits *Limits) {
				limits.MaxArrayItems = 1
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := defaults
			test.limits(&limits)
			if _, err := decodeJSON(context.Background(), []byte(test.raw), limits); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want ErrLimitExceeded", err)
			}
		})
	}

	for _, raw := range []string{
		`}`, `{"a":`, `{"a":1`, `[1`, `{"a":1,"a":2}`,
	} {
		if _, err := decodeJSON(context.Background(), []byte(raw), defaults); !errors.Is(err, ErrInvalidJSON) {
			t.Errorf("%q: got %v, want ErrInvalidJSON", raw, err)
		}
	}
}

func TestExactJSONParserChecksCancellationDuringDescent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	parser := jsonParser{ctx: ctx, limits: DefaultLimits()}
	if _, err := parser.value(1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestExactJSONParserRejectsUnexpectedDecoderTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		parse func(*jsonParser) error
	}{
		{
			name: "closing delimiter at value",
			parse: func(parser *jsonParser) error {
				parser.decoder = &stubTokenDecoder{tokens: []json.Token{json.Delim(']')}}
				_, err := parser.value(1)
				return err
			},
		},
		{
			name: "unsupported token type",
			parse: func(parser *jsonParser) error {
				parser.decoder = &stubTokenDecoder{tokens: []json.Token{1}}
				_, err := parser.value(1)
				return err
			},
		},
		{
			name: "non-string object key",
			parse: func(parser *jsonParser) error {
				parser.decoder = &stubTokenDecoder{
					tokens: []json.Token{true},
					more:   []bool{true},
				}
				_, err := parser.object(1)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parser := &jsonParser{ctx: context.Background(), limits: DefaultLimits()}
			if err := test.parse(parser); !errors.Is(err, ErrInvalidJSON) {
				t.Fatalf("got %v, want ErrInvalidJSON", err)
			}
		})
	}
}

type stubTokenDecoder struct {
	tokens []json.Token
	more   []bool
	offset int64
}

func (decoder *stubTokenDecoder) Token() (json.Token, error) {
	if len(decoder.tokens) == 0 {
		return nil, io.EOF
	}
	token := decoder.tokens[0]
	decoder.tokens = decoder.tokens[1:]
	decoder.offset++
	return token, nil
}

func (decoder *stubTokenDecoder) More() bool {
	if len(decoder.more) == 0 {
		return false
	}
	more := decoder.more[0]
	decoder.more = decoder.more[1:]
	return more
}

func (decoder *stubTokenDecoder) InputOffset() int64 {
	return decoder.offset
}
