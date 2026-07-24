package parse_test

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func TestYAMLTokenLimitCountsMappingKeys(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxTokens = 3
	_, err := parse.YAML(
		context.Background(),
		strings.NewReader("first: 1\nsecond: 2\n"),
		limits,
	)
	if !errors.Is(err, parse.ErrLimitExceeded) {
		t.Fatalf("YAML() error = %v, want token limit", err)
	}
}

func TestYAMLRejectsInvalidLimitsAndValueTokenOverflow(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxDepth = 0
	if _, err := parse.YAML(context.Background(), strings.NewReader("null"), limits); !errors.Is(err, parse.ErrInvalidLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	limits = parse.DefaultLimits()
	limits.MaxTokens = 1
	if _, err := parse.YAML(context.Background(), strings.NewReader("[1]"), limits); !errors.Is(err, parse.ErrLimitExceeded) {
		t.Fatalf("value token limit error = %v", err)
	}
}

func TestParsersPreferCancellationRaisedDuringRead(t *testing.T) {
	t.Parallel()

	for _, parser := range []struct {
		name string
		run  func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		err := parser.run(ctx, cancelingReader{cancel: cancel}, parse.DefaultLimits())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s error = %v", parser.name, err)
		}
	}
}

type cancelingReader struct {
	cancel context.CancelFunc
}

func (reader cancelingReader) Read([]byte) (int, error) {
	reader.cancel()
	return 0, errReaderFailure
}

