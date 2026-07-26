package parse_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func TestYAMLUsesTheJSONSemanticModel(t *testing.T) {
	t.Parallel()

	value, err := parse.YAML(context.Background(), strings.NewReader("z: -0.0e+00\na: [true, null, x]\n"), parse.DefaultLimits())
	if err != nil {
		t.Fatalf("YAML() error = %v", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(raw), `{"z":-0.0e+00,"a":[true,null,"x"]}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestYAMLRejectsAmbiguousOrNonJSONRepresentations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "duplicate", input: "key: 1\nkey: 2\n", want: parse.ErrDuplicateKey},
		{name: "non string key", input: "1: value\n", want: parse.ErrInvalidYAML},
		{name: "anchor", input: "key: &value 1\n", want: parse.ErrUnsupportedYAMLFeature},
		{name: "alias", input: "key: &value 1\ncopy: *value\n", want: parse.ErrUnsupportedYAMLFeature},
		{name: "merge", input: "base: &base {a: 1}\nvalue: {<<: *base}\n", want: parse.ErrUnsupportedYAMLFeature},
		{name: "custom tag", input: "key: !custom value\n", want: parse.ErrUnsupportedYAMLFeature},
		{name: "multiple documents", input: "---\na: 1\n---\nb: 2\n", want: parse.ErrInvalidYAML},
		{name: "hexadecimal number", input: "key: 0xff\n", want: parse.ErrUnsupportedYAMLFeature},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse.YAML(context.Background(), strings.NewReader(test.input), parse.DefaultLimits())
			if !errors.Is(err, test.want) {
				t.Fatalf("YAML() error = %v, want %v", err, test.want)
			}
		})
	}
}
