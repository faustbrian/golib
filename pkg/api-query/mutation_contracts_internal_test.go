package apiquery

import (
	"context"
	"errors"
	"math"
	"slices"
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
		{TypeFloat, "1", true}, {TypeFloat, "1.0", false}, {TypeFloat, "NaN", false},
		{TypeFloat, "+Inf", false}, {TypeFloat, "bad", false},
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

func TestCompileSubphasesPreserveMixedSequenceOutcomes(t *testing.T) {
	t.Parallel()

	config := compileMatrixConfig()
	config.Bounds.MaxFields = 4
	config.Bounds.MaxIncludes = 5
	config.Bounds.MaxIncludeDepth = 2
	config.Relationships = append(config.Relationships,
		RelationshipDefinition{Name: "vendor", Resource: "vendors"})
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatal(err)
	}

	constraintCollector := violationCollector{limit: 20}
	constraintPlan := &Plan{}
	compileConstraints(schema, []Constraint{
		{Name: "Bad", Value: StringValue("a")},
		{Name: "valid", Value: Value{}},
	}, constraintPlan, &constraintCollector)
	if !violationPathsEqual(constraintCollector.items, []string{"constraints[0]", "constraints[1]"}) {
		t.Fatalf("independent constraint violations = %#v", constraintCollector.items)
	}

	for _, test := range []struct {
		name      string
		fields    []string
		authorize AuthorizeFunc
		want      []string
		paths     []string
	}{
		{name: "duplicate", fields: []string{"id", "id", "status"}, want: []string{"id", "status"}, paths: []string{"fields[1]"}},
		{name: "deprecated", fields: []string{"legacy", "status"}, want: []string{"status"}, paths: []string{"fields[0]"}},
		{name: "authorization", fields: []string{"id", "status"},
			authorize: func(_ context.Context, capability Capability) bool { return capability.Name != "id" },
			want:      []string{"status"}, paths: []string{"fields[0]"}},
	} {
		plan := &Plan{}
		collector := violationCollector{limit: 20}
		compileFields(context.Background(), schema, Present(test.fields),
			CompileOptions{Authorize: test.authorize}, plan, &collector)
		if !equalStringSlices(plan.responseFields, test.want) || !violationPathsEqual(collector.items, test.paths) {
			t.Fatalf("%s fields = %q, violations = %#v", test.name, plan.responseFields, collector.items)
		}
	}

	includeTests := []struct {
		name      string
		includes  []string
		authorize AuthorizeFunc
		want      []string
		paths     []string
		calls     []string
	}{
		{name: "unknown", includes: []string{"missing", "vendor"}, want: []string{"vendor"}, paths: []string{"includes[0]"}},
		{name: "deep", includes: []string{"customer.address", "vendor"}, want: []string{"customer.address", "vendor"}},
		{name: "duplicate", includes: []string{"customer", "customer", "customer.address"},
			want: []string{"customer", "customer.address"}, paths: []string{"includes[1]"}},
		{name: "selective denial", includes: []string{"customer.address", "vendor"},
			authorize: func(_ context.Context, capability Capability) bool { return capability.Name != "customer" },
			want:      []string{"vendor"}, paths: []string{"includes[0]"}, calls: []string{"customer", "vendor"}},
	}
	for _, test := range includeTests {
		var calls []string
		authorize := test.authorize
		if authorize != nil {
			authorize = func(ctx context.Context, capability Capability) bool {
				calls = append(calls, capability.Name)
				return test.authorize(ctx, capability)
			}
		}
		plan := &Plan{}
		collector := violationCollector{limit: 20}
		compileIncludes(context.Background(), schema, Present(test.includes),
			CompileOptions{Authorize: authorize}, plan, &collector)
		if !equalStringSlices(plan.includes, test.want) || !violationPathsEqual(collector.items, test.paths) ||
			test.calls != nil && !equalStringSlices(calls, test.calls) {
			t.Fatalf("%s includes = %q, calls = %q, violations = %#v", test.name, plan.includes, calls, collector.items)
		}
	}

	var prefixCalls []string
	prefixPlan := &Plan{}
	prefixCollector := violationCollector{limit: 20}
	compileIncludes(context.Background(), schema, Present([]string{"customer", "customer.address"}),
		CompileOptions{Authorize: func(_ context.Context, capability Capability) bool {
			prefixCalls = append(prefixCalls, capability.Name)
			return true
		}}, prefixPlan, &prefixCollector)
	if !equalStringSlices(prefixCalls, []string{"customer", "customer.address"}) || len(prefixCollector.items) != 0 {
		t.Fatalf("prefix authorization calls = %q, violations = %#v", prefixCalls, prefixCollector.items)
	}

	depthSchema := *schema
	depthSchema.bounds.MaxIncludeDepth = 1
	depthPlan := &Plan{}
	depthCollector := violationCollector{limit: 20}
	compileIncludes(context.Background(), &depthSchema, Present([]string{"customer.address", "vendor"}),
		CompileOptions{}, depthPlan, &depthCollector)
	if !equalStringSlices(depthPlan.includes, []string{"vendor"}) ||
		!violationPathsEqual(depthCollector.items, []string{"includes[0]"}) {
		t.Fatalf("depth includes = %q, violations = %#v", depthPlan.includes, depthCollector.items)
	}
}

