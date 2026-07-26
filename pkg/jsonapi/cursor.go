package jsonapi

import (
	"fmt"
	"sort"
	"strconv"
)

// CursorPaginationProfileURI identifies the official Cursor Pagination
// profile. The profile normatively uses this HTTP URI.
const CursorPaginationProfileURI = "http://jsonapi.org/profiles/ethanresnick/cursor-pagination/"

const (
	// CursorUnsupportedSortTypeURI identifies an unsupported sort error.
	CursorUnsupportedSortTypeURI = "https://jsonapi.org/profiles/ethanresnick/cursor-pagination/unsupported-sort"
	// CursorMaxSizeExceededTypeURI identifies a maximum page size error.
	CursorMaxSizeExceededTypeURI = "https://jsonapi.org/profiles/ethanresnick/cursor-pagination/max-size-exceeded"
	// CursorRangeNotSupportedTypeURI identifies a rejected range request.
	CursorRangeNotSupportedTypeURI = "https://jsonapi.org/profiles/ethanresnick/cursor-pagination/range-pagination-not-supported"
)

// CursorPaginationConfig defines endpoint-specific page sizing, range
// support, and opaque cursor validation.
type CursorPaginationConfig struct {
	DefaultSize    int
	MaxSize        int
	AllowRange     bool
	PageMember     string
	ValidateCursor func(string) error
	ValidateSort   func([]SortField) error
}

// CursorPageRequest is the validated cursor pagination request for an
// endpoint.
type CursorPageRequest struct {
	Size          int
	SizePresent   bool
	After         string
	AfterPresent  bool
	Before        string
	BeforePresent bool
	Range         bool
	PageMember    string
}

// CursorPaginationError describes a profile query failure and its required
// HTTP status.
type CursorPaginationError struct {
	Status     int
	Parameter  string
	Code       string
	Message    string
	MaxSize    int
	PageMember string
	Cause      error
}

// Error implements error.
func (err *CursorPaginationError) Error() string {
	return fmt.Sprintf("invalid cursor pagination parameter %q: %s", err.Parameter, err.Message)
}

// Unwrap returns an application cursor or sort validator failure without
// including its potentially sensitive text in Error.
func (err *CursorPaginationError) Unwrap() error {
	return err.Cause
}

// ErrorObject converts a profile failure to a JSON:API error object with the
// required source, type link, and profile metadata.
func (err *CursorPaginationError) ErrorObject(title, detail string) ErrorObject {
	object := ErrorObject{
		Status: "400",
		Code:   err.Code,
		Title:  title,
		Detail: detail,
		Source: &ErrorSource{Parameter: err.Parameter},
	}
	var typeURI string
	switch err.Code {
	case "unsupported-sort":
		typeURI = CursorUnsupportedSortTypeURI
	case "max-size-exceeded":
		typeURI = CursorMaxSizeExceededTypeURI
		member, memberErr := cursorPageMember(err.PageMember)
		if memberErr != nil {
			member = "page"
		}
		object.Meta = Meta{member: map[string]any{"maxSize": err.MaxSize}}
	case "range-not-supported":
		typeURI = CursorRangeNotSupportedTypeURI
	}
	if typeURI != "" {
		object.Links = Links{"type": URI(typeURI)}
	}

	return object
}

// ValidateCursorPaginationLinks enforces the profile requirement that every
// paginated data instance contains both prev and next links.
func ValidateCursorPaginationLinks(links Links) error {
	validator := documentValidator{}
	if _, exists := links["prev"]; !exists {
		validator.add("/links/prev", "required", "cursor pagination requires a prev link")
	}
	if _, exists := links["next"]; !exists {
		validator.add("/links/next", "required", "cursor pagination requires a next link")
	}
	validator.validateLinks(links, "/links")
	if len(validator.violations) == 0 {
		return nil
	}

	return &ValidationError{Violations: validator.violations}
}

// CursorPagination parses the page family for one configured endpoint.
type CursorPagination struct {
	defaultSize    int
	maxSize        int
	allowRange     bool
	validateCursor func(string) error
	validateSort   func([]SortField) error
	pageMember     string
}

// NewCursorPagination validates endpoint policy before it serves requests.
// A MaxSize of zero means unbounded ordinary pagination; range pagination
// requires a finite maximum because that maximum becomes its default size.
func NewCursorPagination(config CursorPaginationConfig) (*CursorPagination, error) {
	if config.DefaultSize < 1 {
		return nil, fmt.Errorf("cursor pagination default size must be positive")
	}
	if config.MaxSize < 0 {
		return nil, fmt.Errorf("cursor pagination max size must not be negative")
	}
	if config.MaxSize > 0 && config.DefaultSize > config.MaxSize {
		return nil, fmt.Errorf("cursor pagination default size must not exceed max size")
	}
	if config.AllowRange && config.MaxSize == 0 {
		return nil, fmt.Errorf("cursor range pagination requires a finite max size")
	}
	pageMember, err := cursorPageMember(config.PageMember)
	if err != nil {
		return nil, err
	}

	return &CursorPagination{
		defaultSize:    config.DefaultSize,
		maxSize:        config.MaxSize,
		allowRange:     config.AllowRange,
		validateCursor: config.ValidateCursor,
		validateSort:   config.ValidateSort,
		pageMember:     pageMember,
	}, nil
}

