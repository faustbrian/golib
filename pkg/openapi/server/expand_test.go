package server_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/server"
)

func TestExpandUsesDefaultsAndCallerOverrides(t *testing.T) {
	t.Parallel()

	value := serverValue(t, `{
		"url":"https://{region}.example.test/{version}",
		"variables":{
			"region":{"default":"eu"},
			"version":{"default":"v1","enum":["v1","v2"]}
		}
	}`)
	options := server.DefaultOptions()
	options.Values = map[string]string{"version": "v2"}
	got, err := server.Expand(value, options)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://eu.example.test/v2" {
		t.Fatalf("Expand() = %q", got)
	}
	if options.Values["version"] != "v2" || len(options.Values) != 1 {
		t.Fatalf("Expand mutated caller values: %#v", options.Values)
	}
}

func TestExpandRejectsInvalidServerAndVariableInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   jsonvalue.Value
		options server.Options
		want    error
	}{
		{name: "invalid root", value: jsonvalue.Value{}, options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "non object", value: jsonvalue.Null(), options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "missing URL", value: serverValue(t, `{}`), options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "malformed template", value: serverValue(t, `{"url":"https://{region.example"}`), options: server.DefaultOptions(), want: server.ErrInvalidTemplate},
		{name: "undeclared variable", value: serverValue(t, `{"url":"https://{region}"}`), options: server.DefaultOptions(), want: server.ErrMissingVariable},
		{name: "missing default", value: serverValue(t, `{"url":"https://{region}","variables":{"region":{}}}`), options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "variables non object", value: serverValue(t, `{"url":"https://example.test","variables":[]}`), options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "variable non object", value: serverValue(t, `{"url":"https://{region}","variables":{"region":"eu"}}`), options: server.DefaultOptions(), want: server.ErrInvalidServer},
		{name: "unused override", value: serverValue(t, `{"url":"https://example.test"}`), options: server.Options{Values: map[string]string{"region": "eu"}, MaxOutputBytes: 100, MaxVariables: 10}, want: server.ErrUnusedOverride},
		{name: "too many overrides", value: serverValue(t, `{"url":"https://example.test"}`), options: server.Options{Values: map[string]string{"one": "1", "two": "2"}, MaxOutputBytes: 100, MaxVariables: 1}, want: server.ErrLimitExceeded},
		{name: "invalid override UTF8", value: serverValue(t, `{"url":"https://{region}","variables":{"region":{"default":"eu"}}}`), options: server.Options{Values: map[string]string{"region": string([]byte{0xff})}, MaxOutputBytes: 100, MaxVariables: 10}, want: server.ErrInvalidServer},
		{name: "invalid limits", value: serverValue(t, `{"url":"https://example.test"}`), options: server.Options{MaxOutputBytes: -1}, want: server.ErrInvalidOptions},
		{name: "stray closing brace", value: serverValue(t, `{"url":"https://example.test}"}`), options: server.DefaultOptions(), want: server.ErrInvalidTemplate},
		{name: "missing closing brace", value: serverValue(t, `{"url":"https://{region","variables":{"region":{"default":"eu"}}}`), options: server.DefaultOptions(), want: server.ErrInvalidTemplate},
		{name: "empty variable", value: serverValue(t, `{"url":"https://{}"}`), options: server.DefaultOptions(), want: server.ErrInvalidTemplate},
		{name: "nested variable", value: serverValue(t, `{"url":"https://{{region}","variables":{"region":{"default":"eu"}}}`), options: server.DefaultOptions(), want: server.ErrInvalidTemplate},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := server.Expand(test.value, test.options); !errors.Is(err, test.want) {
				t.Fatalf("Expand() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestExpandZeroOptionsUseDefaults(t *testing.T) {
	t.Parallel()

	got, err := server.Expand(serverValue(t, `{"url":"https://example.test"}`), server.Options{})
	if err != nil || got != "https://example.test" {
		t.Fatalf("Expand() = %q, %v", got, err)
	}
}

func TestExpandEnforcesOutputAndVariableBounds(t *testing.T) {
	t.Parallel()

	value := serverValue(t, `{
		"url":"https://{one}/{two}",
		"variables":{"one":{"default":"first"},"two":{"default":"second"}}
	}`)
	options := server.DefaultOptions()
	options.MaxOutputBytes = 10
	if _, err := server.Expand(value, options); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("output limit error = %v", err)
	}
	options = server.DefaultOptions()
	options.MaxVariables = 1
	if _, err := server.Expand(value, options); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("variable limit error = %v", err)
	}
	repeated := serverValue(t, `{
		"url":"{one}{one}","variables":{"one":{"default":"x"}}
	}`)
	if _, err := server.Expand(repeated, options); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("occurrence limit error = %v", err)
	}
	literalOptions := server.DefaultOptions()
	literalOptions.MaxOutputBytes = 2
	if _, err := server.Expand(serverValue(t, `{"url":"long"}`), literalOptions); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("literal output limit error = %v", err)
	}
	prefix := serverValue(t, `{"url":"long{one}","variables":{"one":{"default":"x"}}}`)
	if _, err := server.Expand(prefix, literalOptions); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("prefix output limit error = %v", err)
	}
	replacement := serverValue(t, `{"url":"{one}","variables":{"one":{"default":"long"}}}`)
	if _, err := server.Expand(replacement, literalOptions); !errors.Is(err, server.ErrLimitExceeded) {
		t.Fatalf("replacement output limit error = %v", err)
	}
}

func TestExpandAcceptsEveryExactResourceBoundary(t *testing.T) {
	t.Parallel()

	value := serverValue(t, `{
		"url":"{one}{two}{one}",
		"variables":{"one":{"default":"a"},"two":{"default":"b"}}
	}`)
	options := server.Options{
		Values:         map[string]string{"one": "x", "two": "y"},
		MaxOutputBytes: 3,
		MaxVariables:   3,
	}
	got, err := server.Expand(value, options)
	if err != nil || got != "xyx" {
		t.Fatalf("exact expansion = %q, %v", got, err)
	}

	openingAtZero := serverValue(t, `{
		"url":"{one}","variables":{"one":{"default":"x"}}
	}`)
	if got, err := server.Expand(openingAtZero, server.Options{
		MaxOutputBytes: 1, MaxVariables: 1,
	}); err != nil || got != "x" {
		t.Fatalf("zero-offset opening = %q, %v", got, err)
	}
	if _, err := server.Expand(
		serverValue(t, `{"url":"}"}`), server.DefaultOptions(),
	); !errors.Is(err, server.ErrInvalidTemplate) {
		t.Fatalf("zero-offset closing error = %v", err)
	}
	overrides := serverValue(t, `{
		"url":"{one}{two}",
		"variables":{"one":{"default":"a"},"two":{"default":"b"}}
	}`)
	if got, err := server.Expand(overrides, server.Options{
		Values:         map[string]string{"one": "x", "two": "y"},
		MaxOutputBytes: 2,
		MaxVariables:   2,
	}); err != nil || got != "xy" {
		t.Fatalf("exact override limit = %q, %v", got, err)
	}
}

func serverValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
