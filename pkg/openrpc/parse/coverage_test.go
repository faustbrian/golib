package parse_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestDecodeRejectsMalformedReferencesInEveryUnion(t *testing.T) {
	t.Parallel()

	badReference := `{"$ref":"#/value","extra":true}`
	tests := []string{
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[` + badReference + `]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[` + badReference + `]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"tags":[` + badReference + `]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"errors":[` + badReference + `]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"links":[` + badReference + `]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"examples":[` + badReference + `]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"examples":[{"name":"p","params":[` + badReference + `]}]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{"contentDescriptors":{"bad":` + badReference + `}}}`,
	}
	for _, input := range tests {
		if _, err := parse.Decode([]byte(input), parse.DefaultOptions()); !errors.Is(err, parse.ErrInvalidObject) {
			t.Errorf("Decode(%s) error = %v", input, err)
		}
	}
}

func TestDecodeRejectsMalformedUnionEntriesAndComponentValues(t *testing.T) {
	t.Parallel()

	tests := []string{
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"tags":[false]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"errors":[false]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"links":[false]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"m","params":[],"examples":[false]}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"servers":[{"url":"https://example.com","variables":{"bad":false}}]}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{"tags":{"bad":false}}}`,
		`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{"contentDescriptors":{"bad":{"$ref":"#/value"}}}}`,
	}
	for _, input := range tests {
		if _, err := parse.Decode([]byte(input), parse.DefaultOptions()); err == nil {
			t.Errorf("Decode(%s) succeeded", input)
		}
	}
}

func TestDecodeExercisesEveryCollectionLimit(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile("testdata/complete-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		path   []any
		field  string
		limit  func(*parse.Options)
		second bool
	}{
		{name: "servers", path: []any{}, field: "servers", limit: func(options *parse.Options) { options.MaxServers = 1 }},
		{name: "method params", path: []any{"methods", 0}, field: "params", limit: func(options *parse.Options) { options.MaxParameters = 1 }},
		{name: "pairing params", path: []any{"methods", 0, "examples", 0}, field: "params", limit: func(options *parse.Options) { options.MaxParameters = 1 }},
		{name: "tags", path: []any{"methods", 0}, field: "tags", limit: func(options *parse.Options) { options.MaxTags = 1 }},
		{name: "errors", path: []any{"methods", 0}, field: "errors", limit: func(options *parse.Options) { options.MaxErrors = 1 }},
		{name: "links", path: []any{"methods", 0}, field: "links", limit: func(options *parse.Options) { options.MaxLinks = 1 }},
		{name: "examples", path: []any{"methods", 0}, field: "examples", limit: func(options *parse.Options) { options.MaxExamples = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := decodeJSON(t, input)
			object := objectAt(t, document, test.path)
			values := object[test.field].([]any)
			object[test.field] = append(values, values[0])
			encoded, marshalErr := json.Marshal(document)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			options := parse.DefaultOptions()
			test.limit(&options)
			if _, err := parse.Decode(encoded, options); err == nil {
				t.Fatal("collection over limit succeeded")
			}
		})
	}
}

func TestDecodeExercisesComponentMapAndTotalLimits(t *testing.T) {
	t.Parallel()

	for _, components := range []string{
		`{"schemas":{"one":true,"two":false}}`,
		`{"tags":{"one":{"name":"one"},"two":{"name":"two"}}}`,
		`{"schemas":{"one":true},"tags":{"two":{"name":"two"}}}`,
	} {
		input := []byte(`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":` + components + `}`)
		options := parse.DefaultOptions()
		options.MaxComponents = 1
		if _, err := parse.Decode(input, options); err == nil {
			t.Fatalf("components %s over limit succeeded", components)
		}
	}
}

func TestDecodeAcceptsExactPerRegistryComponentLimits(t *testing.T) {
	t.Parallel()

	for _, components := range []string{
		`{"schemas":{"one":true}}`,
		`{"tags":{"one":{"name":"one"}}}`,
	} {
		input := []byte(`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":` + components + `}`)
		options := parse.DefaultOptions()
		options.MaxComponents = 1
		if _, err := parse.Decode(input, options); err != nil {
			t.Fatalf("components %s at exact limit: %v", components, err)
		}
	}
}

func TestDecodeRejectsEachInvalidOptionIndependently(t *testing.T) {
	t.Parallel()

	input := []byte(`{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[]}`)
	mutators := []func(*parse.Options){
		func(options *parse.Options) { options.MaxMethods = 0 },
		func(options *parse.Options) { options.MaxComponents = 0 },
		func(options *parse.Options) { options.MaxParameters = 0 },
		func(options *parse.Options) { options.MaxServers = 0 },
		func(options *parse.Options) { options.MaxServerVariables = 0 },
		func(options *parse.Options) { options.MaxTags = 0 },
		func(options *parse.Options) { options.MaxErrors = 0 },
		func(options *parse.Options) { options.MaxLinks = 0 },
		func(options *parse.Options) { options.MaxExamples = 0 },
		func(options *parse.Options) { options.UnknownFields = parse.UnknownFieldMode(255) },
	}
	for _, mutate := range mutators {
		options := parse.DefaultOptions()
		mutate(&options)
		if _, err := parse.Decode(input, options); !errors.Is(err, parse.ErrInvalidOptions) {
			t.Errorf("invalid options error = %v", err)
		}
	}
}

func TestDecodeAcceptsEveryExactCollectionLimit(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile("testdata/complete-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	options := parse.DefaultOptions()
	options.MaxMethods = 1
	options.MaxComponents = 7
	options.MaxParameters = 1
	options.MaxServers = 1
	options.MaxServerVariables = 1
	options.MaxTags = 1
	options.MaxErrors = 1
	options.MaxLinks = 1
	options.MaxExamples = 1
	if _, err := parse.Decode(input, options); err != nil {
		t.Fatalf("exact collection limits error = %v", err)
	}
	options.MaxComponents = 6
	if _, err := parse.Decode(input, options); !errors.Is(err, parse.ErrComponentLimit) {
		t.Fatalf("combined component limit error = %v", err)
	}
}
