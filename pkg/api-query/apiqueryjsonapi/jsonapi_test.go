package apiqueryjsonapi_test

import (
	"errors"
	"net/url"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiqueryjsonapi"
	jsonapi "github.com/faustbrian/golib/pkg/jsonapi"
)

func TestBridgeComposesAuthoritativeJSONAPIQuery(t *testing.T) {
	t.Parallel()

	query, err := jsonapi.ParseQuery(url.Values{
		"fields[orders]": {"status"},
		"include":        {"customer.address"},
		"sort":           {"-created_at,id"},
		"filter[status]": {"paid"},
		"page[size]":     {"20"},
	})
	if err != nil {
		t.Fatalf("jsonapi.ParseQuery() error = %v", err)
	}
	request, err := apiqueryjsonapi.FromQuery(query, apiqueryjsonapi.Config{
		Resource: "orders",
		DecodeFilter: func(family jsonapi.ParameterFamily) (*apiquery.FilterExpr, error) {
			values := family["filter[status]"]
			return &apiquery.FilterExpr{Predicate: &apiquery.Predicate{
				Name: "status", Operator: apiquery.OpEqual,
				Values: []apiquery.Value{apiquery.StringValue(values[0])},
			}}, nil
		},
		DecodePage: func(family jsonapi.ParameterFamily) (apiquery.PageRequest, error) {
			if family["page[size]"][0] != "20" {
				t.Fatal("page family was altered before application decoding")
			}
			return apiquery.PageRequest{Mode: apiquery.PageCursor, Size: 20}, nil
		},
	})
	if err != nil {
		t.Fatalf("FromQuery() error = %v", err)
	}
	fields, present := request.Fields.Value()
	if !present || len(fields) != 1 || fields[0] != "status" {
		t.Fatalf("fields = %v, present = %v", fields, present)
	}
	includes, present := request.Includes.Value()
	if !present || len(includes) != 1 || includes[0] != "customer.address" {
		t.Fatalf("includes = %v, present = %v", includes, present)
	}
	sorts, present := request.Sorts.Value()
	if !present || len(sorts) != 2 || sorts[0].Direction != apiquery.Descending {
		t.Fatalf("sorts = %#v, present = %v", sorts, present)
	}
}

func TestBridgeRefusesToInterpretJSONAPIFamilies(t *testing.T) {
	t.Parallel()

	query, err := jsonapi.ParseQuery(url.Values{"filter[status]": {"paid"}})
	if err != nil {
		t.Fatalf("jsonapi.ParseQuery() error = %v", err)
	}
	_, err = apiqueryjsonapi.FromQuery(query, apiqueryjsonapi.Config{Resource: "orders"})
	if !errors.Is(err, apiqueryjsonapi.ErrUnsupported) {
		t.Fatalf("FromQuery() error = %v, want ErrUnsupported", err)
	}
}

func TestBridgeFailureMatrix(t *testing.T) {
	t.Parallel()

	failure := errors.New("decoder failed")
	tests := []struct {
		name   string
		query  jsonapi.Query
		config apiqueryjsonapi.Config
		want   error
	}{
		{name: "missing resource", config: apiqueryjsonapi.Config{}, want: apiqueryjsonapi.ErrInvalid},
		{name: "missing page decoder", query: jsonapi.Query{Page: jsonapi.ParameterFamily{"page[size]": {"1"}}},
			config: apiqueryjsonapi.Config{Resource: "orders"}, want: apiqueryjsonapi.ErrUnsupported},
		{name: "filter decoder error", query: jsonapi.Query{Filter: jsonapi.ParameterFamily{"filter[x]": {"1"}}},
			config: apiqueryjsonapi.Config{Resource: "orders", DecodeFilter: func(jsonapi.ParameterFamily) (*apiquery.FilterExpr, error) { return nil, failure }}, want: apiqueryjsonapi.ErrInvalid},
		{name: "filter decoder nil", query: jsonapi.Query{Filter: jsonapi.ParameterFamily{"filter[x]": {"1"}}},
			config: apiqueryjsonapi.Config{Resource: "orders", DecodeFilter: func(jsonapi.ParameterFamily) (*apiquery.FilterExpr, error) { return nil, nil }}, want: apiqueryjsonapi.ErrInvalid},
		{name: "page decoder error", query: jsonapi.Query{Page: jsonapi.ParameterFamily{"page[size]": {"1"}}},
			config: apiqueryjsonapi.Config{Resource: "orders", DecodePage: func(jsonapi.ParameterFamily) (apiquery.PageRequest, error) { return apiquery.PageRequest{}, failure }}, want: apiqueryjsonapi.ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := apiqueryjsonapi.FromQuery(test.query, test.config); !errors.Is(err, test.want) {
				t.Fatalf("FromQuery() error = %v, want %v", err, test.want)
			}
		})
	}
}
