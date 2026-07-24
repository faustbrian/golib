// Package apiqueryhttp strictly parses conventional HTTP query strings into
// transport-neutral API query requests.
package apiqueryhttp

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/internal/strictjson"
)

// ErrInvalid is the sanitized HTTP query failure.
var ErrInvalid = errors.New("API query string is invalid")

var supported = map[string]struct{}{
	"schema_revision": {}, "fields": {}, "include": {}, "filter": {}, "sort": {},
	"page[mode]": {}, "page[size]": {}, "page[after]": {}, "page[before]": {}, "page[offset]": {},
}

// Parse validates bytes, encoding, names, duplicates, and component syntax.
func Parse(rawQuery string, maxBytes int) (apiquery.Request, error) {
	if maxBytes <= 0 || len(rawQuery) > maxBytes || !utf8.ValidString(rawQuery) {
		return apiquery.Request{}, ErrInvalid
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return apiquery.Request{}, ErrInvalid
	}
	for name, entries := range values {
		if _, ok := supported[name]; !ok || len(entries) != 1 || !utf8.ValidString(entries[0]) {
			return apiquery.Request{}, ErrInvalid
		}
	}
	request := apiquery.Request{}
	if value, ok := one(values, "schema_revision"); ok {
		request.SchemaRevision = apiquery.Present(value)
	}
	if value, ok := one(values, "fields"); ok {
		parsed, valid := commaList(value)
		if !valid {
			return apiquery.Request{}, ErrInvalid
		}
		request.Fields = apiquery.Present(parsed)
	}
	if value, ok := one(values, "include"); ok {
		parsed, valid := commaList(value)
		if !valid {
			return apiquery.Request{}, ErrInvalid
		}
		request.Includes = apiquery.Present(parsed)
	}
	if value, ok := one(values, "filter"); ok {
		var filter apiquery.FilterExpr
		if strictjson.Decode([]byte(value), maxBytes, &filter) != nil {
			return apiquery.Request{}, ErrInvalid
		}
		request.Filter = &filter
	}
	if value, ok := one(values, "sort"); ok {
		sorts, valid := sortList(value)
		if !valid {
			return apiquery.Request{}, ErrInvalid
		}
		request.Sorts = apiquery.Present(sorts)
	}
	if err := parsePage(values, &request.Page); err != nil {
		return apiquery.Request{}, ErrInvalid
	}
	return request, nil
}

func parsePage(values url.Values, page *apiquery.PageRequest) error {
	if value, ok := one(values, "page[mode]"); ok {
		page.Mode = apiquery.PageMode(value)
	}
	if value, ok := one(values, "page[after]"); ok {
		page.After = value
	}
	if value, ok := one(values, "page[before]"); ok {
		page.Before = value
	}
	for name, target := range map[string]*int{"page[size]": &page.Size, "page[offset]": &page.Offset} {
		if value, ok := one(values, name); ok {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			*target = parsed
		}
	}
	return nil
}

func commaList(value string) ([]string, bool) {
	if value == "" {
		return []string{}, true
	}
	items := strings.Split(value, ",")
	for _, item := range items {
		if item == "" {
			return nil, false
		}
	}
	return items, true
}

func sortList(value string) ([]apiquery.SortTerm, bool) {
	items, valid := commaList(value)
	if !valid {
		return nil, false
	}
	result := make([]apiquery.SortTerm, 0, len(items))
	for _, item := range items {
		direction := apiquery.Ascending
		if strings.HasPrefix(item, "-") {
			direction, item = apiquery.Descending, strings.TrimPrefix(item, "-")
		}
		if item == "" {
			return nil, false
		}
		result = append(result, apiquery.SortTerm{Name: item, Direction: direction})
	}
	return result, true
}

func one(values url.Values, name string) (string, bool) {
	entries, ok := values[name]
	if !ok {
		return "", false
	}
	return entries[0], true
}
