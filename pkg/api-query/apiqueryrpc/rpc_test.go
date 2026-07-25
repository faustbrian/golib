package apiqueryrpc

import (
	"errors"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestParseAndRequestPreserveCompleteParams(t *testing.T) {
	t.Parallel()

	params, err := Parse([]byte(`{
		"schema_revision":"v1","fields":[],"includes":["customer"],
		"filter":{"logic":"not","children":[{"predicate":{"name":"status","operator":"eq","values":[{"type":"string","value":"paid"}]}}]},
		"sorts":[{"name":"id","direction":"asc"}],
		"page":{"mode":"cursor","size":10,"after":"opaque"}
	}`), 1000)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	request := params.Request()
	if revision, present := request.SchemaRevision.Value(); !present || revision != "v1" {
		t.Fatalf("revision = %q, present = %v", revision, present)
	}
	if fields, present := request.Fields.Value(); !present || len(fields) != 0 {
		t.Fatalf("fields = %v", fields)
	}
	if includes, present := request.Includes.Value(); !present || len(includes) != 1 {
		t.Fatalf("includes = %v", includes)
	}
	if sorts, present := request.Sorts.Value(); !present || len(sorts) != 1 {
		t.Fatalf("sorts = %#v", sorts)
	}
	if request.Page.After != "opaque" || request.Filter.Children[0].Predicate.Values[0].String() != "paid" {
		t.Fatalf("request = %#v", request)
	}
	params.Filter.Children[0].Predicate.Values[0] = apiquery.StringValue("changed")
	if request.Filter.Children[0].Predicate.Values[0].String() != "paid" {
		t.Fatal("Request() returned Params-owned filter storage")
	}
}

func TestParseRejectsStrictJSONFailures(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{nil, []byte(`[]`), []byte(`{"unknown":1}`),
		[]byte(`{"fields":[],"fields":[]}`)} {
		if _, err := Parse(data, 100); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Parse(%q) error = %v", data, err)
		}
	}
}

func TestOpenRPCDescriptorIsIndependent(t *testing.T) {
	t.Parallel()

	first := OpenRPCContentDescriptor()
	second := OpenRPCContentDescriptor()
	if first.Name != "query" || first.Required || first.Schema["type"] != "object" {
		t.Fatalf("descriptor = %#v", first)
	}
	first.Schema["type"] = "changed"
	if second.Schema["type"] != "object" {
		t.Fatal("descriptors shared map storage")
	}
	if request := (Params{}).Request(); request.Filter != nil {
		t.Fatalf("empty request = %#v", request)
	}
}
