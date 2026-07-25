package apiqueryhttp

import (
	"errors"
	"net/url"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestParseCompleteQuery(t *testing.T) {
	t.Parallel()

	filter := url.QueryEscape(`{"logic":"and","children":[{"predicate":{"name":"status","operator":"eq","values":[{"type":"string","value":"paid"}]}}]}`)
	request, err := Parse("schema_revision=v1&fields=id,status&include=customer.address&filter="+filter+
		"&sort=-created_at,id&page%5Bmode%5D=cursor&page%5Bsize%5D=20&page%5Bbefore%5D=opaque", 2048)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if revision, present := request.SchemaRevision.Value(); !present || revision != "v1" {
		t.Fatalf("revision = %q, present = %v", revision, present)
	}
	if fields, _ := request.Fields.Value(); len(fields) != 2 {
		t.Fatalf("fields = %v", fields)
	}
	if includes, _ := request.Includes.Value(); len(includes) != 1 {
		t.Fatalf("includes = %v", includes)
	}
	if request.Filter == nil || len(request.Filter.Children) != 1 {
		t.Fatalf("filter = %#v", request.Filter)
	}
	if sorts, _ := request.Sorts.Value(); len(sorts) != 2 || sorts[0].Direction != apiquery.Descending {
		t.Fatalf("sorts = %#v", sorts)
	}
	if request.Page.Size != 20 || request.Page.Before != "opaque" {
		t.Fatalf("page = %#v", request.Page)
	}
}

func TestParseBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, raw string
		max       int
	}{
		{"disabled limit", "", 0},
		{"excess bytes", "fields=id", 2},
		{"invalid raw UTF8", "fields=\xff", 100},
		{"malformed encoding", "fields=%zz", 100},
		{"semicolon encoding", "fields=id;sort=id", 100},
		{"unknown", "sql=drop", 100},
		{"duplicate", "fields=id&fields=status", 100},
		{"invalid decoded UTF8", "fields=%FF", 100},
		{"empty list member", "fields=id,,status", 100},
		{"empty include member", "include=customer,", 100},
		{"invalid filter", "filter=%7B", 100},
		{"empty sort name", "sort=-", 100},
		{"invalid page size", "page%5Bsize%5D=x", 100},
		{"invalid page offset", "page%5Boffset%5D=x", 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(test.raw, test.max); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestParseEmptyAndOffsetComponents(t *testing.T) {
	t.Parallel()

	request, err := Parse("fields=&include=&sort=&page%5Bmode%5D=offset&page%5Boffset%5D=4&page%5Bafter%5D=a", 200)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	fields, fieldsPresent := request.Fields.Value()
	includes, includesPresent := request.Includes.Value()
	sorts, sortsPresent := request.Sorts.Value()
	if !fieldsPresent || !includesPresent || !sortsPresent || len(fields)+len(includes)+len(sorts) != 0 {
		t.Fatalf("empty components were not preserved")
	}
	if request.Page.Mode != apiquery.PageOffset || request.Page.Offset != 4 || request.Page.After != "a" {
		t.Fatalf("page = %#v", request.Page)
	}
	if _, err := Parse("", 1); err != nil {
		t.Fatalf("Parse(empty) error = %v", err)
	}
	if _, valid := sortList("id,,status"); valid {
		t.Fatal("sortList accepted an empty member")
	}
}
