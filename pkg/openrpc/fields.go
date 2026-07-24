package openrpc

import (
	"errors"
	"sort"
	"strings"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrInvalidExtensionName reports a specification extension without the
	// required case-sensitive x- prefix.
	ErrInvalidExtensionName = errors.New("openrpc: invalid extension name")
	// ErrDuplicateField reports repeated field input to an ownership-safe
	// constructor.
	ErrDuplicateField = errors.New("openrpc: duplicate field")
	// ErrInvalidField reports an empty name or invalid zero JSON value.
	ErrInvalidField = errors.New("openrpc: invalid field")
)

// Field is one named, arbitrary JSON value supplied to a field constructor.
type Field struct {
	Name  string
	Value jsonvalue.Value
}

// Fields is an immutable, deterministically ordered collection of arbitrary
// JSON fields.
type Fields struct {
	names  []string
	values map[string]jsonvalue.Value
}

// NewExtensions constructs specification extension fields. Names must begin
// with the case-sensitive x- prefix required by OpenRPC.
func NewExtensions(fields ...Field) (Fields, error) {
	return newFields(fields, true)
}

// NewUnknownFields constructs preserved standard-looking fields for explicit
// preserving parser mode.
func NewUnknownFields(fields ...Field) (Fields, error) {
	return newFields(fields, false)
}

func newFields(fields []Field, extensions bool) (Fields, error) {
	values := make(map[string]jsonvalue.Value, len(fields))
	for _, field := range fields {
		if field.Name == "" || len(field.Value.Bytes()) == 0 {
			return Fields{}, ErrInvalidField
		}
		if extensions && !strings.HasPrefix(field.Name, "x-") {
			return Fields{}, ErrInvalidExtensionName
		}
		if _, exists := values[field.Name]; exists {
			return Fields{}, ErrDuplicateField
		}
		values[field.Name] = field.Value
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return Fields{names: names, values: values}, nil
}

// Len returns the number of fields.
func (fields Fields) Len() int {
	return len(fields.names)
}

// Names returns an owned, lexically sorted name snapshot.
func (fields Fields) Names() []string {
	return append([]string(nil), fields.names...)
}

// Get returns an immutable value by its exact case-sensitive name.
func (fields Fields) Get(name string) (jsonvalue.Value, bool) {
	value, ok := fields.values[name]
	return value, ok
}
