// Package apiqueryjsonapi composes parsed jsonapi queries with apiquery.
// JSON:API names, syntax, extensions, and recommendations remain exclusively
// owned by github.com/faustbrian/golib/pkg/jsonapi.
package apiqueryjsonapi

import (
	"errors"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	jsonapi "github.com/faustbrian/golib/pkg/jsonapi"
)

// ErrInvalid reports an invalid bridge configuration or callback result.
var ErrInvalid = errors.New("JSON:API query bridge is invalid")

// ErrUnsupported reports a JSON:API family that the application did not bind
// to its general query contract.
var ErrUnsupported = errors.New("JSON:API query family is not configured")

// FilterDecoder gives application code authority over JSON:API filter profile
// or implementation-specific semantics.
type FilterDecoder func(jsonapi.ParameterFamily) (*apiquery.FilterExpr, error)

// PageDecoder gives application code authority over JSON:API pagination
// profile or implementation-specific semantics.
type PageDecoder func(jsonapi.ParameterFamily) (apiquery.PageRequest, error)

// Config binds one resource fieldset and application-owned family decoders.
type Config struct {
	Resource     string
	DecodeFilter FilterDecoder
	DecodePage   PageDecoder
}

// FromQuery converts an already parsed jsonapi Query without reinterpreting
// JSON:API parameter syntax.
func FromQuery(query jsonapi.Query, config Config) (apiquery.Request, error) {
	if config.Resource == "" {
		return apiquery.Request{}, ErrInvalid
	}
	request := apiquery.Request{}
	if fields, present := query.Fields[config.Resource]; present {
		request.Fields = apiquery.Present(append([]string(nil), fields...))
	}
	if query.IncludePresent {
		request.Includes = apiquery.Present(append([]string(nil), query.Include...))
	}
	if query.SortPresent {
		sorts := make([]apiquery.SortTerm, len(query.Sort))
		for index, sort := range query.Sort {
			direction := apiquery.Ascending
			if sort.Descending {
				direction = apiquery.Descending
			}
			sorts[index] = apiquery.SortTerm{Name: sort.Name, Direction: direction}
		}
		request.Sorts = apiquery.Present(sorts)
	}
	if len(query.Filter) > 0 {
		if config.DecodeFilter == nil {
			return apiquery.Request{}, ErrUnsupported
		}
		filter, err := config.DecodeFilter(cloneFamily(query.Filter))
		if err != nil || filter == nil {
			return apiquery.Request{}, ErrInvalid
		}
		request.Filter = filter
	}
	if len(query.Page) > 0 {
		if config.DecodePage == nil {
			return apiquery.Request{}, ErrUnsupported
		}
		page, err := config.DecodePage(cloneFamily(query.Page))
		if err != nil {
			return apiquery.Request{}, ErrInvalid
		}
		request.Page = page
	}
	return request, nil
}

func cloneFamily(family jsonapi.ParameterFamily) jsonapi.ParameterFamily {
	result := make(jsonapi.ParameterFamily, len(family))
	for name, values := range family {
		result[name] = append([]string(nil), values...)
	}
	return result
}
