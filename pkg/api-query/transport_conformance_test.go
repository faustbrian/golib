package apiquery_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiqueryhttp"
	"github.com/faustbrian/golib/pkg/api-query/apiqueryjsonapi"
	"github.com/faustbrian/golib/pkg/api-query/apiqueryrpc"
	"github.com/faustbrian/golib/pkg/api-query/cursor"
	jsonapi "github.com/faustbrian/golib/pkg/jsonapi"
)

func TestHTTPRPCJSONAPIAndOpenRPCProduceEquivalentContracts(t *testing.T) {
	t.Parallel()

	schema := transportSchema(t)
	rpcParams, err := apiqueryrpc.Parse([]byte(`{
		"schema_revision":"v1",
		"fields":["status"],
		"filter":{"predicate":{"name":"status","operator":"eq","values":[{"type":"string","value":"paid"}]}},
		"sorts":[{"name":"created_at","direction":"desc"}],
		"page":{"mode":"cursor","size":20}
	}`), 2048)
	if err != nil {
		t.Fatalf("rpc Parse() error = %v", err)
	}
	filter := url.QueryEscape(`{"predicate":{"name":"status","operator":"eq","values":[{"type":"string","value":"paid"}]}}`)
	httpRequest, err := apiqueryhttp.Parse("schema_revision=v1&fields=status&filter="+filter+
		"&sort=-created_at&page%5Bmode%5D=cursor&page%5Bsize%5D=20", 2048)
	if err != nil {
		t.Fatalf("http Parse() error = %v", err)
	}
	jsonAPIQuery, err := jsonapi.ParseQuery(url.Values{
		"fields[orders]": {"status"},
		"filter[status]": {"paid"},
		"sort":           {"-created_at"},
		"page[size]":     {"20"},
	})
	if err != nil {
		t.Fatalf("jsonapi ParseQuery() error = %v", err)
	}
	jsonAPIRequest, err := apiqueryjsonapi.FromQuery(jsonAPIQuery, apiqueryjsonapi.Config{
		Resource: "orders",
		DecodeFilter: func(family jsonapi.ParameterFamily) (*apiquery.FilterExpr, error) {
			return &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
				Name: "status", Operator: apiquery.OpEqual,
				Values: []apiquery.Value{apiquery.StringValue(family["filter[status]"][0])},
			}}, nil
		},
		DecodePage: func(jsonapi.ParameterFamily) (apiquery.PageRequest, error) {
			return apiquery.PageRequest{Mode: apiquery.PageCursor, Size: 20}, nil
		},
	})
	if err != nil {
		t.Fatalf("jsonapi FromQuery() error = %v", err)
	}

	rpcPlan, err := apiquery.Compile(context.Background(), schema, rpcParams.Request(), apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile(rpc) error = %v", err)
	}
	httpPlan, err := apiquery.Compile(context.Background(), schema, httpRequest, apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile(http) error = %v", err)
	}
	jsonAPIPlan, err := apiquery.Compile(context.Background(), schema, jsonAPIRequest,
		apiquery.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile(jsonapi) error = %v", err)
	}
	rpcCanonical, _ := rpcPlan.Canonical()
	httpCanonical, _ := httpPlan.Canonical()
	jsonAPICanonical, _ := jsonAPIPlan.Canonical()
	if string(rpcCanonical) != string(httpCanonical) ||
		string(rpcCanonical) != string(jsonAPICanonical) {
		t.Fatalf("canonical plans differ\nrpc:     %s\nhttp:    %s\njsonapi: %s",
			rpcCanonical, httpCanonical, jsonAPICanonical)
	}

	descriptor := apiqueryrpc.OpenRPCContentDescriptor()
	properties, ok := descriptor.Schema["properties"].(map[string]any)
	if !ok || descriptor.Schema["additionalProperties"] != false {
		t.Fatalf("OpenRPC descriptor does not describe a closed query object: %#v", descriptor)
	}
	for _, name := range []string{"schema_revision", "fields", "includes", "filter", "sorts", "page"} {
		if _, exists := properties[name]; !exists {
			t.Fatalf("OpenRPC descriptor omits JSON-RPC query member %q", name)
		}
	}
}

