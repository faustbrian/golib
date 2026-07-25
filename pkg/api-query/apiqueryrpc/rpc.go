// Package apiqueryrpc parses bounded JSON-RPC query parameters and describes
// their OpenRPC content without compiling or executing a query.
package apiqueryrpc

import (
	"errors"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/internal/strictjson"
)

// ErrInvalid is the sanitized JSON-RPC parameter failure.
var ErrInvalid = errors.New("API query RPC parameters are invalid")

// Params preserves absent versus explicitly empty JSON-RPC components.
type Params struct {
	SchemaRevision *string               `json:"schema_revision,omitempty"`
	Fields         *[]string             `json:"fields,omitempty"`
	Includes       *[]string             `json:"includes,omitempty"`
	Filter         *apiquery.FilterExpr  `json:"filter,omitempty"`
	Sorts          *[]apiquery.SortTerm  `json:"sorts,omitempty"`
	Page           *apiquery.PageRequest `json:"page,omitempty"`
}

// Parse strictly decodes one bounded JSON-RPC parameter object.
func Parse(data []byte, maxBytes int) (Params, error) {
	var params Params
	if err := strictjson.Decode(data, maxBytes, &params); err != nil {
		return Params{}, ErrInvalid
	}
	return params, nil
}

// Request returns a transport-neutral defensive snapshot.
func (p Params) Request() apiquery.Request {
	request := apiquery.Request{Filter: cloneFilter(p.Filter)}
	if p.SchemaRevision != nil {
		request.SchemaRevision = apiquery.Present(*p.SchemaRevision)
	}
	if p.Fields != nil {
		request.Fields = apiquery.Present(append([]string(nil), (*p.Fields)...))
	}
	if p.Includes != nil {
		request.Includes = apiquery.Present(append([]string(nil), (*p.Includes)...))
	}
	if p.Sorts != nil {
		request.Sorts = apiquery.Present(append([]apiquery.SortTerm(nil), (*p.Sorts)...))
	}
	if p.Page != nil {
		request.Page = *p.Page
	}
	return request
}

// ContentDescriptor is a minimal OpenRPC-compatible parameter descriptor.
type ContentDescriptor struct {
	Name     string         `json:"name"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}

// OpenRPCContentDescriptor describes the query parameter object. The returned
// maps are newly allocated and safe for caller customization.
func OpenRPCContentDescriptor() ContentDescriptor {
	return ContentDescriptor{Name: "query", Required: false, Schema: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"schema_revision": map[string]any{"type": "string"},
			"fields":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"includes":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"filter":          map[string]any{"type": "object"},
			"sorts":           map[string]any{"type": "array"},
			"page":            map[string]any{"type": "object"},
		},
	}}
}

func cloneFilter(filter *apiquery.FilterExpr) *apiquery.FilterExpr {
	if filter == nil {
		return nil
	}
	result := &apiquery.FilterExpr{Logic: filter.Logic}
	if filter.Predicate != nil {
		result.Predicate = &apiquery.Predicate{Name: filter.Predicate.Name,
			Operator: filter.Predicate.Operator,
			Values:   append([]apiquery.Value(nil), filter.Predicate.Values...)}
	}
	result.Children = make([]apiquery.FilterExpr, len(filter.Children))
	for index := range filter.Children {
		result.Children[index] = *cloneFilter(&filter.Children[index])
	}
	return result
}