// ParseQuery validates both the page family and the endpoint's stable sorting
// requirement. When ValidateSort is nil, the caller remains responsible for
// applying a unique order before fetching the page.
func (pagination *CursorPagination) ParseQuery(query Query) (CursorPageRequest, error) {
	request, err := pagination.Parse(query.Page)
	if err != nil {
		return CursorPageRequest{}, err
	}
	if pagination.validateSort != nil {
		if err := callApplicationCallback("sort", func() error {
			return pagination.validateSort(query.Sort)
		}); err != nil {
			return CursorPageRequest{}, pagination.failureWithCause(
				"sort", "unsupported-sort", "requested sort is unsupported", 0, err,
			)
		}
	}

	return request, nil
}

// Parse validates the page parameter family according to the profile and
// endpoint configuration.
func (pagination *CursorPagination) Parse(family ParameterFamily) (CursorPageRequest, error) {
	names := make([]string, 0, len(family))
	for name := range family {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "page[size]" && name != "page[after]" && name != "page[before]" {
			return CursorPageRequest{}, pagination.failure(
				name,
				"unknown-parameter",
				"parameter is not defined by the Cursor Pagination profile",
				0,
			)
		}
	}

	request := CursorPageRequest{Size: pagination.defaultSize, PageMember: pagination.pageMember}
	if values, exists := family["page[size]"]; exists {
		if len(values) != 1 {
			return CursorPageRequest{}, pagination.failure(
				"page[size]", "multiple-values", "page size must occur once", 0,
			)
		}
		size, err := positiveDecimal(values[0])
		if err != nil {
			return CursorPageRequest{}, pagination.failure(
				"page[size]", "invalid-parameter", "page size must be a positive integer", 0,
			)
		}
		if pagination.maxSize > 0 && size > pagination.maxSize {
			return CursorPageRequest{}, pagination.failure(
				"page[size]",
				"max-size-exceeded",
				"page size exceeds the endpoint maximum",
				pagination.maxSize,
			)
		}
		request.Size = size
		request.SizePresent = true
	}

	for _, field := range []struct {
		name    string
		target  *string
		present *bool
	}{
		{"page[after]", &request.After, &request.AfterPresent},
		{"page[before]", &request.Before, &request.BeforePresent},
	} {
		values, exists := family[field.name]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return CursorPageRequest{}, pagination.failure(
				field.name, "multiple-values", "cursor parameter must occur once", 0,
			)
		}
		if pagination.validateCursor != nil {
			if err := callApplicationCallback("cursor", func() error {
				return pagination.validateCursor(values[0])
			}); err != nil {
				return CursorPageRequest{}, pagination.failureWithCause(
					field.name,
					"invalid-parameter",
					"cursor value is invalid",
					0,
					err,
				)
			}
		}
		*field.target = values[0]
		*field.present = true
	}

	request.Range = request.AfterPresent && request.BeforePresent
	if request.Range && !pagination.allowRange {
		return CursorPageRequest{}, pagination.failure(
			"page[before]",
			"range-not-supported",
			"endpoint does not support range pagination",
			0,
		)
	}
	if request.Range && !request.SizePresent {
		request.Size = pagination.maxSize
	}

	return request, nil
}

func positiveDecimal(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty decimal")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("non-decimal character")
		}
	}
	size, err := strconv.Atoi(value)
	if err != nil || size < 1 {
		return 0, fmt.Errorf("decimal must be positive")
	}

	return size, nil
}

func (pagination *CursorPagination) failure(
	parameter, code, message string,
	maxSize int,
) *CursorPaginationError {
	err := cursorFailure(parameter, code, message, maxSize)
	err.PageMember = pagination.pageMember
	return err
}

func (pagination *CursorPagination) failureWithCause(
	parameter, code, message string,
	maxSize int,
	cause error,
) error {
	err := pagination.failure(parameter, code, message, maxSize)
	err.Cause = cause
	return err
}

func cursorFailure(parameter, code, message string, maxSize int) *CursorPaginationError {
	return &CursorPaginationError{
		Status:    400,
		Parameter: parameter,
		Code:      code,
		Message:   message,
		MaxSize:   maxSize,
	}
}
