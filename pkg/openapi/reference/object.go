package reference

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// Object is an immutable, syntactically valid OpenAPI Reference Object. It
// retains all sibling fields so version-specific consumers can apply their
// own sibling policy without data loss.
type Object struct {
	raw          jsonvalue.Value
	rawReference string
}

// ParseObject requires an object with a string $ref containing a URI
// reference. It performs no resolution or I/O.
func ParseObject(value jsonvalue.Value) (Object, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return Object{}, ErrInvalidReference
	}
	referenceValue, exists := value.Lookup("$ref")
	if !exists {
		return Object{}, ErrInvalidReference
	}
	rawReference, valid := referenceValue.Text()
	if !valid || strings.IndexFunc(rawReference, unicode.IsControl) >= 0 {
		return Object{}, ErrInvalidReference
	}
	if _, err := url.Parse(rawReference); err != nil {
		return Object{}, ErrInvalidReference
	}
	return Object{raw: value, rawReference: rawReference}, nil
}

// Raw returns the complete immutable Reference Object.
func (object Object) Raw() jsonvalue.Value { return object.raw }

// RawReference returns the exact $ref URI-reference string.
func (object Object) RawReference() string { return object.rawReference }
