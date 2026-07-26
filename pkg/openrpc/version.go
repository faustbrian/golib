// Package openrpc provides an ownership-safe OpenRPC document model and the
// core contracts shared by parsing, validation, resolution, and discovery.
package openrpc

import (
	"errors"
	"regexp"
)

// ErrUnsupportedVersion reports a malformed, unsupported, or future OpenRPC
// specification version. Callers must not infer semantics for rejected values.
var ErrUnsupportedVersion = errors.New("openrpc: unsupported specification version")

var supportedVersionPattern = regexp.MustCompile(`^1\.4\.(0|[1-9][0-9]*)$`)

// Version is a validated OpenRPC specification version.
type Version struct {
	value string
}

// ParseVersion validates that value belongs to an explicitly supported OpenRPC
// feature line. Patch releases share the 1.4 feature set as required by the
// specification's versioning rules.
func ParseVersion(value string) (Version, error) {
	if !supportedVersionPattern.MatchString(value) {
		return Version{}, ErrUnsupportedVersion
	}
	return Version{value: value}, nil
}

// String returns the exact validated semantic version.
func (version Version) String() string {
	return version.value
}

// FeatureSet returns the major.minor OpenRPC feature line.
func (version Version) FeatureSet() string {
	if version.value == "" {
		return ""
	}
	return "1.4"
}

// SupportedVersions returns the supported OpenRPC compatibility lines.
func SupportedVersions() []string {
	return []string{"1.4.x"}
}
