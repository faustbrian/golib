package apiquery

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestCompileRejectsCapabilityAndBoundMatrix(t *testing.T) {
	t.Parallel()

	if _, err := Compile(context.Background(), nil, Request{}, CompileOptions{}); err == nil {
		t.Fatal("Compile accepted nil schema")
	}
	schema := compileMatrixSchema(t)
	deny := func(kind CapabilityKind, name string) CompileOptions {
		return CompileOptions{Authorize: func(context.Context, Capability) bool { return false }}
	}
	tests := []struct {
		name    string
		request Request
		options CompileOptions
	}{
		{"invalid constraint", Request{}, CompileOptions{MandatoryConstraints: []Constraint{{Name: "Bad", Value: Value{}}}}},
		{"duplicate constraint", Request{}, CompileOptions{MandatoryConstraints: []Constraint{{Name: "tenant", Value: StringValue("a")}, {Name: "tenant", Value: StringValue("b")}}}},
		{"long constraint", Request{}, CompileOptions{MandatoryConstraints: []Constraint{{Name: "tenant", Value: StringValue("long")}}}},
		{"excess fields", Request{Fields: Present([]string{"id", "status", "extra"})}, CompileOptions{}},
		{"deprecated field", Request{Fields: Present([]string{"legacy"})}, CompileOptions{}},
		{"unauthorized field", Request{Fields: Present([]string{"id"})}, deny(CapabilityField, "id")},
		{"excess includes", Request{Includes: Present([]string{"customer", "customer.address", "customer"})}, CompileOptions{}},
		{"unknown include", Request{Includes: Present([]string{"missing"})}, CompileOptions{}},
		{"deep include", Request{Includes: Present([]string{"customer.address"})}, CompileOptions{}},
		{"duplicate include", Request{Includes: Present([]string{"customer", "customer"})}, CompileOptions{}},
		{"unauthorized include", Request{Includes: Present([]string{"customer"})}, deny(CapabilityRelationship, "customer")},
		{"empty filter", Request{Filter: &FilterExpr{}}, CompileOptions{}},
		{"mixed filter", Request{Filter: &FilterExpr{Predicate: &Predicate{Name: "value", Operator: OpEqual, Values: []Value{StringValue("a")}}, Logic: LogicAnd, Children: []FilterExpr{{}}}}, CompileOptions{}},
		{"unknown filter", Request{Filter: testLeaf("missing", OpEqual, StringValue("a"))}, CompileOptions{}},
		{"unauthorized filter", Request{Filter: testLeaf("value", OpEqual, StringValue("a"))}, deny(CapabilityFilter, "value")},
		{"unsupported filter", Request{Filter: testLeaf("value", Operator("raw"), StringValue("a"))}, CompileOptions{}},
		{"unsupported logic", Request{Filter: &FilterExpr{Logic: LogicOr, Children: []FilterExpr{*testLeaf("value", OpEqual, StringValue("a"))}}}, CompileOptions{}},
		{"filter value excess", Request{Filter: testLeaf("value", OpIn, StringValue("a"), StringValue("b"), StringValue("c"))}, CompileOptions{}},
		{"filter depth", Request{Filter: &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{{Logic: LogicAnd, Children: []FilterExpr{*testLeaf("value", OpEqual, StringValue("a"))}}}}}, CompileOptions{}},
		{"filter node count", Request{Filter: &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{*testLeaf("value", OpEqual, StringValue("a")), *testLeaf("other", OpEqual, StringValue("b"))}}}, CompileOptions{}},
		{"not arity", Request{Filter: &FilterExpr{Logic: LogicNot, Children: []FilterExpr{*testLeaf("value", OpEqual, StringValue("a")), *testLeaf("other", OpEqual, StringValue("b"))}}}, CompileOptions{}},
		{"value count", Request{Filter: testLeaf("value", OpEqual)}, CompileOptions{}},
		{"value type", Request{Filter: testLeaf("value", OpEqual, IntValue(1))}, CompileOptions{}},
		{"value size", Request{Filter: testLeaf("value", OpEqual, StringValue("long"))}, CompileOptions{}},
		{"empty value", Request{Filter: testLeaf("value", OpEqual, StringValue(""))}, CompileOptions{}},
		{"unknown sort", Request{Sorts: Present([]SortTerm{{Name: "missing", Direction: Ascending}})}, CompileOptions{}},
		{"duplicate sort", Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Ascending}, {Name: "id", Direction: Ascending}})}, CompileOptions{}},
		{"invalid sort direction", Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Direction("sideways")}})}, CompileOptions{}},
		{"unauthorized sort", Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Ascending}})}, deny(CapabilitySort, "id")},
		{"unsupported null order", Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsFirst}})}, CompileOptions{}},
		{"sort excess", Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Ascending}, {Name: "status", Direction: Ascending}})}, CompileOptions{}},
		{"cursor before excess", Request{Page: PageRequest{Mode: PageCursor, Before: "long"}}, CompileOptions{}},
		{"cursor conflict", Request{Page: PageRequest{Mode: PageCursor, After: "a", Before: "b"}}, CompileOptions{}},
		{"unknown page mode", Request{Page: PageRequest{Mode: PageMode("other")}}, CompileOptions{}},
		{"negative page size", Request{Page: PageRequest{Mode: PageOffset, Size: -1}}, CompileOptions{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(context.Background(), schema, test.request, test.options); err == nil {
				t.Fatal("Compile accepted invalid request")
			}
		})
	}
}

