package modelaccess

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestTypedFieldsPreservePresenceNullAndInvalidRepresentations(t *testing.T) {
	t.Parallel()

	text, _ := jsonvalue.String("")
	wrong, _ := jsonvalue.String("not a boolean")
	object, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: "empty", Value: text},
		{Name: "null", Value: jsonvalue.Null()},
		{Name: "wrong", Value: wrong},
	})
	if err != nil {
		t.Fatal(err)
	}

	if value, ok := String(object, "empty").Value(); !ok || value != "" {
		t.Fatalf("String(empty) = %q, %t", value, ok)
	}
	if field := String(object, "missing"); field.Present() {
		t.Fatal("String(missing) present = true")
	}
	if field := String(object, "null"); !field.Present() || !field.Null() {
		t.Fatal("String(null) did not preserve null")
	}
	if field := Boolean(object, "wrong"); field.Valid() {
		t.Fatal("Boolean(wrong) valid = true")
	}
}

func TestTypedCollectionsAndExtensionsRemainOrdered(t *testing.T) {
	t.Parallel()

	one, _ := jsonvalue.String("one")
	two, _ := jsonvalue.String("two")
	array, _ := jsonvalue.Array([]jsonvalue.Value{one, two})
	nested, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "b", Value: one}, {Name: "a", Value: two}})
	object, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "items", Value: array},
		{Name: "map", Value: nested},
		{Name: "x-first", Value: one},
		{Name: "known", Value: two},
		{Name: "x-second", Value: two},
	})

	items, ok := List(object, "items", stringValue).Value()
	if !ok || items.Len() != 2 {
		t.Fatalf("List(items) ok=%t len=%d", ok, items.Len())
	}
	values, ok := Map(object, "map", stringValue).Value()
	if !ok || values.Len() != 2 || values.Names()[0] != "b" {
		t.Fatalf("Map(map) ok=%t names=%v", ok, values.Names())
	}
	extensions := Extensions(object)
	if got := extensions.Names(); len(got) != 2 || got[0] != "x-first" || got[1] != "x-second" {
		t.Fatalf("Extensions() names = %v", got)
	}
}

func TestPatternEntriesExcludeFixedAndExtensionFields(t *testing.T) {
	t.Parallel()

	text, _ := jsonvalue.String("value")
	wrong := jsonvalue.Boolean(false)
	object, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "default", Value: text},
		{Name: "200", Value: text},
		{Name: "2XX", Value: wrong},
		{Name: "null", Value: jsonvalue.Null()},
		{Name: "x-extra", Value: text},
	})

	entries := PatternEntries(object, []string{"default"}, func(name string) bool {
		return name != "" && name[0] != 'x'
	}, stringValue)
	if got := entries.Names(); len(got) != 3 || got[0] != "200" || got[1] != "2XX" || got[2] != "null" {
		t.Fatalf("PatternEntries() names = %v", got)
	}
	valid, _ := entries.Lookup("200")
	if value, ok := valid.Value(); !ok || value != "value" {
		t.Fatalf("valid pattern value = %q, %t", value, ok)
	}
	invalid, _ := entries.Lookup("2XX")
	if invalid.Valid() {
		t.Fatal("invalid pattern value valid = true")
	}
	null, _ := entries.Lookup("null")
	if !null.Null() {
		t.Fatal("null pattern value did not preserve null")
	}
}

func TestRawAndCollectionConvertersPreserveInvalidInput(t *testing.T) {
	t.Parallel()

	text, _ := jsonvalue.String("value")
	invalid := jsonvalue.Boolean(false)
	array, _ := jsonvalue.Array([]jsonvalue.Value{text, invalid})
	objectMap, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "valid", Value: text},
		{Name: "invalid", Value: invalid},
	})
	object, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "null", Value: jsonvalue.Null()},
		{Name: "raw", Value: invalid},
	})

	if field := Raw(object, "missing"); field.Present() {
		t.Fatal("missing raw field is present")
	}
	if field := Raw(object, "null"); !field.Null() {
		t.Fatal("null raw field did not preserve null")
	}
	if value, ok := Raw(object, "raw").Value(); !ok || value.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("raw field = %#v, %t", value, ok)
	}
	if _, ok := ListValue(text, stringValue); ok {
		t.Fatal("ListValue accepted a scalar")
	}
	if _, ok := ListValue(array, stringValue); ok {
		t.Fatal("ListValue accepted an invalid element")
	}
	if _, ok := MapValue(text, stringValue); ok {
		t.Fatal("MapValue accepted a scalar")
	}
	if _, ok := MapValue(objectMap, stringValue); ok {
		t.Fatal("MapValue accepted an invalid member")
	}
	if extensions := Extensions(text); extensions.Len() != 0 {
		t.Fatalf("scalar extensions = %#v", extensions.Entries())
	}
	if entries := PatternEntries(text, nil, func(string) bool { return true }, stringValue); entries.Len() != 0 {
		t.Fatalf("scalar patterns = %#v", entries.Entries())
	}
}

func stringValue(value jsonvalue.Value) (string, bool) {
	return value.Text()
}
