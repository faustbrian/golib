package serialize_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
)

func FuzzJSONAndYAMLSemanticRoundTrip(f *testing.F) {
	for _, seed := range []string{
		`null`,
		`-0.0e+2`,
		`{"z":[true,null],"a":"value"}`,
		`{"escaped":"~1/%20","unicode":"\ud83d\ude00"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		limits := serializeFuzzParseLimits()
		value, err := parse.JSON(context.Background(), bytes.NewReader(raw), limits)
		if err != nil {
			return
		}
		options := serialize.Options{
			Mode: serialize.Canonical, MaxBytes: 256 << 10,
			MaxDepth: 64, MaxNodes: 4_096,
		}
		want := canonicalJSON(t, value, options)
		for _, format := range []struct {
			name  string
			write func(context.Context, io.Writer, serialize.Source, serialize.Options) error
			parse func(context.Context, io.Reader, parse.Limits) (jsonvalue.Value, error)
		}{
			{name: "JSON", write: serialize.JSON, parse: parse.JSON},
			{name: "YAML", write: serialize.YAML, parse: parse.YAML},
		} {
			var output bytes.Buffer
			if err := format.write(context.Background(), &output, value, options); err != nil {
				return
			}
			reparsed, err := format.parse(context.Background(), bytes.NewReader(output.Bytes()), limits)
			if err != nil {
				t.Fatalf("%s output failed to parse: %v", format.name, err)
			}
			if got := canonicalJSON(t, reparsed, options); !bytes.Equal(got, want) {
				t.Fatalf("%s semantic round trip changed value\n%s\n%s", format.name, want, got)
			}
		}
	})
}

func canonicalJSON(t *testing.T, value jsonvalue.Value, options serialize.Options) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := serialize.JSON(context.Background(), &output, value, options); err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(output.Bytes())
}

func serializeFuzzParseLimits() parse.Limits {
	limits := parse.DefaultLimits()
	limits.MaxBytes = 32 << 10
	limits.MaxTokens = 4_096
	limits.MaxDepth = 64
	limits.MaxObjectMembers = 1_024
	limits.MaxArrayItems = 1_024
	limits.MaxScalarBytes = 8 << 10
	limits.MaxTotalValues = 2_048
	return limits
}
