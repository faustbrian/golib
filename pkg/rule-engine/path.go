package ruleengine

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Path identifies a fact without reflection or model discovery.
type Path struct {
	segments []string
	key      string
}

// NewPath validates and copies explicit path segments.
func NewPath(limits Limits, segments ...string) (Path, error) {
	if err := limits.validate(); err != nil {
		return Path{}, err
	}
	if len(segments) == 0 || len(segments) > limits.MaxPathSegments {
		return Path{}, newError(CodeInvalidPath, "invalid segment count")
	}
	cloned := make([]string, len(segments))
	bytes := len(segments) - 1
	for index, segment := range segments {
		if segment == "" || strings.ContainsAny(segment, ".\x00/\\") ||
			!utf8.ValidString(segment) || containsControl(segment) {
			return Path{}, newError(CodeInvalidPath, "invalid segment")
		}
		bytes += len(segment)
		if bytes > limits.MaxPathBytes {
			return Path{}, newError(CodeInvalidPath, "path is too large")
		}
		cloned[index] = segment
	}
	return Path{segments: cloned, key: strings.Join(cloned, ".")}, nil
}

// MustPath creates a path with DefaultLimits and panics on programmer error.
func MustPath(segments ...string) Path {
	path, err := NewPath(DefaultLimits(), segments...)
	if err != nil {
		panic(err)
	}
	return path
}

// String returns the stable dotted representation.
func (p Path) String() string { return p.key }

// Segments returns a copy of the path segments.
func (p Path) Segments() []string { return append([]string(nil), p.segments...) }

func (p Path) valid() bool { return p.key != "" && len(p.segments) > 0 }

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
