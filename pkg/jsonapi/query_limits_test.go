package jsonapi

import (
	"errors"
	"net/url"
	"testing"
)

func TestQueryLimitsRejectExcessiveDecodedQueries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		values url.Values
		limits QueryLimits
	}{
		"parameter names": {
			values: url.Values{"include": {"author"}},
			limits: QueryLimits{MaxNameBytes: 3},
		},
		"parameter values": {
			values: url.Values{"include": {"author"}},
			limits: QueryLimits{MaxValueBytes: 3},
		},
		"distinct parameters": {
			values: url.Values{"include": {"author"}, "sort": {"id"}},
			limits: QueryLimits{MaxParameters: 1},
		},
		"total values": {
			values: url.Values{"page[x]": {"one", "two"}},
			limits: QueryLimits{MaxValues: 1},
		},
		"total bytes": {
			values: url.Values{"include": {"author"}},
			limits: QueryLimits{MaxTotalBytes: 5},
		},
		"selector depth": {
			values: url.Values{"filter[a][b]": {"one"}},
			limits: QueryLimits{MaxSelectors: 1},
		},
		"comma list items": {
			values: url.Values{"include": {"author,comments"}},
			limits: QueryLimits{MaxListItems: 1},
		},
		"fieldset list items": {
			values: url.Values{"fields[articles]": {"title,body"}},
			limits: QueryLimits{MaxListItems: 1},
		},
		"sort list items": {
			values: url.Values{"sort": {"created,id"}},
			limits: QueryLimits{MaxListItems: 1},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseQueryWithLimits(test.values, test.limits)
			var queryError *QueryError
			if !errors.As(err, &queryError) || queryError.Code != "limit" {
				t.Fatalf("unexpected query limit error: %T %#v", err, queryError)
			}
		})
	}
}

func TestConfiguredQueryParserUsesLimits(t *testing.T) {
	t.Parallel()

	parser, err := NewQueryParserWithLimits(
		[]string{"customParam"},
		nil,
		QueryLimits{MaxValues: 1},
	)
	if err != nil {
		t.Fatalf("construct limited query parser: %v", err)
	}
	_, err = parser.Parse(url.Values{"customParam": {"one", "two"}})
	var queryError *QueryError
	if !errors.As(err, &queryError) || queryError.Code != "limit" {
		t.Fatalf("configured parser ignored limits: %T %#v", err, queryError)
	}
}

func TestQueryLimitConfiguration(t *testing.T) {
	t.Parallel()

	defaults := DefaultQueryLimits()
	if defaults.MaxParameters < 1 || defaults.MaxValues < 1 ||
		defaults.MaxNameBytes < 1 || defaults.MaxValueBytes < 1 ||
		defaults.MaxTotalBytes < 1 || defaults.MaxSelectors < 1 ||
		defaults.MaxListItems < 1 {
		t.Fatalf("unsafe query defaults: %#v", defaults)
	}
	if _, err := NewQueryParserWithLimits(
		nil,
		nil,
		QueryLimits{MaxParameters: -1},
	); err == nil {
		t.Fatal("expected invalid query limits error")
	}
	if _, err := ParseQueryWithLimits(
		nil,
		QueryLimits{MaxValueBytes: -1},
	); err == nil {
		t.Fatal("expected invalid direct query limits error")
	}
}