func TestParsersRejectNilCanceledAndFailingInputs(t *testing.T) {
	t.Parallel()

	parsers := []struct {
		name string
		run  func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	}
	for _, parser := range parsers {
		parser := parser
		t.Run(parser.name, func(t *testing.T) {
			t.Parallel()
			if err := parser.run(nil, strings.NewReader("null"), parse.DefaultLimits()); err == nil {
				t.Fatal("nil context was accepted")
			}
			if err := parser.run(context.Background(), nil, parse.DefaultLimits()); err == nil {
				t.Fatal("nil reader was accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := parser.run(ctx, strings.NewReader("null"), parse.DefaultLimits()); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			if err := parser.run(context.Background(), errorReader{}, parse.DefaultLimits()); !errors.Is(err, errReaderFailure) {
				t.Fatalf("reader error = %v", err)
			}
		})
	}
}

func TestEveryParseLimitMustBePositive(t *testing.T) {
	t.Parallel()

	mutations := []func(*parse.Limits){
		func(value *parse.Limits) { value.MaxBytes = 0 },
		func(value *parse.Limits) { value.MaxTokens = 0 },
		func(value *parse.Limits) { value.MaxDepth = 0 },
		func(value *parse.Limits) { value.MaxObjectMembers = 0 },
		func(value *parse.Limits) { value.MaxArrayItems = 0 },
		func(value *parse.Limits) { value.MaxScalarBytes = 0 },
		func(value *parse.Limits) { value.MaxTotalValues = 0 },
	}
	for _, mutate := range mutations {
		limits := parse.DefaultLimits()
		mutate(&limits)
		if _, err := parse.JSON(context.Background(), strings.NewReader("null"), limits); !errors.Is(err, parse.ErrInvalidLimits) {
			t.Fatalf("JSON limits error = %v", err)
		}
	}
}

func TestParsersRejectOverflowingByteLimit(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxBytes = math.MaxInt64
	for _, parser := range []struct {
		name string
		run  func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	} {
		if err := parser.run(
			context.Background(), strings.NewReader("null"), limits,
		); !errors.Is(err, parse.ErrInvalidLimits) {
			t.Fatalf("%s maximum byte limit error = %v", parser.name, err)
		}
	}
}

func TestYAMLEnforcesIndependentLimitsAndStrictScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		change func(*parse.Limits)
		want   error
	}{
		{name: "bytes", input: "key: value\n", change: func(value *parse.Limits) { value.MaxBytes = 4 }, want: parse.ErrLimitExceeded},
		{name: "depth", input: "- - value\n", change: func(value *parse.Limits) { value.MaxDepth = 2 }, want: parse.ErrLimitExceeded},
		{name: "object members", input: "a: 1\nb: 2\n", change: func(value *parse.Limits) { value.MaxObjectMembers = 1 }, want: parse.ErrLimitExceeded},
		{name: "array items", input: "[1, 2]", change: func(value *parse.Limits) { value.MaxArrayItems = 1 }, want: parse.ErrLimitExceeded},
		{name: "scalar value", input: "key: long", change: func(value *parse.Limits) { value.MaxScalarBytes = 3 }, want: parse.ErrLimitExceeded},
		{name: "scalar key", input: "long: x", change: func(value *parse.Limits) { value.MaxScalarBytes = 3 }, want: parse.ErrLimitExceeded},
		{name: "total values", input: "[1]", change: func(value *parse.Limits) { value.MaxTotalValues = 1 }, want: parse.ErrLimitExceeded},
		{name: "uppercase boolean", input: "TRUE", want: parse.ErrUnsupportedYAMLFeature},
		{name: "underscored number", input: "1_000", want: parse.ErrUnsupportedYAMLFeature},
		{name: "nan", input: ".nan", want: parse.ErrUnsupportedYAMLFeature},
		{name: "positive infinity", input: "+.inf", want: parse.ErrUnsupportedYAMLFeature},
		{name: "negative infinity", input: "-.inf", want: parse.ErrUnsupportedYAMLFeature},
		{name: "non JSON decimal", input: ".1", want: parse.ErrUnsupportedYAMLFeature},
		{name: "false", input: "false", want: nil},
		{name: "direct merge key", input: "'<<': value", want: parse.ErrUnsupportedYAMLFeature},
		{name: "anchored key", input: "&key name: value", want: parse.ErrUnsupportedYAMLFeature},
		{name: "malformed number", input: "1:2:3", want: nil},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := parse.DefaultLimits()
			if test.change != nil {
				test.change(&limits)
			}
			_, err := parse.YAML(context.Background(), strings.NewReader(test.input), limits)
			if test.want == nil {
				if err != nil {
					t.Fatalf("YAML() error = %v", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("YAML() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestJSONCoversScalarAndContainerFailureBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		change func(*parse.Limits)
		want   error
	}{
		{name: "number scalar bytes", input: "123", change: func(value *parse.Limits) { value.MaxScalarBytes = 2 }, want: parse.ErrLimitExceeded},
		{name: "key scalar bytes", input: `{"long": 1}`, change: func(value *parse.Limits) { value.MaxScalarBytes = 3 }, want: parse.ErrLimitExceeded},
		{name: "unterminated object key", input: `{"`, want: parse.ErrInvalidJSON},
		{name: "missing object value", input: `{"key"`, want: parse.ErrInvalidJSON},
		{name: "missing object close", input: `{"key": 1`, want: parse.ErrInvalidJSON},
		{name: "missing array close", input: `[1`, want: parse.ErrInvalidJSON},
	}
	for _, test := range tests {
		limits := parse.DefaultLimits()
		if test.change != nil {
			test.change(&limits)
		}
		if _, err := parse.JSON(context.Background(), strings.NewReader(test.input), limits); !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}
}

func TestYAMLRejectsMalformedEmptyAndInvalidUTF8Streams(t *testing.T) {
	t.Parallel()

	inputs := [][]byte{
		{},
		[]byte("key: ["),
		{0xff},
	}
	for _, input := range inputs {
		if _, err := parse.YAML(context.Background(), strings.NewReader(string(input)), parse.DefaultLimits()); !errors.Is(err, parse.ErrInvalidYAML) {
			t.Fatalf("YAML(%q) error = %v", input, err)
		}
	}
}

func TestParseErrorProvidesBoundedClassification(t *testing.T) {
	t.Parallel()

	var nilError *parse.Error
	if nilError.Error() != "parse: <nil>" || nilError.Unwrap() != nil {
		t.Fatalf("nil error behavior = %q, %#v", nilError.Error(), nilError.Unwrap())
	}
	cause := errors.New("cause")
	value := &parse.Error{Code: "code", Offset: 12, Kind: parse.ErrInvalidJSON, Cause: cause}
	if value.Error() != "parse: code at byte 12" || !errors.Is(value, parse.ErrInvalidJSON) || !errors.Is(value, cause) {
		t.Fatalf("error behavior = %q, %#v", value.Error(), value.Unwrap())
	}
}

func TestParsersAcceptEveryExactResourceBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		change func(*parse.Limits)
	}{
		{name: "bytes", input: `"x"`, change: func(limits *parse.Limits) {
			limits.MaxBytes = 3
		}},
		{name: "depth", input: `[0]`, change: func(limits *parse.Limits) {
			limits.MaxDepth = 2
		}},
		{name: "mapping depth", input: `{"x":0}`, change: func(limits *parse.Limits) {
			limits.MaxDepth = 2
		}},
		{name: "tokens", input: `"x"`, change: func(limits *parse.Limits) {
			limits.MaxTokens = 1
		}},
		{name: "values", input: `[0]`, change: func(limits *parse.Limits) {
			limits.MaxTotalValues = 2
		}},
		{name: "array items", input: `[0]`, change: func(limits *parse.Limits) {
			limits.MaxArrayItems = 1
		}},
		{name: "object members", input: `{"x":0}`, change: func(limits *parse.Limits) {
			limits.MaxObjectMembers = 1
		}},
		{name: "string bytes", input: `"x"`, change: func(limits *parse.Limits) {
			limits.MaxScalarBytes = 1
		}},
		{name: "number bytes", input: `1`, change: func(limits *parse.Limits) {
			limits.MaxScalarBytes = 1
		}},
		{name: "key bytes", input: `{"x":0}`, change: func(limits *parse.Limits) {
			limits.MaxScalarBytes = 1
		}},
	}
	parsers := []struct {
		name string
		run  func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	}
	for _, parser := range parsers {
		for _, test := range tests {
			limits := parse.DefaultLimits()
			test.change(&limits)
			if parser.name == "JSON" {
				switch test.name {
				case "bytes":
					limits.MaxBytes = int64(len(test.input))
				case "depth":
					limits.MaxTokens = 3
				}
			} else if test.name == "bytes" {
				limits.MaxBytes = int64(len(test.input))
			}
			if err := parser.run(
				context.Background(), strings.NewReader(test.input), limits,
			); err != nil {
				t.Fatalf("%s %s exact boundary error = %v", parser.name, test.name, err)
			}
		}
	}
}

func TestParsersClassifyASecondCompleteDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		run   func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", input: "null null", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", input: "---\nnull\n---\nnull\n", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	}
	for _, test := range tests {
		err := test.run(context.Background(), strings.NewReader(test.input), parse.DefaultLimits())
		var parseError *parse.Error
		if !errors.As(err, &parseError) || parseError.Cause == nil {
			t.Fatalf("%s trailing document error = %#v", test.name, err)
		}
	}
}