func TestTransportsCanonicalizeEquivalentAuthenticatedCursors(t *testing.T) {
	t.Parallel()

	schema := transportSchema(t)
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	keys, err := cursor.NewKeyring(cursor.Key{ID: "active",
		Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := cursor.NewCodec(cursor.Config{Version: "v1", Keys: keys,
		MaxEncodedBytes: 2048, MaxPositions: 2, MaxStringBytes: 128,
		MaxTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sorts := []apiquery.SortTerm{{Name: "created_at", Direction: apiquery.Descending},
		{Name: "id", Direction: apiquery.Ascending}}
	payload := cursor.Payload{SchemaRevision: "v1", Direction: cursor.Forward, Sorts: sorts,
		Positions: []apiquery.Value{apiquery.TimeValue(now), apiquery.StringValue("order-42")},
		ExpiresAt: now.Add(time.Minute)}
	rpcToken, err := codec.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	httpToken, err := codec.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	rpcParams, err := apiqueryrpc.Parse([]byte(fmt.Sprintf(
		`{"sorts":[{"name":"created_at","direction":"desc"}],"page":{"mode":"cursor","after":%q}}`,
		rpcToken)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := apiqueryhttp.Parse("sort=-created_at&page%5Bmode%5D=cursor&page%5Bafter%5D="+
		url.QueryEscape(httpToken), 4096)
	if err != nil {
		t.Fatal(err)
	}
	options := apiquery.CompileOptions{CursorDecoder: codec}
	rpcPlan, err := apiquery.Compile(context.Background(), schema, rpcParams.Request(), options)
	if err != nil {
		t.Fatal(err)
	}
	httpPlan, err := apiquery.Compile(context.Background(), schema, httpRequest, options)
	if err != nil {
		t.Fatal(err)
	}
	rpcCanonical, _ := rpcPlan.Canonical()
	httpCanonical, _ := httpPlan.Canonical()
	if string(rpcCanonical) != string(httpCanonical) || string(rpcCanonical) == "" {
		t.Fatalf("cursor plans differ: rpc=%s http=%s", rpcCanonical, httpCanonical)
	}
}

func TestTransportParsersRejectAmbiguityAndExcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parse func() error
	}{
		{name: "HTTP duplicate", parse: func() error {
			_, err := apiqueryhttp.Parse("fields=id&fields=status", 100)
			return err
		}},
		{name: "HTTP unknown", parse: func() error {
			_, err := apiqueryhttp.Parse("sql=drop", 100)
			return err
		}},
		{name: "HTTP malformed escape", parse: func() error {
			_, err := apiqueryhttp.Parse("fields=%zz", 100)
			return err
		}},
		{name: "RPC duplicate", parse: func() error {
			_, err := apiqueryrpc.Parse([]byte(`{"fields":[],"fields":["id"]}`), 100)
			return err
		}},
		{name: "RPC unknown", parse: func() error {
			_, err := apiqueryrpc.Parse([]byte(`{"sql":"drop"}`), 100)
			return err
		}},
		{name: "RPC oversized", parse: func() error {
			_, err := apiqueryrpc.Parse([]byte(`{"fields":["id"]}`), 5)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.parse(); !errors.Is(err, apiqueryrpc.ErrInvalid) &&
				!errors.Is(err, apiqueryhttp.ErrInvalid) {
				t.Fatalf("parse error = %v, want transport ErrInvalid", err)
			}
		})
	}
}

func TestTransportsPreserveExplicitlyEmptyFields(t *testing.T) {
	t.Parallel()

	rpcParams, err := apiqueryrpc.Parse([]byte(`{"fields":[]}`), 100)
	if err != nil {
		t.Fatalf("rpc Parse() error = %v", err)
	}
	httpRequest, err := apiqueryhttp.Parse("fields=", 100)
	if err != nil {
		t.Fatalf("http Parse() error = %v", err)
	}
	if _, present := rpcParams.Request().Fields.Value(); !present {
		t.Fatal("RPC lost explicitly empty fields")
	}
	if fields, present := httpRequest.Fields.Value(); !present || len(fields) != 0 {
		t.Fatalf("HTTP fields = %v, present = %v", fields, present)
	}
}

func transportSchema(t *testing.T) *apiquery.Schema {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "status", Type: apiquery.TypeString},
		},
		Filters: []apiquery.FilterDefinition{{Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual}}},
		Sorts: []apiquery.SortDefinition{
			{Name: "created_at", Type: apiquery.TypeTime},
			{Name: "id", Type: apiquery.TypeString, TieBreaker: true},
		},
		Pagination: apiquery.PaginationDefinition{Cursor: true, DefaultPageSize: 25},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	return schema
}
