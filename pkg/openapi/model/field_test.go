package model_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/model"
)

func TestFieldDistinguishesAbsentNullZeroAndEmpty(t *testing.T) {
	t.Parallel()

	absent := model.Absent[string]()
	if absent.Present() || absent.Null() {
		t.Fatalf("Absent() present=%t null=%t", absent.Present(), absent.Null())
	}
	if _, ok := absent.Value(); ok {
		t.Fatal("Absent().Value() ok = true")
	}

	null := model.Null[string]()
	if !null.Present() || !null.Null() {
		t.Fatalf("Null() present=%t null=%t", null.Present(), null.Null())
	}
	if _, ok := null.Value(); ok {
		t.Fatal("Null().Value() ok = true")
	}

	zero := model.Present(0)
	if value, ok := zero.Value(); !ok || value != 0 || zero.Null() {
		t.Fatalf("Present(0).Value() = %d, %t", value, ok)
	}

	empty := model.Present(model.NewList([]string{}))
	value, ok := empty.Value()
	if !ok || value.Len() != 0 {
		t.Fatalf("empty list field ok=%t len=%d", ok, value.Len())
	}
}

func TestFieldPreservesAnInvalidTypedRepresentation(t *testing.T) {
	t.Parallel()

	raw, err := jsonvalue.String("not a boolean")
	if err != nil {
		t.Fatal(err)
	}
	field := model.Invalid[bool](raw)
	if !field.Present() || field.Null() || field.Valid() {
		t.Fatalf(
			"Invalid() present=%t null=%t valid=%t",
			field.Present(),
			field.Null(),
			field.Valid(),
		)
	}
	if _, ok := field.Value(); ok {
		t.Fatal("Invalid().Value() ok = true")
	}
	got, ok := field.Raw()
	if !ok || got.Kind() != jsonvalue.StringKind {
		t.Fatalf("Invalid().Raw() kind=%v ok=%t", got.Kind(), ok)
	}
}

func TestListAndMapOwnCallerCollections(t *testing.T) {
	t.Parallel()

	values := []string{"first", "second"}
	list := model.NewList(values)
	values[0] = "changed"
	if got, _ := list.At(0); got != "first" {
		t.Fatalf("List.At(0) = %q", got)
	}
	copyValues := list.Values()
	copyValues[0] = "changed again"
	if got, _ := list.At(0); got != "first" {
		t.Fatalf("List storage changed to %q", got)
	}

	entries := []model.Entry[string]{{Name: "z", Value: "last"}, {Name: "a", Value: "first"}}
	ordered, err := model.NewMap(entries)
	if err != nil {
		t.Fatalf("NewMap() error = %v", err)
	}
	entries[0].Name = "changed"
	if got := ordered.Names(); len(got) != 2 || got[0] != "z" || got[1] != "a" {
		t.Fatalf("Map.Names() = %#v", got)
	}
	if got, ok := ordered.Lookup("a"); !ok || got != "first" {
		t.Fatalf("Map.Lookup(a) = %q, %t", got, ok)
	}
	if _, err := model.NewMap([]model.Entry[int]{{Name: "duplicate"}, {Name: "duplicate"}}); err == nil {
		t.Fatal("NewMap() duplicate error = nil")
	}
}