func TestYAMLMalformedCauseRetainsItsLocation(t *testing.T) {
	t.Parallel()

	_, err := parse.YAML(
		context.Background(), strings.NewReader("key: ["), parse.DefaultLimits(),
	)
	var parseError *parse.Error
	if !errors.As(err, &parseError) || parseError.Cause == nil ||
		!strings.HasPrefix(parseError.Cause.Error(), "line 0 column 0: ") {
		t.Fatalf("malformed YAML cause = %#v", err)
	}
}

func TestParsersRejectNestedMappingsBeyondExactDepth(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxDepth = 2
	for _, parser := range []struct {
		name string
		run  func(context.Context, io.Reader, parse.Limits) error
	}{
		{name: "JSON", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.JSON(ctx, reader, limits)
			return err
		}},
		{name: "YAML", run: func(ctx context.Context, reader io.Reader, limits parse.Limits) error {
			_, err := parse.YAML(ctx, reader, limits)
			return err
		}},
	} {
		err := parser.run(
			context.Background(), strings.NewReader(`{"x":{"y":0}}`), limits,
		)
		if !errors.Is(err, parse.ErrLimitExceeded) {
			t.Fatalf("%s nested mapping error = %v", parser.name, err)
		}
	}
}

func TestYAMLAcceptsExactMappingTokenLimit(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxTokens = 3
	if _, err := parse.YAML(
		context.Background(), strings.NewReader(`{"x":0}`), limits,
	); err != nil {
		t.Fatalf("YAML exact mapping token limit error = %v", err)
	}
}

func TestYAMLTokenExhaustionPointsToTheFirstUnacceptedValue(t *testing.T) {
	t.Parallel()

	limits := parse.DefaultLimits()
	limits.MaxTokens = 2
	_, err := parse.YAML(
		context.Background(), strings.NewReader("x:\n  - 0\n"), limits,
	)
	var parseError *parse.Error
	if !errors.As(err, &parseError) || parseError.Offset != 2 {
		t.Fatalf("YAML token exhaustion error = %#v", err)
	}
}
