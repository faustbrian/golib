package jsonvalue_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestValuePreservesNumberSpellingAndObjectOrder(t *testing.T) {
	t.Parallel()

	number, err := jsonvalue.Number("-0.0e+00")
	if err != nil {
		t.Fatalf("Number() error = %v", err)
	}
	text, err := jsonvalue.String("<value>")
	if err != nil {
		t.Fatalf("String() error = %v", err)
	}
	value, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: "z", Value: number},
		{Name: "a", Value: text},
	})
	if err != nil {
		t.Fatalf("Object() error = %v", err)
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(raw), `{"z":-0.0e+00,"a":"\u003cvalue\u003e"}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}

	members, ok := value.Members()
	if !ok {
		t.Fatal("Members() ok = false")
	}
	members[0].Name = "changed"
	unchanged, _ := value.Members()
	if got, want := unchanged[0].Name, "z"; got != want {
		t.Fatalf("stored member name = %q, want %q", got, want)
	}
}

func TestValueKindsAccessorsAndCompositeOwnership(t *testing.T) {
	t.Parallel()

	text, err := jsonvalue.String("text")
	if err != nil {
		t.Fatal(err)
	}
	number, err := jsonvalue.Number("1.5")
	if err != nil {
		t.Fatal(err)
	}
	elements := []jsonvalue.Value{
		jsonvalue.Null(), jsonvalue.Boolean(false), jsonvalue.Boolean(true), number, text,
	}
	array, err := jsonvalue.Array(elements)
	if err != nil {
		t.Fatal(err)
	}
	elements[0] = text
	owned, ok := array.Elements()
	if !ok || owned[0].Kind() != jsonvalue.NullKind {
		t.Fatal("Array did not retain an owned element slice")
	}
	if length, ok := array.Length(); !ok || length != len(elements) {
		t.Fatalf("array Length() = %d, %t", length, ok)
	}
	owned[0] = text
	again, _ := array.Elements()
	if again[0].Kind() != jsonvalue.NullKind {
		t.Fatal("Elements exposed stored elements")
	}
	if _, ok := text.Elements(); ok {
		t.Fatal("Elements accepted a scalar")
	}
	if _, ok := text.Members(); ok {
		t.Fatal("Members accepted a scalar")
	}
	if length, ok := text.Length(); ok || length != 0 {
		t.Fatalf("scalar Length() = %d, %t", length, ok)
	}
	if _, ok := text.Lookup("missing"); ok {
		t.Fatal("Lookup accepted a scalar")
	}
	if value, ok := text.Text(); !ok || value != "text" {
		t.Fatalf("Text() = %q, %t", value, ok)
	}
	if value, ok := number.NumberText(); !ok || value != "1.5" {
		t.Fatalf("NumberText() = %q, %t", value, ok)
	}
	if value, ok := jsonvalue.Boolean(true).Bool(); !ok || !value {
		t.Fatalf("Bool() = %t, %t", value, ok)
	}
	if array.Raw().Kind() != jsonvalue.ArrayKind {
		t.Fatal("Raw changed the value")
	}
	raw, err := array.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `[null,false,true,1.5,"text"]` {
		t.Fatalf("array JSON = %s", raw)
	}
}

func TestValueRejectsZeroCompositeMembersAndZeroSerialization(t *testing.T) {
	t.Parallel()

	if _, err := jsonvalue.Array([]jsonvalue.Value{{}}); !errors.Is(err, jsonvalue.ErrInvalidValue) {
		t.Fatalf("Array zero element error = %v", err)
	}
	if _, err := jsonvalue.Object([]jsonvalue.Member{{Name: "zero"}}); !errors.Is(err, jsonvalue.ErrInvalidValue) {
		t.Fatalf("Object zero member error = %v", err)
	}
	if _, err := (jsonvalue.Value{}).MarshalJSON(); !errors.Is(err, jsonvalue.ErrInvalidValue) {
		t.Fatalf("zero MarshalJSON error = %v", err)
	}
	object, err := jsonvalue.Object([]jsonvalue.Member{{Name: "value", Value: jsonvalue.Null()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object.Lookup("missing"); ok {
		t.Fatal("Lookup found a missing member")
	}
	if value, ok := object.Lookup("value"); !ok || value.Kind() != jsonvalue.NullKind {
		t.Fatalf("Lookup value = %#v, %t", value, ok)
	}
	if length, ok := object.Length(); !ok || length != 1 {
		t.Fatalf("object Length() = %d, %t", length, ok)
	}
}

func TestValueRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	if _, err := jsonvalue.String(string([]byte{0xff})); err == nil {
		t.Fatal("String() invalid UTF-8 error = nil")
	}
	_, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: string([]byte{0xff}), Value: jsonvalue.Null()},
	})
	if err == nil {
		t.Fatal("Object() invalid UTF-8 member error = nil")
	}
}

func TestValueRejectsInvalidNumbersAndDuplicateMembers(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"", "+1", "01", ".1", "NaN", "1."} {
		if _, err := jsonvalue.Number(invalid); err == nil {
			t.Errorf("Number(%q) error = nil", invalid)
		}
	}

	_, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: "duplicate", Value: jsonvalue.Null()},
		{Name: "duplicate", Value: jsonvalue.Null()},
	})
	if err == nil {
		t.Fatal("Object() duplicate error = nil")
	}
}

func TestValueMarshalJSONEnforcesCallerLimits(t *testing.T) {
	t.Parallel()
	zero, err := jsonvalue.Number("0")
	if err != nil {
		t.Fatal(err)
	}
	minimum := jsonvalue.MarshalLimits{MaxBytes: 1, MaxDepth: 1, MaxNodes: 1}
	if raw, err := zero.MarshalJSONWithLimits(minimum); err != nil || string(raw) != "0" {
		t.Fatalf("minimum limits = %q, %v", raw, err)
	}

	for _, input := range []string{
		"", "<", `\"`, "\b\f\n\r\t", "\x00", "\u2028", "snowman ☃",
	} {
		text, err := jsonvalue.String(input)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		limits := jsonvalue.DefaultMarshalLimits()
		limits.MaxBytes = len(encoded) - 1
		if _, err := text.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
			t.Errorf("%q byte limit error = %v", input, err)
		}
		limits.MaxBytes++
		if raw, err := text.MarshalJSONWithLimits(limits); err != nil || string(raw) != string(encoded) {
			t.Errorf("%q exact byte limit = %q, %v", input, raw, err)
		}
	}

	array, err := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	if err != nil {
		t.Fatal(err)
	}
	limits := jsonvalue.DefaultMarshalLimits()
	limits.MaxDepth = 1
	if _, err := array.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
		t.Fatalf("depth limit error = %v", err)
	}
	limits = jsonvalue.DefaultMarshalLimits()
	limits.MaxNodes = 1
	if _, err := array.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
		t.Fatalf("node limit error = %v", err)
	}

	emptyArray, err := jsonvalue.Array(nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyObject, err := jsonvalue.Object(nil)
	if err != nil {
		t.Fatal(err)
	}
	object, err := jsonvalue.Object([]jsonvalue.Member{{Name: "a", Value: jsonvalue.Null()}})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := jsonvalue.Array([]jsonvalue.Value{array})
	if err != nil {
		t.Fatal(err)
	}
	emptyNameObject, err := jsonvalue.Object([]jsonvalue.Member{{Name: "", Value: jsonvalue.Null()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		value    jsonvalue.Value
		maxBytes int
	}{
		{name: "array below exact", value: array, maxBytes: len(`[null]`) - 1},
		{name: "object below exact", value: emptyNameObject, maxBytes: len(`{"":null}`) - 1},
	} {
		limits := jsonvalue.DefaultMarshalLimits()
		limits.MaxBytes = test.maxBytes
		if _, err := test.value.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
			t.Errorf("%s error = %v", test.name, err)
		}
		limits.MaxBytes++
		if raw, err := test.value.MarshalJSONWithLimits(limits); err != nil || len(raw) != limits.MaxBytes {
			t.Errorf("%s exact limit = %q, %v", test.name, raw, err)
		}
	}
	limits = jsonvalue.DefaultMarshalLimits()
	limits.MaxDepth = 2
	if _, err := object.MarshalJSONWithLimits(limits); err != nil {
		t.Fatalf("exact object depth error = %v", err)
	}
	objectArray, err := jsonvalue.Array([]jsonvalue.Value{object})
	if err != nil {
		t.Fatal(err)
	}
	limits.MaxDepth = 2
	if _, err := objectArray.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
		t.Fatalf("nested object depth error = %v", err)
	}
	limits.MaxDepth = 3
	if _, err := objectArray.MarshalJSONWithLimits(limits); err != nil {
		t.Fatalf("exact nested object depth error = %v", err)
	}
	for _, test := range []struct {
		name     string
		value    jsonvalue.Value
		maxBytes int
	}{
		{name: "empty array punctuation", value: emptyArray, maxBytes: 1},
		{name: "empty object punctuation", value: emptyObject, maxBytes: 1},
		{name: "nested container", value: outer, maxBytes: 2},
		{name: "array punctuation", value: array, maxBytes: 1},
		{name: "object punctuation", value: object, maxBytes: 2},
		{name: "object name", value: object, maxBytes: 3},
		{name: "object child", value: object, maxBytes: 6},
	} {
		limits := jsonvalue.DefaultMarshalLimits()
		limits.MaxBytes = test.maxBytes
		if _, err := test.value.MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}

	for _, limits := range []jsonvalue.MarshalLimits{
		{MaxBytes: 0, MaxDepth: 1, MaxNodes: 1},
		{MaxBytes: 1, MaxDepth: 0, MaxNodes: 1},
		{MaxBytes: 1, MaxDepth: 1, MaxNodes: 0},
	} {
		if _, err := jsonvalue.Null().MarshalJSONWithLimits(limits); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
			t.Errorf("invalid limits error = %v", err)
		}
	}
}

func TestValueMarshalJSONUsesBoundedDefaults(t *testing.T) {
	t.Parallel()

	value := jsonvalue.Null()
	for range jsonvalue.DefaultMarshalLimits().MaxDepth {
		var err error
		value, err = jsonvalue.Array([]jsonvalue.Value{value})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := value.MarshalJSON(); !errors.Is(err, jsonvalue.ErrMarshalLimit) {
		t.Fatalf("default depth limit error = %v", err)
	}
}
