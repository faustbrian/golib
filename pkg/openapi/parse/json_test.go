package parse_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
)

var errReaderFailure = errors.New("reader failure")

func TestJSONPreservesSemanticOrderAndExactNumbers(t *testing.T) {
	t.Parallel()

	value, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{"z":-0.0e+00,"a":[true,null,"x"]}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(raw), `{"z":-0.0e+00,"a":[true,null,"x"]}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestJSONRejectsDuplicateObjectMembers(t *testing.T) {
	t.Parallel()

	_, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{"outer":{"duplicate":1,"duplicate":2}}`),
		parse.DefaultLimits(),
	)
	if !errors.Is(err, parse.ErrDuplicateKey) {
		t.Fatalf("JSON() error = %v, want ErrDuplicateKey", err)
	}
}

func TestJSONEnforcesIndependentLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		change func(*parse.Limits)
		want   error
	}{
		{name: "bytes", input: "[0]", change: func(limits *parse.Limits) { limits.MaxBytes = 2 }, want: parse.ErrLimitExceeded},
		{name: "tokens", input: "[0]", change: func(limits *parse.Limits) { limits.MaxTokens = 2 }, want: parse.ErrLimitExceeded},
		{name: "depth", input: "[0]", change: func(limits *parse.Limits) { limits.MaxDepth = 1 }, want: parse.ErrLimitExceeded},
		{name: "object members", input: `{"a":0,"b":1}`, change: func(limits *parse.Limits) { limits.MaxObjectMembers = 1 }, want: parse.ErrLimitExceeded},
		{name: "array items", input: "[0,1]", change: func(limits *parse.Limits) { limits.MaxArrayItems = 1 }, want: parse.ErrLimitExceeded},
		{name: "scalar bytes", input: `"xx"`, change: func(limits *parse.Limits) { limits.MaxScalarBytes = 1 }, want: parse.ErrLimitExceeded},
		{name: "total values", input: "[0]", change: func(limits *parse.Limits) { limits.MaxTotalValues = 1 }, want: parse.ErrLimitExceeded},
		{name: "invalid limits", input: "null", change: func(limits *parse.Limits) { limits.MaxDepth = 0 }, want: parse.ErrInvalidLimits},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			limits := parse.DefaultLimits()
			test.change(&limits)
			_, err := parse.JSON(context.Background(), strings.NewReader(test.input), limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("JSON() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestJSONRejectsMalformedRepresentations(t *testing.T) {
	t.Parallel()

	inputs := [][]byte{
		{},
		{0xef, 0xbb, 0xbf, '{', '}'},
		{0xff},
		[]byte("{}{}"),
		[]byte("["),
		[]byte(`"\u1"`),
		[]byte(`"\uZZZZ"`),
	}
	for _, input := range inputs {
		_, err := parse.JSON(context.Background(), strings.NewReader(string(input)), parse.DefaultLimits())
		if !errors.Is(err, parse.ErrInvalidJSON) {
			t.Errorf("JSON(%q) error = %v, want ErrInvalidJSON", input, err)
		}
	}
}

func TestJSONRejectsUnpairedUnicodeSurrogateEscapes(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`"\ud800"`,
		`"\udfff"`,
		`"\ud800x"`,
		`"\ud800\u0041"`,
		`{"\udfff":true}`,
	} {
		if _, err := parse.JSON(
			context.Background(), strings.NewReader(input), parse.DefaultLimits(),
		); !errors.Is(err, parse.ErrInvalidJSON) {
			t.Errorf("JSON(%s) error = %v, want invalid JSON", input, err)
		}
	}

	for input, want := range map[string]string{
		`"\ud83d\ude00"`: "😀",
		`"\uD83D\uDE00"`: "😀",
		`"\u0061"`:       "a",
		`"\\ud800"`:      `\ud800`,
		`"\"\u0061"`:     `"a`,
	} {
		value, err := parse.JSON(
			context.Background(), strings.NewReader(input), parse.DefaultLimits(),
		)
		if err != nil {
			t.Errorf("JSON(%s) error = %v", input, err)
			continue
		}
		if text, ok := value.Text(); !ok || text != want {
			t.Errorf("JSON(%s) = %#v, want %q", input, value, want)
		}
	}
}

func TestJSONPropagatesCancellationAndReaderFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parse.JSON(ctx, strings.NewReader("null"), parse.DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("JSON() canceled error = %v", err)
	}

	reader := io.MultiReader(strings.NewReader("{"), errorReader{})
	if _, err := parse.JSON(context.Background(), reader, parse.DefaultLimits()); !errors.Is(err, errReaderFailure) {
		t.Fatalf("JSON() reader error = %v, want reader cause", err)
	}
}

func TestErrorUnwrapOmitsNilCauses(t *testing.T) {
	t.Parallel()

	parseError := &parse.Error{Kind: parse.ErrInvalidJSON}
	causes := parseError.Unwrap()
	if got, want := len(causes), 1; got != want {
		t.Fatalf("len(Unwrap()) = %d, want %d", got, want)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errReaderFailure
}