func TestCompileSuccessfulNestedAndCostPaths(t *testing.T) {
	t.Parallel()

	config := compileMatrixConfig()
	config.Bounds.MaxIncludes = 3
	config.Bounds.MaxIncludeDepth = 2
	config.Bounds.MaxFilterDepth = 3
	config.Bounds.MaxFilterNodes = 5
	config.Bounds.MaxValues = 5
	config.Bounds.MaxMembership = 3
	config.Bounds.MaxSorts = 2
	config.Bounds.MaxCost = 50
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Includes: Present([]string{"customer", "customer.address"}),
		Filter: &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
			*testLeaf("value", OpBetween, StringValue("a"), StringValue("b")),
			*testLeaf("other", OpIsNull),
		}},
		Sorts: Present([]SortTerm{{Name: "status", Direction: Descending}}),
		Page:  PageRequest{Mode: PageCursor, Size: 2},
	}
	plan, err := Compile(context.Background(), schema, request, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(plan.Includes()) != 2 || len(plan.Sorts()) != 2 || plan.Filter() == nil || plan.Cost() <= 1 {
		t.Fatalf("plan = %#v", plan)
	}
	filterCopy := plan.Filter()
	filterCopy.Children[0].Predicate.Values[0] = StringValue("changed")
	if plan.Filter().Children[0].Predicate.Values[0].String() != "a" {
		t.Fatal("Filter returned owned storage")
	}
	if filterCost(schema, nil) != 0 {
		t.Fatal("nil filter had a cost")
	}
	if _, duplicateErr := Compile(context.Background(), schema,
		Request{Includes: Present([]string{"customer", "customer"})}, CompileOptions{}); duplicateErr == nil {
		t.Fatal("Compile accepted a duplicate include")
	}
	if _, duplicateErr := Compile(context.Background(), schema,
		Request{Sorts: Present([]SortTerm{{Name: "id", Direction: Ascending},
			{Name: "id", Direction: Descending}})}, CompileOptions{}); duplicateErr == nil {
		t.Fatal("Compile accepted a duplicate sort")
	}

	depthConfig := compileMatrixConfig()
	depthConfig.Bounds.MaxFilterDepth = 2
	depthConfig.Bounds.MaxFilterNodes = 5
	depthSchema, depthErr := NewSchema(depthConfig)
	if depthErr != nil {
		t.Fatal(depthErr)
	}
	deepFilter := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{{Logic: LogicAnd,
		Children: []FilterExpr{{Logic: LogicAnd, Children: []FilterExpr{
			*testLeaf("value", OpEqual, StringValue("a")),
		}}}}}}
	if _, compileErr := Compile(context.Background(), depthSchema,
		Request{Filter: deepFilter}, CompileOptions{}); compileErr == nil {
		t.Fatal("Compile accepted excessive filter depth")
	}

	noPageConfig := compileMatrixConfig()
	noPageConfig.Pagination = PaginationDefinition{}
	noPageSchema, noPageErr := NewSchema(noPageConfig)
	if noPageErr != nil {
		t.Fatal(noPageErr)
	}
	if _, compileErr := Compile(context.Background(), noPageSchema,
		Request{Page: PageRequest{Mode: PageCursor}}, CompileOptions{}); compileErr == nil {
		t.Fatal("Compile accepted undeclared cursor pagination")
	}

	costConfig := compileMatrixConfig()
	costConfig.Fields[0].Cost = 3
	costConfig.Bounds.MaxCost = 2
	_, schemaErr := NewSchema(costConfig)
	if schemaErr == nil {
		// Individual declared costs cannot exceed MaxCost; use two allowed costs.
		t.Fatal("schema accepted individually excessive cost")
	}
	costConfig = compileMatrixConfig()
	costConfig.Fields[0].Cost = 1
	costConfig.Fields[1].Cost = 1
	costConfig.Bounds.MaxCost = 1
	costSchema, schemaErr := NewSchema(costConfig)
	if schemaErr != nil {
		t.Fatalf("NewSchema(cost) error = %v", schemaErr)
	}
	if _, compileErr := Compile(context.Background(), costSchema, Request{}, CompileOptions{}); compileErr == nil {
		t.Fatal("Compile accepted aggregate cost over maximum")
	}
}

