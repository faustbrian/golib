package apiquery

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestViolationCollectorHonorsExactAndUnlimitedBounds(t *testing.T) {
	t.Parallel()

	bounded := violationCollector{limit: 2}
	bounded.add(CodeConflict, "first", "first")
	bounded.add(CodeUnsupported, "second", "second")
	bounded.add(CodeInvalidElement, "third", "third")
	if len(bounded.items) != 2 || bounded.items[0].Path != "first" || bounded.items[1].Path != "second" {
		t.Fatalf("bounded violations = %#v", bounded.items)
	}

	unlimited := violationCollector{}
	unlimited.add(CodeConflict, "first", "first")
	if len(unlimited.items) != 1 {
		t.Fatalf("unlimited violations = %#v", unlimited.items)
	}
}

func TestCanonicalPlanAcceptsItsExactEncodedLimit(t *testing.T) {
	t.Parallel()

	plan := &Plan{resource: "records", revision: "v1", page: PageRequest{Mode: PageNone}, maxCanonical: math.MaxInt}
	encoded, err := plan.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	plan.maxCanonical = len(encoded)
	if exact, exactErr := plan.Canonical(); exactErr != nil || string(exact) != string(encoded) {
		t.Fatalf("exact canonical limit = %q, error = %v", exact, exactErr)
	}
	plan.maxCanonical--
	if _, overflowErr := plan.Canonical(); !errors.As(overflowErr, new(*Violations)) {
		t.Fatalf("canonical overflow error = %v", overflowErr)
	}

	page := PageRequest{Mode: PageCursor, Size: 2}
	if canonical := canonicalizePage(page, nil); canonical.CursorDigest != "" || canonical.Mode != PageCursor {
		t.Fatalf("nil cursor canonical page = %#v", canonical)
	}
	cursor := &CursorState{Direction: CursorForward, Positions: []Value{StringValue("a")}}
	if canonical := canonicalizePage(page, cursor); canonical.CursorDigest == "" {
		t.Fatalf("cursor canonical page = %#v", canonical)
	}
}

func TestCanonicalValueRequiresSuccessfulCanonicalRoundTrips(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kind  ValueType
		value string
		want  bool
	}{
		{TypeUint, "0", true}, {TypeUint, "01", false}, {TypeUint, "bad", false},
		{TypeFloat, "1", true}, {TypeFloat, "1.0", false}, {TypeFloat, "NaN", false}, {TypeFloat, "bad", false},
		{TypeBytes, "AA", true}, {TypeBytes, "AB", false}, {TypeBytes, "***", false},
	} {
		if got := canonicalValue(test.kind, test.value); got != test.want {
			t.Fatalf("canonicalValue(%q, %q) = %v, want %v", test.kind, test.value, got, test.want)
		}
	}
}

func TestCostAccountingAcceptsExactLimitsAndSaturatesOverflow(t *testing.T) {
	t.Parallel()

	schema := &Schema{bounds: Bounds{MaxCost: 10}}
	plan := &Plan{cost: 7}
	addPlanCost(schema, plan, 3)
	if plan.cost != 10 || plan.costExceeded {
		t.Fatalf("exact plan cost = %d, exceeded = %v", plan.cost, plan.costExceeded)
	}
	addPlanCost(schema, plan, 1)
	if plan.cost != 10 || !plan.costExceeded {
		t.Fatalf("overflow plan cost = %d, exceeded = %v", plan.cost, plan.costExceeded)
	}
	addPlanCost(schema, plan, 0)
	if plan.cost != 10 || !plan.costExceeded {
		t.Fatalf("pre-exceeded plan cost = %d, exceeded = %v", plan.cost, plan.costExceeded)
	}

	filterSchema := &Schema{
		filters:     []FilterDefinition{{Name: "one", Cost: 1}, {Name: "two", Cost: 2}},
		filterIndex: map[string]int{"one": 0, "two": 1},
	}
	filter := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
		{Predicate: &Predicate{Name: "one"}},
		{Predicate: &Predicate{Name: "two"}},
	}}
	if got := filterCost(filterSchema, filter); got != 3 {
		t.Fatalf("filter cost = %d", got)
	}
	filterSchema.filters[0].Cost = math.MaxInt
	if got := filterCost(filterSchema, filter); got != math.MaxInt {
		t.Fatalf("overflow filter cost = %d", got)
	}
}

func TestCompileAcceptsEveryExactRequestBoundary(t *testing.T) {
	t.Parallel()

	schema := compileMatrixSchema(t)
	request := Request{
		Fields:   Present([]string{"id", "status"}),
		Includes: Present([]string{"customer"}),
		Filter:   testLeaf("value", OpEqual, StringValue("abc")),
		Sorts:    Present([]SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsLast}}),
		Page:     PageRequest{Mode: PageOffset, Size: 3, Offset: 3},
	}
	options := CompileOptions{MandatoryConstraints: []Constraint{
		{Name: "one", Value: StringValue("abc")},
		{Name: "two", Value: StringValue("abc")},
	}}
	plan, err := Compile(context.Background(), schema, request, options)
	if err != nil {
		t.Fatalf("Compile(exact boundaries) error = %v", err)
	}
	if len(plan.ResponseFields()) != 2 || len(plan.Includes()) != 1 || len(plan.Sorts()) != 1 ||
		plan.Page().Size != 3 || plan.Page().Offset != 3 || len(plan.MandatoryConstraints()) != 2 {
		t.Fatalf("exact-boundary plan = %#v", plan)
	}
}

func TestSchemaAcceptsExactDeclarationBoundaries(t *testing.T) {
	t.Parallel()

	config := SchemaConfig{
		Resource: "records", Revision: "v1",
		Fields:        []FieldDefinition{{Name: "id", Type: TypeString, Cost: 3}},
		Filters:       []FilterDefinition{{Name: "id", Type: TypeString, Operators: []Operator{OpEqual}, Cost: 3}},
		Sorts:         []SortDefinition{{Name: "id", Type: TypeString, Cost: 3, TieBreaker: true, Nulls: NullsLast}},
		Relationships: []RelationshipDefinition{{Name: "child", Resource: "children", Cost: 3}},
		DefaultSort:   []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsLast}},
		AllowedLogic:  []Logic{LogicAnd, LogicOr, LogicNot},
		Pagination:    PaginationDefinition{Cursor: true, Offset: true, DefaultPageSize: 3, MaxOffset: 0},
		Bounds:        Bounds{MaxSorts: 1, MaxPageSize: 3, MaxCost: 3},
	}
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatalf("NewSchema(exact boundaries) error = %v", err)
	}
	if schema.bounds.MaxRequestBytes != 16<<10 || len(schema.allowedLogic) != 3 {
		t.Fatalf("normalized schema = %#v", schema)
	}

	for _, name := range []string{"a", "z", "a0", "a9", "a_"} {
		if !validName(name) {
			t.Fatalf("validName(%q) = false", name)
		}
	}
	for _, name := range []string{"`", "{", "a/", "a:", "0a", "_a"} {
		if validName(name) {
			t.Fatalf("validName(%q) = true", name)
		}
	}
}
