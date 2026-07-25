package serialize_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
)

func TestYAMLRoundTripPreservesJSONSemantics(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"x-number":-0.0e+2,
		"x-values":[null,true,"yes"],
		"info":{"version":"1","title":"API"},
		"paths":{}
	}`)
	var output bytes.Buffer
	if err := serialize.YAML(
		context.Background(), &output, document, serialize.DefaultOptions(),
	); err != nil {
		t.Fatal(err)
	}
	parsed, err := parse.YAML(
		context.Background(),
		strings.NewReader(output.String()),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("parse emitted YAML: %v\n%s", err, output.String())
	}
	if !reflect.DeepEqual(parsed, document.Raw()) {
		got, _ := parsed.MarshalJSON()
		want, _ := document.Raw().MarshalJSON()
		t.Fatalf("semantic drift\ngot:  %s\nwant: %s", got, want)
	}
}

func TestYAMLCanonicalizesMappingOrder(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"paths":{},
		"info":{"version":"1","title":"API"},
		"openapi":"3.2.0"
	}`)
	options := serialize.DefaultOptions()
	options.Mode = serialize.Canonical
	var output bytes.Buffer
	if err := serialize.YAML(context.Background(), &output, document, options); err != nil {
		t.Fatal(err)
	}
	if output.String() != "info:\n  title: API\n  version: \"1\"\nopenapi: 3.2.0\npaths: {}\n" {
		t.Fatalf("canonical YAML =\n%s", output.String())
	}
}

func TestYAMLEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	options := serialize.DefaultOptions()
	options.MaxBytes = 12
	var output bytes.Buffer
	if err := serialize.YAML(
		context.Background(), &output, document, options,
	); !errors.Is(err, serialize.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
	if output.Len() > options.MaxBytes {
		t.Fatalf("wrote %d bytes past limit %d", output.Len(), options.MaxBytes)
	}
}