func compileMatrixSchema(t *testing.T) *Schema {
	t.Helper()
	schema, err := NewSchema(compileMatrixConfig())
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func compileMatrixConfig() SchemaConfig {
	return SchemaConfig{Resource: "records", Revision: "v1",
		Fields: []FieldDefinition{{Name: "id", Type: TypeString, Required: true},
			{Name: "status", Type: TypeString, Default: true}, {Name: "extra", Type: TypeString},
			{Name: "legacy", Type: TypeString, Deprecated: true}},
		Filters: []FilterDefinition{
			{Name: "value", Type: TypeString, Operators: []Operator{OpEqual, OpIn, OpBetween}},
			{Name: "other", Type: TypeString, Nullable: true, Operators: []Operator{OpEqual, OpIsNull}},
		},
		Sorts: []SortDefinition{{Name: "id", Type: TypeString, TieBreaker: true, Nulls: NullsLast},
			{Name: "status", Type: TypeString}},
		Relationships: []RelationshipDefinition{{Name: "customer", Resource: "customers", Cost: 1,
			Relationships: []RelationshipDefinition{{Name: "address", Resource: "addresses", Cost: 1}}}},
		AllowedLogic: []Logic{LogicAnd, LogicNot}, Pagination: PaginationDefinition{Cursor: true, Offset: true, DefaultPageSize: 2, MaxOffset: 3},
		Bounds: Bounds{MaxFields: 2, MaxIncludes: 1, MaxIncludeDepth: 1, MaxFilterDepth: 2,
			MaxFilterNodes: 2, MaxValues: 2, MaxMembership: 1, MaxStringBytes: 3,
			MaxSorts: 1, MaxPageSize: 3, MaxCursorBytes: 3, MaxErrors: 20, MaxCost: 20},
	}
}

func testLeaf(name string, operator Operator, values ...Value) *FilterExpr {
	return &FilterExpr{Predicate: &Predicate{Name: name, Operator: operator, Values: values}}
}

func TestCompileConcealsMessages(t *testing.T) {
	t.Parallel()

	schema := compileMatrixSchema(t)
	_, err := Compile(context.Background(), schema, Request{Filter: testLeaf("value", OpEqual,
		StringValue(strings.Repeat("secret", 2)))}, CompileOptions{})
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("diagnostic exposed rejected filter value")
	}
}

type cursorDecoderFunc func(context.Context, string, string, []SortTerm) (CursorState, error)

func (function cursorDecoderFunc) DecodeCursor(ctx context.Context, token, revision string,
	sorts []SortTerm) (CursorState, error) {
	return function(ctx, token, revision, sorts)
}

