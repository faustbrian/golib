package apiquery

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTypedValueCanonicalMatrix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 12, 34, 56, 123, time.FixedZone("offset", 3600))
	tests := []struct {
		value Value
		kind  ValueType
		text  string
	}{
		{StringValue("hello"), TypeString, "hello"},
		{IntValue(-42), TypeInt, "-42"},
		{UintValue(42), TypeUint, "42"},
		{FloatValue(1.25), TypeFloat, "1.25"},
		{BoolValue(true), TypeBool, "true"},
		{TimeValue(now), TypeTime, "2026-07-16T11:34:56.000000123Z"},
		{BytesValue([]byte{0, 1, 2}), TypeBytes, "AAEC"},
		{NullValue(), TypeNull, ""},
	}
	for _, test := range tests {
		if test.value.Type() != test.kind || test.value.String() != test.text {
			t.Fatalf("value = %#v, want %s %q", test.value, test.kind, test.text)
		}
		data, err := json.Marshal(test.value)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", test.kind, err)
		}
		var decoded Value
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", test.kind, err)
		}
		if decoded != test.value {
			t.Fatalf("roundtrip = %#v, want %#v", decoded, test.value)
		}
	}
	if FloatValue(math.NaN()).Type() != "" || FloatValue(math.Inf(-1)).Type() != "" {
		t.Fatal("non-finite float produced a typed value")
	}
	optional := Present("value")
	if !optional.IsPresent() {
		t.Fatal("Present value reports absent")
	}
}

func TestTypedValueRejectsNonCanonicalJSON(t *testing.T) {
	t.Parallel()

	invalid := []string{
		`{`, `{"type":"unknown","value":"x"}`,
		`{"type":"int","value":"01"}`, `{"type":"uint","value":"-1"}`,
		`{"type":"float","value":"NaN"}`, `{"type":"float","value":"1.0"}`,
		`{"type":"bool","value":"TRUE"}`, `{"type":"time","value":"bad"}`,
		`{"type":"time","value":"2026-07-16T12:00:00+01:00"}`,
		`{"type":"bytes","value":"***"}`, `{"type":"bytes","value":"AA=="}`,
	}
	for _, data := range invalid {
		var value Value
		if err := json.Unmarshal([]byte(data), &value); err == nil {
			t.Fatalf("Unmarshal(%s) accepted non-canonical value", data)
		}
	}
	if !canonicalValue(TypeString, "anything") || canonicalValue(ValueType("bad"), "x") ||
		canonicalValue(TypeNull, "not-empty") {
		t.Fatal("canonicalValue closed type contract failed")
	}
	var direct Value
	if err := direct.UnmarshalJSON([]byte("{")); err == nil {
		t.Fatal("Value.UnmarshalJSON accepted malformed JSON")
	}
}

func TestSchemaDeclarationFailureMatrix(t *testing.T) {
	t.Parallel()

	base := func() SchemaConfig {
		return SchemaConfig{Resource: "orders", Revision: "v1",
			Fields: []FieldDefinition{{Name: "id", Type: TypeString}}}
	}
	tests := []SchemaConfig{
		{Resource: "", Revision: "v1"}, {Resource: "orders", Revision: ""},
		{Resource: "orders", Revision: "v1", Fields: []FieldDefinition{{Name: "Bad", Type: TypeString}}},
		{Resource: "orders", Revision: "v1", Fields: []FieldDefinition{{Name: "id", Type: ValueType("bad")}}},
		{Resource: "orders", Revision: "v1", Fields: []FieldDefinition{{Name: "id", Type: TypeString}, {Name: "id", Type: TypeString}}},
	}
	config := base()
	config.Filters = []FilterDefinition{{Name: "f", Type: TypeString}}
	tests = append(tests, config)
	config = base()
	config.Filters = []FilterDefinition{{Name: "f", Type: TypeString, Operators: []Operator{OpEqual}}, {Name: "f", Type: TypeString, Operators: []Operator{OpEqual}}}
	tests = append(tests, config)
	config = base()
	config.Filters = []FilterDefinition{{Name: "f", Type: TypeString, Operators: []Operator{OpEqual, OpEqual}}}
	tests = append(tests, config)
	config = base()
	config.Filters = []FilterDefinition{{Name: "f", Type: TypeString, Operators: []Operator{OpEqual}, Cost: -1}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "Bad", Type: TypeString}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, Nulls: NullOrder("middle")}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}, {Name: "id", Type: TypeString}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, Cost: -1}}
	tests = append(tests, config)
	config = base()
	config.Relationships = []RelationshipDefinition{{Name: "Bad", Resource: "customers"}}
	tests = append(tests, config)
	config = base()
	config.Relationships = []RelationshipDefinition{{Name: "customer", Resource: "customers"}, {Name: "customer", Resource: "people"}}
	tests = append(tests, config)
	config = base()
	config.Relationships = []RelationshipDefinition{{Name: "customer", Resource: "customers", Cost: -1}}
	tests = append(tests, config)
	config = base()
	config.Pagination = PaginationDefinition{Offset: true, DefaultPageSize: 1, MaxOffset: -1}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "a", Type: TypeString, TieBreaker: true}, {Name: "b", Type: TypeString, TieBreaker: true}}
	config.Pagination = PaginationDefinition{Cursor: true, DefaultPageSize: 1}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "missing", Direction: Ascending}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Direction("sideways")}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending}, {Name: "id", Direction: Ascending}}
	tests = append(tests, config)
	config = base()
	config.AllowedLogic = []Logic{LogicAnd, LogicAnd}
	tests = append(tests, config)
	config = base()
	config.AllowedLogic = []Logic{Logic("raw")}
	tests = append(tests, config)
	config = base()
	config.Bounds.MaxFields = -1
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, Nulls: NullsLast}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsFirst}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullOrder("raw")}}
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}, {Name: "other", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending}, {Name: "other", Direction: Ascending}}
	config.Bounds.MaxSorts = 1
	tests = append(tests, config)
	config = base()
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, TieBreaker: true},
		{Name: "other", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "other", Direction: Ascending}}
	config.Pagination = PaginationDefinition{Cursor: true, DefaultPageSize: 1}
	config.Bounds.MaxSorts = 1
	tests = append(tests, config)
	for index, candidate := range tests {
		if _, err := NewSchema(candidate); err == nil {
			t.Fatalf("NewSchema(case %d) accepted invalid declaration", index)
		}
	}
}