func TestFilterSortAndPageSubphasesHonorExactAndMixedBounds(t *testing.T) {
	t.Parallel()

	config := compileMatrixConfig()
	config.Bounds.MaxFilterDepth = 2
	config.Bounds.MaxFilterNodes = 3
	config.Bounds.MaxValues = 3
	config.Bounds.MaxMembership = 2
	config.Bounds.MaxSorts = 4
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatal(err)
	}

	nodes, values := 0, 0
	collector := violationCollector{limit: 20}
	leaf := testLeaf("value", OpEqual, StringValue("a"))
	if compiled := compileFilterNode(context.Background(), schema, leaf, CompileOptions{}, &collector,
		"filter", 2, &nodes, &values, map[string]struct{}{}); compiled == nil || len(collector.items) != 0 {
		t.Fatalf("exact-depth filter = %#v, violations = %#v", compiled, collector.items)
	}

	exactValues := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
		*testLeaf("value", OpIn, StringValue("a"), StringValue("b")),
		*testLeaf("other", OpEqual, StringValue("c")),
	}}
	nodes, values = 0, 0
	collector = violationCollector{limit: 20}
	if compiled := compileFilterNode(context.Background(), schema, exactValues, CompileOptions{}, &collector,
		"filter", 1, &nodes, &values, map[string]struct{}{}); compiled == nil || len(collector.items) != 0 || values != 3 {
		t.Fatalf("exact-value filter = %#v, values = %d, violations = %#v", compiled, values, collector.items)
	}
	overValues := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
		*testLeaf("value", OpBetween, StringValue("a"), StringValue("b")),
		*testLeaf("other", OpEqual, StringValue("c"), StringValue("d")),
	}}
	collector = violationCollector{limit: 20}
	_ = compileFilter(context.Background(), schema, overValues, CompileOptions{}, &collector)
	if !hasViolation(collector.items, CodeLimitExceeded, "filter.children[1]") {
		t.Fatalf("over-value violations = %#v", collector.items)
	}

	nodeLimit := &FilterExpr{Logic: LogicAnd, Children: []FilterExpr{
		*testLeaf("value", OpEqual, StringValue("a")),
		*testLeaf("other", OpEqual, StringValue("b")),
		*testLeaf("status", OpEqual, StringValue("c")),
		*testLeaf("value", OpEqual, StringValue("d")),
	}}
	nodes, values = 0, 0
	collector = violationCollector{limit: 20}
	_ = compileFilterNode(context.Background(), schema, nodeLimit, CompileOptions{}, &collector,
		"filter", 1, &nodes, &values, map[string]struct{}{})
	if len(collector.items) != 1 || !hasViolation(collector.items, CodeLimitExceeded, "filter") {
		t.Fatalf("node-limit violations = %#v", collector.items)
	}

	collector = violationCollector{limit: 20}
	validatePredicateValues(schema, schema.filters[schema.filterIndex["other"]],
		&Predicate{Name: "other", Operator: OpIsNull, Values: []Value{NullValue()}}, "filter", &collector)
	if !hasViolation(collector.items, CodeConflict, "filter") {
		t.Fatalf("is-null arity violations = %#v", collector.items)
	}

	sortCases := []struct {
		name      string
		sorts     []SortTerm
		authorize AuthorizeFunc
		want      []SortTerm
	}{
		{name: "unknown", sorts: []SortTerm{{Name: "missing", Direction: Ascending}, {Name: "id", Direction: Ascending}}, want: []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsLast}}},
		{name: "duplicate", sorts: []SortTerm{{Name: "id", Direction: Ascending}, {Name: "id", Direction: Ascending}, {Name: "status", Direction: Ascending}}, want: []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsLast}, {Name: "status", Direction: Ascending}}},
		{name: "direction", sorts: []SortTerm{{Name: "id", Direction: "sideways"}, {Name: "status", Direction: Ascending}}, want: []SortTerm{{Name: "status", Direction: Ascending}}},
		{name: "authorization", sorts: []SortTerm{{Name: "id", Direction: Ascending}, {Name: "status", Direction: Ascending}}, authorize: func(_ context.Context, capability Capability) bool { return capability.Name != "id" }, want: []SortTerm{{Name: "status", Direction: Ascending}}},
		{name: "null ordering", sorts: []SortTerm{{Name: "id", Direction: Ascending, Nulls: "middle"}, {Name: "status", Direction: Ascending}}, want: []SortTerm{{Name: "status", Direction: Ascending}}},
		{name: "unsupported null ordering", sorts: []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsFirst}, {Name: "status", Direction: Ascending}}, want: []SortTerm{{Name: "status", Direction: Ascending}}},
	}
	for _, test := range sortCases {
		collector = violationCollector{limit: 20}
		got := compileSorts(context.Background(), schema, Present(test.sorts), CompileOptions{Authorize: test.authorize}, &collector)
		if !equalSorts(got, test.want) || len(collector.items) != 1 {
			t.Fatalf("%s sorts = %#v, violations = %#v", test.name, got, collector.items)
		}
	}

	for _, page := range []PageRequest{
		{Mode: PageNone},
		{Mode: PageOffset, Size: 1, Offset: 0},
		{Mode: PageCursor, Size: 1, After: "abc"},
		{Mode: PageCursor, Size: 1, Before: "abc"},
	} {
		collector = violationCollector{limit: 20}
		_ = compilePage(context.Background(), schema, &page, []SortTerm{{Name: "id", Direction: Ascending}}, CompileOptions{}, &collector)
		if len(collector.items) != 0 {
			t.Fatalf("exact page %#v violations = %#v", page, collector.items)
		}
	}
}