func TestCompileAuthenticatesAndCanonicalizesCursorState(t *testing.T) {
	t.Parallel()

	config := compileMatrixConfig()
	config.Bounds.MaxCursorBytes = 64
	config.Bounds.MaxStringBytes = 64
	schema, schemaErr := NewSchema(config)
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	decoder := cursorDecoderFunc(func(_ context.Context, token, revision string,
		sorts []SortTerm) (CursorState, error) {
		if token == "rejected-secret" {
			return CursorState{}, errors.New("unsafe cursor details")
		}
		if revision != "v1" || len(sorts) != 1 || sorts[0].Name != "id" {
			t.Fatal("decoder received the wrong cursor contract")
		}
		return CursorState{Direction: CursorForward,
			Positions: []Value{StringValue("position-secret")}, Policy: "policy-v1"}, nil
	})
	compile := func(token string, decoder CursorDecoder) (*Plan, error) {
		return Compile(context.Background(), schema, Request{Page: PageRequest{
			Mode: PageCursor, After: token,
		}}, CompileOptions{CursorDecoder: decoder})
	}
	first, err := compile("opaque-one", decoder)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compile("opaque-two", decoder)
	if err != nil {
		t.Fatal(err)
	}
	state := first.Cursor()
	if state == nil || state.Direction != CursorForward || state.Positions[0].String() != "position-secret" ||
		first.Page().After != "" {
		t.Fatalf("compiled cursor state = %#v page=%#v", state, first.Page())
	}
	state.Positions[0] = StringValue("mutated")
	freshState := first.Cursor()
	if freshState == nil || freshState.Positions[0].String() != "position-secret" {
		t.Fatal("Cursor returned plan-owned storage")
	}
	firstCanonical, _ := first.Canonical()
	secondCanonical, _ := second.Canonical()
	if string(firstCanonical) != string(secondCanonical) || strings.Contains(string(firstCanonical), "opaque") ||
		strings.Contains(string(firstCanonical), "position-secret") {
		t.Fatalf("cursor canonicalization = %s / %s", firstCanonical, secondCanonical)
	}

	failures := []struct {
		name    string
		request Request
		decoder CursorDecoder
	}{
		{"missing decoder", Request{Page: PageRequest{Mode: PageCursor, After: "token"}}, nil},
		{"decode failure", Request{Page: PageRequest{Mode: PageCursor, After: "rejected-secret"}}, decoder},
		{"wrong direction", Request{Page: PageRequest{Mode: PageCursor, After: "token"}},
			cursorDecoderFunc(func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorBackward, Positions: []Value{StringValue("1")}}, nil
			})},
		{"wrong count", Request{Page: PageRequest{Mode: PageCursor, After: "token"}},
			cursorDecoderFunc(func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorForward}, nil
			})},
		{"wrong type", Request{Page: PageRequest{Mode: PageCursor, After: "token"}},
			cursorDecoderFunc(func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorForward, Positions: []Value{IntValue(1)}}, nil
			})},
		{"oversized state", Request{Page: PageRequest{Mode: PageCursor, After: "token"}},
			cursorDecoderFunc(func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorForward,
					Positions: []Value{StringValue(strings.Repeat("x", 65))}}, nil
			})},
		{"oversized policy", Request{Page: PageRequest{Mode: PageCursor, After: "token"}},
			cursorDecoderFunc(func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorForward, Positions: []Value{StringValue("1")},
					Policy: strings.Repeat("x", 65)}, nil
			})},
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			_, compileErr := Compile(context.Background(), schema, failure.request,
				CompileOptions{CursorDecoder: failure.decoder})
			var violations *Violations
			if !errors.As(compileErr, &violations) || violations.Items()[0].Code != CodeCursorFailure ||
				strings.Contains(compileErr.Error(), "secret") {
				t.Fatalf("Compile() error = %v", compileErr)
			}
		})
	}
}

func TestCompileRejectsCostOverflowAndExcessCursorTotalOrder(t *testing.T) {
	t.Parallel()

	overflowSchema, err := NewSchema(SchemaConfig{Resource: "records", Revision: "v1",
		Fields: []FieldDefinition{{Name: "one", Type: TypeString, Default: true, Cost: math.MaxInt},
			{Name: "two", Type: TypeString, Default: true, Cost: math.MaxInt}},
		Bounds: Bounds{MaxCost: math.MaxInt}})
	if err != nil {
		t.Fatal(err)
	}
	if _, compileErr := Compile(context.Background(), overflowSchema, Request{}, CompileOptions{}); compileErr == nil {
		t.Fatal("Compile accepted overflowing aggregate cost")
	}
	filterSchema, err := NewSchema(SchemaConfig{Resource: "records", Revision: "v1",
		Fields: []FieldDefinition{{Name: "id", Type: TypeString}},
		Filters: []FilterDefinition{
			{Name: "one", Type: TypeString, Operators: []Operator{OpEqual}, Cost: math.MaxInt},
			{Name: "two", Type: TypeString, Operators: []Operator{OpEqual}, Cost: math.MaxInt},
		},
		AllowedLogic: []Logic{LogicAnd}, Bounds: Bounds{MaxCost: math.MaxInt}})
	if err != nil {
		t.Fatal(err)
	}
	filter := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
		*testLeaf("one", OpEqual, StringValue("a")), *testLeaf("two", OpEqual, StringValue("b")),
	}}
	if _, compileErr := Compile(context.Background(), filterSchema,
		Request{Filter: filter}, CompileOptions{}); compileErr == nil {
		t.Fatal("Compile accepted overflowing aggregate filter cost")
	}

	config := compileMatrixConfig()
	config.Bounds.MaxSorts = 1
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(context.Background(), schema, Request{
		Sorts: Present([]SortTerm{{Name: "status", Direction: Ascending}}),
		Page:  PageRequest{Mode: PageCursor},
	}, CompileOptions{})
	if err == nil {
		t.Fatal("Compile appended a tie-breaker beyond the sort limit")
	}
}
