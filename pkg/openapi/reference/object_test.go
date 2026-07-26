package reference_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestParseObjectRequiresAndPreservesReferenceString(t *testing.T) {
	t.Parallel()

	raw := referenceObjectValue(t,
		jsonvalue.Member{Name: "$ref", Value: referenceStringValue(t, "../models.yaml#/Pet")},
		jsonvalue.Member{Name: "summary", Value: referenceStringValue(t, "Pet")},
	)
	object, err := reference.ParseObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if object.RawReference() != "../models.yaml#/Pet" ||
		object.Raw().Kind() != jsonvalue.ObjectKind {
		t.Fatalf("reference object = %#v", object)
	}
	if _, exists := object.Raw().Lookup("summary"); !exists {
		t.Fatal("ParseObject discarded a sibling field")
	}
}

func TestParseObjectRejectsMissingNonStringAndInvalidReferences(t *testing.T) {
	t.Parallel()

	for _, value := range []jsonvalue.Value{
		jsonvalue.Null(),
		referenceObjectValue(t),
		referenceObjectValue(t,
			jsonvalue.Member{Name: "$ref", Value: jsonvalue.Boolean(true)}),
		referenceObjectValue(t,
			jsonvalue.Member{Name: "$ref", Value: referenceStringValue(t, "bad\nreference")}),
		referenceObjectValue(t,
			jsonvalue.Member{Name: "$ref", Value: referenceStringValue(t, "\nreference")}),
		referenceObjectValue(t,
			jsonvalue.Member{Name: "$ref", Value: referenceStringValue(t, "\u0085reference")}),
		referenceObjectValue(t,
			jsonvalue.Member{Name: "$ref", Value: referenceStringValue(t, "bad%zz")}),
	} {
		if _, err := reference.ParseObject(value); !errors.Is(
			err, reference.ErrInvalidReference,
		) {
			t.Fatalf("ParseObject() error = %v", err)
		}
	}
}

func referenceObjectValue(
	t *testing.T,
	members ...jsonvalue.Member,
) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func referenceStringValue(t *testing.T, value string) jsonvalue.Value {
	t.Helper()
	result, err := jsonvalue.String(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
