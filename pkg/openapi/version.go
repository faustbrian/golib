// Package openapi defines shared, version-aware OpenAPI document APIs.
package openapi

import "github.com/faustbrian/golib/pkg/openapi/specversion"

// ErrMalformedVersion reports a version string that is not syntactically an
// OpenAPI or Swagger version.
var ErrMalformedVersion = specversion.ErrMalformedVersion

// ErrUnsupportedVersion reports a syntactically valid version whose
// specification revision is not pinned and implemented by this package.
var ErrUnsupportedVersion = specversion.ErrUnsupportedVersion

// Dialect identifies a version line with distinct model and validation
// semantics.
type Dialect = specversion.Dialect

const (
	DialectSwagger20 = specversion.DialectSwagger20
	DialectOAS30     = specversion.DialectOAS30
	DialectOAS31     = specversion.DialectOAS31
	DialectOAS32     = specversion.DialectOAS32
)

// Version preserves the exact supported patch revision selected by a
// document or caller. Its zero value is invalid.
type Version = specversion.Version

// ParseVersion parses an exact supported OpenAPI or Swagger version. Future
// patch releases are rejected until their authoritative specification inputs
// have been pinned and reviewed.
func ParseVersion(value string) (Version, error) {
	return specversion.Parse(value)
}
