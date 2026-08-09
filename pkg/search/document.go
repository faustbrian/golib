package search

import (
	"bytes"
	"encoding/json"
	"errors"
)

var (
	ErrTenantRequired  = errors.New("search: tenant is required")
	ErrTenantTooLarge  = errors.New("search: tenant exceeds limit")
	ErrIndexRequired   = errors.New("search: index is required")
	ErrIndexTooLarge   = errors.New("search: index exceeds limit")
	ErrIDRequired      = errors.New("search: document ID is required")
	ErrIDTooLarge      = errors.New("search: document ID exceeds limit")
	ErrVersionRequired = errors.New("search: external version is required")
	ErrSourceRequired  = errors.New("search: document source is required")
	ErrSourceTooLarge  = errors.New("search: document source exceeds limit")
	ErrInvalidSource   = errors.New("search: document source must be a JSON object")
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
	if !json.Valid(trimmed) {
		return Document{}, ErrInvalidSource
	}

	return Document{Tenant: tenant, Index: index, ID: id, Version: version, Source: bytes.Clone(source)}, nil
}
