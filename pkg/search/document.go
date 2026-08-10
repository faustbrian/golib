package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var (
	ErrTenantRequired      = errors.New("search: tenant is required")
	ErrTenantTooLarge      = errors.New("search: tenant exceeds limit")
	ErrIndexRequired       = errors.New("search: index is required")
	ErrIndexTooLarge       = errors.New("search: index exceeds limit")
	ErrIDRequired          = errors.New("search: document ID is required")
	ErrIDTooLarge          = errors.New("search: document ID exceeds limit")
	ErrVersionRequired     = errors.New("search: external version is required")
	ErrSourceRequired      = errors.New("search: document source is required")
	ErrSourceTooLarge      = errors.New("search: document source exceeds limit")
	ErrInvalidSource       = errors.New("search: document source must be a JSON object")
	ErrJSONDepthLimit      = errors.New("search: JSON nesting depth exceeds limit")
	ErrJSONNodeLimit       = errors.New("search: JSON object-field or array-element count exceeds limit")
	ErrDuplicateJSONKey    = errors.New("search: JSON object contains a duplicate key")
	errMalformedJSONObject = errors.New("search: malformed JSON object")
)

// Document is a rebuildable source projection with a stable ID and a
// source-owned positive external version. Source is owned by this value.
type Document struct {
	Tenant  string
	Index   string
	ID      string
	Version uint64
	Source  json.RawMessage
}

// NewDocument validates and copies a bounded JSON object document.
func NewDocument(tenant, index, id string, version uint64, source json.RawMessage, limits Limits) (Document, error) {
	if tenant == "" {
		return Document{}, ErrTenantRequired
	}
	if len(tenant) > limits.MaxTenantBytes {
		return Document{}, ErrTenantTooLarge
	}
	if index == "" {
		return Document{}, ErrIndexRequired
	}
	if len(index) > limits.MaxIndexBytes {
		return Document{}, ErrIndexTooLarge
	}
	if id == "" {
		return Document{}, ErrIDRequired
	}
	if len(id) > limits.MaxIDBytes {
		return Document{}, ErrIDTooLarge
	}
	if version == 0 {
		return Document{}, ErrVersionRequired
	}
	if source == nil {
		return Document{}, ErrSourceRequired
	}
	if len(source) > limits.MaxSourceBytes {
		return Document{}, ErrSourceTooLarge
	}
	if limits.MaxJSONDepth <= 0 || limits.MaxJSONNodes <= 0 {
		return Document{}, ErrInvalidLimits
	}
	trimmed := bytes.TrimSpace(source)
	if len(trimmed) < 2 {
		return Document{}, ErrInvalidSource
	}
	if trimmed[0] != '{' {
		return Document{}, ErrInvalidSource
	}
	if trimmed[len(trimmed)-1] != '}' {
		return Document{}, ErrInvalidSource
	}
	remainingNodes := limits.MaxJSONNodes
	if err := validateBoundedJSONObject(trimmed, limits.MaxJSONDepth, &remainingNodes); err != nil {
		return Document{}, errors.Join(ErrInvalidSource, err)
	}

	return Document{Tenant: tenant, Index: index, ID: id, Version: version, Source: bytes.Clone(source)}, nil
}

func validateBoundedJSONObject(value json.RawMessage, maximumDepth int, remainingNodes *int) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return errMalformedJSONObject
	}
	delimiter, ok := opening.(json.Delim)
	if !ok || delimiter != '{' {
		return errMalformedJSONObject
	}
	if err := validateJSONContainer(decoder, delimiter, 1, maximumDepth, remainingNodes); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errMalformedJSONObject
	}
	return nil
}

func validateJSONContainer(decoder *json.Decoder, opening json.Delim, depth, maximumDepth int, remainingNodes *int) error {
	if depth > maximumDepth {
		return ErrJSONDepthLimit
	}
	var keys map[string]struct{}
	if opening == '{' {
		keys = make(map[string]struct{})
	}
	for decoder.More() {
		if keys != nil {
			token, err := decoder.Token()
			if err != nil {
				return errMalformedJSONObject
			}
			key, ok := token.(string)
			if !ok {
				return errMalformedJSONObject
			}
			if _, duplicate := keys[key]; duplicate {
				return ErrDuplicateJSONKey
			}
			keys[key] = struct{}{}
		}
		if *remainingNodes == 0 {
			return ErrJSONNodeLimit
		}
		*remainingNodes = *remainingNodes - 1
		value, err := decoder.Token()
		if err != nil {
			return errMalformedJSONObject
		}
		if delimiter, nested := value.(json.Delim); nested {
			if delimiter != '{' && delimiter != '[' {
				return errMalformedJSONObject
			}
			if err := validateJSONContainer(decoder, delimiter, depth+1, maximumDepth, remainingNodes); err != nil {
				return err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return errMalformedJSONObject
	}
	expected := json.Delim('}')
	if opening == '[' {
		expected = ']'
	}
	if closing != expected {
		return errMalformedJSONObject
	}
	return nil
}