func TestCursorCompilationSeparatesEveryBoundary(t *testing.T) {
	t.Parallel()

	config := compileMatrixConfig()
	config.Bounds.MaxStringBytes = 3
	schema, err := NewSchema(config)
	if err != nil {
		t.Fatal(err)
	}
	sorts := []SortTerm{{Name: "id", Direction: Ascending, Nulls: NullsLast}}

	called := false
	plan := &Plan{page: PageRequest{Mode: PageCursor}, sorts: sorts}
	collector := violationCollector{limit: 20}
	compileCursor(context.Background(), schema, plan, CompileOptions{CursorDecoder: cursorDecoderFunc(
		func(context.Context, string, string, []SortTerm) (CursorState, error) {
			called = true
			return CursorState{}, nil
		})}, &collector)
	if called || len(collector.items) != 0 {
		t.Fatalf("empty cursor called = %v, violations = %#v", called, collector.items)
	}
	for _, page := range []PageRequest{
		{Mode: PageNone, After: "token"},
		{Mode: PageCursor, After: "after", Before: "before"},
	} {
		called = false
		plan = &Plan{page: page, sorts: sorts}
		collector = violationCollector{limit: 20}
		compileCursor(context.Background(), schema, plan, CompileOptions{CursorDecoder: cursorDecoderFunc(
			func(context.Context, string, string, []SortTerm) (CursorState, error) {
				called = true
				return CursorState{}, nil
			})}, &collector)
		if called || len(collector.items) != 0 {
			t.Fatalf("non-decodable page %#v called = %v, violations = %#v", page, called, collector.items)
		}
	}

	for _, position := range []Value{StringValue("abc"), NullValue()} {
		plan = &Plan{page: PageRequest{Mode: PageCursor, After: "token"}, sorts: sorts}
		collector = violationCollector{limit: 20}
		compileCursor(context.Background(), schema, plan, CompileOptions{CursorDecoder: cursorDecoderFunc(
			func(context.Context, string, string, []SortTerm) (CursorState, error) {
				return CursorState{Direction: CursorForward, Positions: []Value{position}, Policy: "abc"}, nil
			})}, &collector)
		if plan.cursor == nil || len(collector.items) != 0 {
			t.Fatalf("exact cursor position %q = %#v, violations = %#v", position.String(), plan.cursor, collector.items)
		}
	}
}

