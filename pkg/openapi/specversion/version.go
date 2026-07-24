// Package specversion defines supported OpenAPI and Swagger versions without
// importing any version-specific model package.
package specversion

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMalformedVersion reports a syntactically malformed specification
// version.
var ErrMalformedVersion = errors.New("malformed specification version")

// ErrUnsupportedVersion reports a syntactically valid but unpinned version.
var ErrUnsupportedVersion = errors.New("unsupported specification version")

// Dialect identifies a version line with distinct model semantics.
type Dialect string

const (
	DialectSwagger20 Dialect = "swagger-2.0"
	DialectOAS30     Dialect = "openapi-3.0"
	DialectOAS31     Dialect = "openapi-3.1"
	DialectOAS32     Dialect = "openapi-3.2"
)

// Version preserves the exact supported patch revision. Its zero value is
// invalid.
type Version struct {
	raw     string
	dialect Dialect
}

var supportedVersions = map[string]Dialect{
	"2.0":   DialectSwagger20,
	"3.0.0": DialectOAS30,
	"3.0.1": DialectOAS30,
	"3.0.2": DialectOAS30,
	"3.0.3": DialectOAS30,
	"3.0.4": DialectOAS30,
	"3.1.0": DialectOAS31,
	"3.1.1": DialectOAS31,
	"3.1.2": DialectOAS31,
	"3.2.0": DialectOAS32,
}

// Parse parses an exact supported specification version.
func Parse(value string) (Version, error) {
	if dialect, ok := supportedVersions[value]; ok {
		return Version{raw: value, dialect: dialect}, nil
	}
	if !isVersionSyntax(value) {
		return Version{}, fmt.Errorf("openapi: %w", ErrMalformedVersion)
	}
	return Version{}, fmt.Errorf("openapi: %w", ErrUnsupportedVersion)
}

// String returns the exact selected patch version.
func (version Version) String() string {
	return version.raw
}

// Dialect returns the governing version line.
func (version Version) Dialect() Dialect {
	return version.dialect
}

// IsLegacy reports whether this is the separated Swagger 2.0 dialect.
func (version Version) IsLegacy() bool {
	return version.dialect == DialectSwagger20
}

func isVersionSyntax(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	if len(parts) == 2 {
		return parts[0] == "1" || parts[0] == "2"
	}
	return true
}
