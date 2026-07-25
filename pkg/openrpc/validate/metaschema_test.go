package validate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestPinnedOfficialExamplesAreExplicitlyOutsideCurrentVersionLine(t *testing.T) {
	t.Parallel()

	examples, err := filepath.Glob("../specification/examples/*-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("official example corpus is empty")
	}
	for _, example := range examples {
		data, err := os.ReadFile(example)
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonvalue.Parse(data, jsonvalue.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		report := validate.MetaSchema(context.Background(), value, 100)
		if report.Err() != nil || report.Valid() || len(report.Issues()) == 0 ||
			report.Issues()[0].InstancePointer != "#/openrpc" {
			t.Errorf("%s report = %#v, error = %v", filepath.Base(example), report.Issues(), report.Err())
		}
		_, parseErr := openrpcparse.Decode(data, openrpcparse.DefaultOptions())
		var structuralError *openrpcparse.Error
		if !errors.Is(parseErr, openrpc.ErrUnsupportedVersion) ||
			!errors.As(parseErr, &structuralError) || structuralError.Pointer != "#/openrpc" {
			t.Errorf("%s parse error = %v", filepath.Base(example), parseErr)
		}
	}
}

func TestPinnedMetaSchemaAcceptsCurrentMinimalDocument(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Current","version":"1"},
		"methods":[]
	}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.MetaSchema(context.Background(), value, 100)
	if report.Err() != nil || !report.Valid() {
		t.Fatalf("report = %#v, error = %v", report.Issues(), report.Err())
	}
}

func TestPinnedMetaSchemaAcceptsNormativeServerURLForms(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Servers","version":"1"},
		"servers":[
			{"url":"../rpc"},
			{"url":"https://${region}.example.com","variables":{"region":{"default":"eu"}}}
		],
		"methods":[]
	}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.MetaSchema(context.Background(), value, 100)
	if report.Err() != nil || !report.Valid() {
		t.Fatalf("report = %#v, error = %v", report.Issues(), report.Err())
	}
}

func TestMetaSchemaReportsStructuralFailuresAndBounds(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`{"openrpc":1,"info":{},"methods":"invalid"}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.MetaSchema(context.Background(), value, 1)
	if report.Valid() || len(report.Issues()) != 1 || !report.Truncated() {
		t.Fatalf("report = %#v, truncated = %t", report.Issues(), report.Truncated())
	}
	if report.Issues()[0].InstancePointer == "" {
		t.Fatalf("missing instance pointer: %#v", report.Issues()[0])
	}
	if report := validate.MetaSchema(context.Background(), value, 0); !errors.Is(report.Err(), validate.ErrMetaSchemaPolicy) {
		t.Fatalf("policy error = %v", report.Err())
	}
}

func TestMetaSchemaBytesAreOwned(t *testing.T) {
	t.Parallel()

	first := openrpc.MetaSchema()
	if len(first) == 0 {
		t.Fatal("embedded meta-schema is empty")
	}
	first[0] = '['
	if openrpc.MetaSchema()[0] != '{' {
		t.Fatal("MetaSchema exposed mutable embedded storage")
	}
	companion := openrpc.JSONSchemaToolsMetaSchema()
	companion[0] = '['
	if openrpc.JSONSchemaToolsMetaSchema()[0] != '{' {
		t.Fatal("JSONSchemaToolsMetaSchema exposed mutable embedded storage")
	}
}