func TestSchemaMixedValidationContinuesAfterEachInvalidEntry(t *testing.T) {
	t.Parallel()

	base := SchemaConfig{Resource: "records", Revision: "v1", Fields: []FieldDefinition{{Name: "id", Type: TypeString}}}
	for _, test := range []struct {
		name   string
		config SchemaConfig
		paths  []string
	}{
		{name: "logic", config: func() SchemaConfig {
			config := base
			config.AllowedLogic = []Logic{"raw", LogicAnd, LogicAnd}
			return config
		}(), paths: []string{"schema.allowed_logic[0]", "schema.allowed_logic[2]"}},
		{name: "duplicate logic before invalid", config: func() SchemaConfig {
			config := base
			config.AllowedLogic = []Logic{LogicAnd, LogicAnd, "raw"}
			return config
		}(), paths: []string{"schema.allowed_logic[1]", "schema.allowed_logic[2]"}},
		{name: "relationship cycle", config: func() SchemaConfig {
			config := base
			config.Relationships = []RelationshipDefinition{
				{Name: "cycle", Resource: "records"},
				{Name: "Bad", Resource: "children"},
			}
			return config
		}(), paths: []string{"schema.relationships[0]", "schema.relationships[1]"}},
	} {
		_, err := NewSchema(test.config)
		var violations *Violations
		if !errors.As(err, &violations) || !violationPathsEqual(violations.Items(), test.paths) {
			t.Fatalf("%s violations = %v", test.name, err)
		}
	}

	config := base
	config.Filters = []FilterDefinition{
		{Name: "Bad", Type: TypeString, Operators: []Operator{OpEqual}},
		{Name: "valid", Type: "bad", Operators: []Operator{OpEqual}},
		{Name: "empty", Type: TypeString},
	}
	_, err := NewSchema(config)
	var violations *Violations
	if !errors.As(err, &violations) || len(violations.Items()) != 3 {
		t.Fatalf("independent filter declaration violations = %v", err)
	}

	config = base
	config.Pagination = PaginationDefinition{Offset: true, DefaultPageSize: 0}
	_, err = NewSchema(config)
	if err == nil {
		t.Fatal("zero pagination default was accepted")
	}

	config = base
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString}}
	config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending}}
	_, err = NewSchema(config)
	if err != nil {
		t.Fatalf("empty default null ordering error = %v", err)
	}

	for _, nulls := range []NullOrder{NullsFirst, NullsLast} {
		config = base
		config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, Nulls: nulls}}
		config.DefaultSort = []SortTerm{{Name: "id", Direction: Ascending, Nulls: nulls}}
		_, err = NewSchema(config)
		if err != nil {
			t.Fatalf("matching default null ordering %q error = %v", nulls, err)
		}
	}

	config = base
	config.Sorts = []SortDefinition{{Name: "id", Type: TypeString, Nulls: NullsFirst}}
	config.DefaultSort = []SortTerm{{Name: "missing", Direction: Ascending, Nulls: NullsLast}}
	_, err = NewSchema(config)
	violations = nil
	if !errors.As(err, &violations) || len(violations.Items()) != 1 ||
		!hasViolation(violations.Items(), CodeInvalidElement, "schema.default_sort[0]") {
		t.Fatalf("unknown default sort violations = %v", err)
	}
}

func TestCompileAcceptsExactProjectedCost(t *testing.T) {
	t.Parallel()

	schema, err := NewSchema(SchemaConfig{
		Resource: "records",
		Revision: "v1",
		Fields:   []FieldDefinition{{Name: "id", Type: TypeString, Default: true, Cost: 2}},
		Bounds:   Bounds{MaxCost: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(context.Background(), schema, Request{}, CompileOptions{})
	if err != nil || plan.Cost() != 3 {
		t.Fatalf("exact projected cost plan = %#v, error = %v", plan, err)
	}
}

func equalStringSlices(left, right []string) bool {
	return slices.Equal(left, right)
}

func violationPathsEqual(violations []Violation, paths []string) bool {
	return slices.EqualFunc(violations, paths, func(violation Violation, path string) bool {
		return violation.Path == path
	})
}

func hasViolation(violations []Violation, code ErrorCode, path string) bool {
	for _, violation := range violations {
		if violation.Code == code && violation.Path == path {
			return true
		}
	}
	return false
}

func equalSorts(left, right []SortTerm) bool {
	return slices.Equal(left, right)
}