func TestSchemaHelperClosedSets(t *testing.T) {
	t.Parallel()

	if validName("") || validName("Upper") || validName("1bad") || validName("bad-name") || validName(string([]byte{0xff})) {
		t.Fatal("validName accepted invalid name")
	}
	if !validName("a_1") {
		t.Fatal("validName rejected valid name")
	}
	for _, kind := range []ValueType{TypeString, TypeInt, TypeUint, TypeFloat, TypeBool, TypeTime, TypeBytes} {
		if !validType(kind) {
			t.Fatalf("validType(%s) rejected", kind)
		}
	}
	if validType("bad") {
		t.Fatal("validType accepted unknown type")
	}
	if validType(TypeNull) || !validValueType(TypeNull) || validValueType("bad") {
		t.Fatal("null leaked into schema types or was rejected as a value type")
	}
	if operatorSupportsType(OpContains, TypeInt, false) || operatorSupportsType(OpIsNull, TypeString, false) ||
		operatorSupportsType(OpLess, TypeBool, false) || operatorSupportsType(OpLess, TypeBytes, false) ||
		operatorSupportsType(Operator("raw"), TypeString, true) {
		t.Fatal("operatorSupportsType accepted invalid combination")
	}
	if !operatorSupportsType(OpEqual, TypeBool, false) || !operatorSupportsType(OpBetween, TypeTime, false) ||
		!operatorSupportsType(OpIsNull, TypeString, true) || !operatorSupportsType(OpStartsWith, TypeString, false) {
		t.Fatal("operatorSupportsType rejected valid combination")
	}
}

func TestSchemaExposesNormalizedTransportBounds(t *testing.T) {
	t.Parallel()

	schema, err := NewSchema(SchemaConfig{Resource: "records", Revision: "v1",
		Fields:      []FieldDefinition{{Name: "id", Type: TypeString}},
		Sorts:       []SortDefinition{{Name: "id", Type: TypeString, TieBreaker: true}},
		DefaultSort: []SortTerm{{Name: "id", Direction: Ascending}},
		Pagination:  PaginationDefinition{Cursor: true, DefaultPageSize: 10},
		Bounds:      Bounds{MaxRequestBytes: 1234}})
	if err != nil {
		t.Fatal(err)
	}
	if schema.Bounds().MaxRequestBytes != 1234 || schema.Bounds().MaxFields <= 0 {
		t.Fatalf("Bounds() = %#v", schema.Bounds())
	}
}

func TestViolationAndPlanDefensivePaths(t *testing.T) {
	t.Parallel()

	var nilViolations *Violations
	if nilViolations.Error() != "query validation failed" || nilViolations.Items() != nil {
		t.Fatal("nil violations contract failed")
	}
	collector := violationCollector{limit: 1}
	collector.add(CodeConflict, "one", "first")
	collector.add(CodeConflict, "two", "second")
	var err *Violations
	if !errors.As(collector.err(), &err) {
		t.Fatal("collector did not return violations")
	}
	if len(err.Items()) != 1 || !strings.Contains(err.Error(), "one: first") {
		t.Fatalf("violations = %v", err)
	}

	schema, schemaErr := NewSchema(SchemaConfig{Resource: "orders", Revision: "v1",
		Fields: []FieldDefinition{{Name: "id", Type: TypeString}}, Bounds: Bounds{MaxCanonicalBytes: 1}})
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	plan, compileErr := Compile(t.Context(), schema, Request{}, CompileOptions{})
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	if plan.Resource() != "orders" || plan.SchemaRevision() != "v1" || plan.Page().Mode != PageNone {
		t.Fatalf("plan accessors failed: %#v", plan)
	}
	if plan.Cursor() != nil {
		t.Fatal("non-cursor plan returned cursor state")
	}
	if _, canonicalErr := plan.Canonical(); canonicalErr == nil {
		t.Fatal("Canonical accepted size overflow")
	}
	if cloneFilter(nil) != nil {
		t.Fatal("cloneFilter(nil) was non-nil")
	}
}
