package apiquery

import (
	"context"
	"fmt"
	"math"
)

// CapabilityKind identifies an authorization decision without exposing schema
// internals through failures.
type CapabilityKind string

const (
	CapabilityField        CapabilityKind = "field"
	CapabilityFilter       CapabilityKind = "filter"
	CapabilitySort         CapabilityKind = "sort"
	CapabilityRelationship CapabilityKind = "relationship"
)

// Capability describes a declared element to an application authorizer.
type Capability struct {
	Kind CapabilityKind
	Name string
}

// AuthorizeFunc decides whether a declared capability is available to the
// current principal. It runs only after the schema declaration is found.
type AuthorizeFunc func(context.Context, Capability) bool

// CursorDecoder authenticates and decodes one opaque cursor for an exact
// schema revision and ordered sort contract.
type CursorDecoder interface {
	DecodeCursor(context.Context, string, string, []SortTerm) (CursorState, error)
}

// CompileOptions supplies request-scoped policy without mutating the schema.
type CompileOptions struct {
	Authorize            AuthorizeFunc
	MandatoryConstraints []Constraint
	CursorDecoder        CursorDecoder
}

// Constraint is a server-owned mandatory equality predicate. Transport input
// cannot create, remove, or replace constraints.
type Constraint struct {
	Name      string `json:"name"`
	Value     Value  `json:"value"`
	Protected bool   `json:"protected"`
}

// Compile validates and snapshots a request into an immutable plan.
func Compile(ctx context.Context, schema *Schema, request Request, options CompileOptions) (*Plan, error) {
	if schema == nil {
		return nil, &Violations{items: []Violation{{Code: CodeInvalidElement,
			Path: "schema", Message: "schema is required"}}}
	}
	collector := violationCollector{limit: schema.bounds.MaxErrors}
	plan := &Plan{resource: schema.resource, revision: schema.revision,
		page: request.Page, cost: 1, maxCanonical: schema.bounds.MaxCanonicalBytes}
	if revision, present := request.SchemaRevision.Value(); present && revision != schema.revision {
		collector.add(CodeVersionMismatch, "schema_revision", "schema revision does not match")
	}
	compileConstraints(schema, options.MandatoryConstraints, plan, &collector)
	compileFields(ctx, schema, request.Fields, options, plan, &collector)
	compileIncludes(ctx, schema, request.Includes, options, plan, &collector)
	plan.filter = compileFilter(ctx, schema, request.Filter, options, &collector)
	plan.sorts = compileSorts(ctx, schema, request.Sorts, options, &collector)
	plan.sorts = compilePage(ctx, schema, &plan.page, plan.sorts, options, &collector)
	compileCursor(ctx, schema, plan, options, &collector)
	if plan.filter != nil {
		addPlanCost(schema, plan, filterCost(schema, plan.filter))
	}
	for _, sort := range plan.sorts {
		addPlanCost(schema, plan, schema.sorts[schema.sortIndex[sort.Name]].Cost)
	}
	if plan.costExceeded || plan.cost > schema.bounds.MaxCost {
		collector.add(CodeCostLimit, "query", "projected query cost exceeds the limit")
	}
	if err := collector.err(); err != nil {
		return nil, err
	}
	return plan, nil
}

func compileConstraints(schema *Schema, constraints []Constraint, plan *Plan, collector *violationCollector) {
	if len(constraints) > schema.bounds.MaxValues {
		collector.add(CodeLimitExceeded, "constraints", "mandatory constraint count exceeds the limit")
		constraints = constraints[:schema.bounds.MaxValues]
	}
	plan.constraints = append([]Constraint(nil), constraints...)
	seen := make(map[string]struct{}, len(constraints))
	for index, constraint := range constraints {
		path := fmt.Sprintf("constraints[%d]", index)
		if !validName(constraint.Name) || !validType(constraint.Value.Type()) {
			collector.add(CodeInvalidElement, path, "mandatory constraint is invalid")
		}
		if _, duplicate := seen[constraint.Name]; duplicate {
			collector.add(CodeConflict, path, "mandatory constraint is duplicated")
		}
		seen[constraint.Name] = struct{}{}
		if len(constraint.Value.String()) > schema.bounds.MaxStringBytes {
			collector.add(CodeLimitExceeded, path, "mandatory constraint exceeds the size limit")
		}
	}
}

func compileFields(ctx context.Context, schema *Schema, requested Optional[[]string], options CompileOptions, plan *Plan, collector *violationCollector) {
	fields, present := requested.Value()
	if !present {
		for _, field := range schema.fields {
			if field.Default {
				fields = append(fields, field.Name)
			}
		}
	}
	if len(fields) > schema.bounds.MaxFields {
		collector.add(CodeLimitExceeded, "fields", "field count exceeds the limit")
		fields = fields[:schema.bounds.MaxFields]
	}
	seen := make(map[string]struct{}, len(fields))
	for index, name := range fields {
		path := fmt.Sprintf("fields[%d]", index)
		definitionIndex, exists := schema.fieldIndex[name]
		if !exists {
			collector.add(CodeInvalidElement, path, "field is not available")
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			collector.add(CodeConflict, path, "field is duplicated")
			continue
		}
		seen[name] = struct{}{}
		if schema.fields[definitionIndex].Deprecated {
			collector.add(CodeUnsupported, path, "field is deprecated")
			continue
		}
		if !authorized(ctx, options, CapabilityField, name) {
			collector.add(CodeAuthorization, path, "capability is not available")
			continue
		}
		plan.responseFields = append(plan.responseFields, name)
		addPlanCost(schema, plan, schema.fields[definitionIndex].Cost)
	}
	executionSeen := make(map[string]struct{}, len(plan.responseFields))
	for _, name := range plan.responseFields {
		executionSeen[name] = struct{}{}
	}
	for _, field := range schema.fields {
		if field.Required {
			if _, exists := executionSeen[field.Name]; !exists {
				plan.executionFields = append(plan.executionFields, field.Name)
				addPlanCost(schema, plan, field.Cost)
				executionSeen[field.Name] = struct{}{}
			}
		}
	}
	plan.executionFields = append(plan.executionFields, plan.responseFields...)
}

func compileIncludes(ctx context.Context, schema *Schema, requested Optional[[]string], options CompileOptions, plan *Plan, collector *violationCollector) {
	includes, present := requested.Value()
	if !present {
		return
	}
	if len(includes) > schema.bounds.MaxIncludes {
		collector.add(CodeLimitExceeded, "includes", "include count exceeds the limit")
		includes = includes[:schema.bounds.MaxIncludes]
	}
	seen := make(map[string]struct{}, len(includes))
	seenEdges := make(map[string]struct{}, len(includes))
	for index, name := range includes {
		path := fmt.Sprintf("includes[%d]", index)
		node, exists := schema.relationIndex[name]
		if !exists {
			collector.add(CodeInvalidElement, path, "relationship is not available")
			continue
		}
		if node.depth > schema.bounds.MaxIncludeDepth {
			collector.add(CodeLimitExceeded, path, "include depth exceeds the limit")
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			collector.add(CodeConflict, path, "relationship is duplicated")
			continue
		}
		seen[name] = struct{}{}
		allowed := true
		for _, edge := range relationshipPrefixes(name) {
			if _, checked := seenEdges[edge]; checked {
				continue
			}
			if !authorized(ctx, options, CapabilityRelationship, edge) {
				collector.add(CodeAuthorization, path, "capability is not available")
				allowed = false
				break
			}
			seenEdges[edge] = struct{}{}
			addPlanCost(schema, plan, schema.relationIndex[edge].definition.Cost)
		}
		if !allowed {
			continue
		}
		plan.includes = append(plan.includes, name)
	}
}

func relationshipPrefixes(path string) []string {
	result := make([]string, 0, relationshipDepth(path))
	for index, char := range path {
		if char == '.' {
			result = append(result, path[:index])
		}
	}
	return append(result, path)
}

func compileFilter(ctx context.Context, schema *Schema, filter *FilterExpr, options CompileOptions, collector *violationCollector) *FilterExpr {
	if filter == nil {
		return nil
	}
	nodes, values := 0, 0
	seen := make(map[string]struct{})
	result := compileFilterNode(ctx, schema, filter, options, collector, "filter", 1, &nodes, &values, seen)
	return result
}

func compileFilterNode(ctx context.Context, schema *Schema, node *FilterExpr, options CompileOptions, collector *violationCollector, path string, depth int, nodes, values *int, seen map[string]struct{}) *FilterExpr {
	*nodes++
	if depth > schema.bounds.MaxFilterDepth {
		collector.add(CodeLimitExceeded, path, "filter depth exceeds the limit")
		return nil
	}
	if (node.Predicate == nil) == (len(node.Children) == 0) {
		collector.add(CodeConflict, path, "filter must contain exactly one predicate or logical group")
		return nil
	}
	if node.Predicate != nil {
		predicate := node.Predicate
		definitionIndex, exists := schema.filterIndex[predicate.Name]
		if !exists {
			collector.add(CodeInvalidElement, path, "filter is not available")
			return nil
		}
		if !authorized(ctx, options, CapabilityFilter, predicate.Name) {
			collector.add(CodeAuthorization, path, "capability is not available")
			return nil
		}
		definition := schema.filters[definitionIndex]
		if definition.Deprecated {
			collector.add(CodeUnsupported, path, "filter is deprecated")
			return nil
		}
		key := predicate.Name + "\x00" + string(predicate.Operator)
		if _, duplicate := seen[key]; duplicate {
			collector.add(CodeConflict, path, "filter is duplicated")
			return nil
		}
		seen[key] = struct{}{}
		if !containsOperator(definition.Operators, predicate.Operator) {
			collector.add(CodeUnsupported, path, "filter operation is not available")
		}
		if len(predicate.Values) > schema.bounds.MaxValues-*values {
			collector.add(CodeLimitExceeded, path, "filter value count exceeds the limit")
			return nil
		}
		*values += len(predicate.Values)
		validatePredicateValues(schema, definition, predicate, path, collector)
		return cloneFilter(node)
	}
	if _, allowed := schema.allowedLogic[node.Logic]; !allowed {
		collector.add(CodeUnsupported, path, "logical operation is not available")
	}
	if node.Logic == LogicNot && len(node.Children) != 1 {
		collector.add(CodeConflict, path, "not requires exactly one child")
	}
	result := &FilterExpr{Logic: node.Logic, Children: make([]FilterExpr, 0, len(node.Children))}
	for index := range node.Children {
		if *nodes >= schema.bounds.MaxFilterNodes {
			collector.add(CodeLimitExceeded, path, "filter node count exceeds the limit")
			break
		}
		child := compileFilterNode(ctx, schema, &node.Children[index], options, collector,
			fmt.Sprintf("%s.children[%d]", path, index), depth+1, nodes, values, seen)
		if child != nil {
			result.Children = append(result.Children, *child)
		}
	}
	return result
}

func validatePredicateValues(schema *Schema, definition FilterDefinition, predicate *Predicate, path string, collector *violationCollector) {
	want := 1
	switch predicate.Operator {
	case OpIsNull:
		want = 0
	case OpBetween:
		want = 2
	case OpIn, OpNotIn:
		want = -1
		if len(predicate.Values) == 0 || len(predicate.Values) > schema.bounds.MaxMembership {
			collector.add(CodeLimitExceeded, path, "membership value count is outside its bounds")
		}
	case OpEqual, OpNotEqual, OpLess, OpLessOrEqual, OpGreater, OpGreaterOrEqual,
		OpContains, OpStartsWith, OpEndsWith:
	}
	if want >= 0 && len(predicate.Values) != want {
		collector.add(CodeConflict, path, "filter has an invalid value count")
	}
	seen := make(map[string]struct{}, len(predicate.Values))
	for _, value := range predicate.Values {
		key := string(value.Type()) + "\x00" + value.String()
		if _, duplicate := seen[key]; duplicate {
			collector.add(CodeConflict, path, "filter value is duplicated")
		}
		seen[key] = struct{}{}
		if value.Type() != definition.Type {
			collector.add(CodeInvalidElement, path, "filter value type does not match")
		}
		if len(value.String()) > schema.bounds.MaxStringBytes {
			collector.add(CodeLimitExceeded, path, "filter value exceeds the size limit")
		}
		if value.Type() == TypeString && value.String() == "" && !definition.AllowEmpty {
			collector.add(CodeInvalidElement, path, "empty filter value is not allowed")
		}
	}
}

func compileSorts(ctx context.Context, schema *Schema, requested Optional[[]SortTerm], options CompileOptions, collector *violationCollector) []SortTerm {
	sorts, present := requested.Value()
	if !present {
		sorts = append([]SortTerm(nil), schema.defaultSort...)
	}
	if len(sorts) > schema.bounds.MaxSorts {
		collector.add(CodeLimitExceeded, "sorts", "sort count exceeds the limit")
		sorts = sorts[:schema.bounds.MaxSorts]
	}
	result := make([]SortTerm, 0, len(sorts)+1)
	seen := make(map[string]struct{}, len(sorts))
	for index, sort := range sorts {
		path := fmt.Sprintf("sorts[%d]", index)
		if _, exists := schema.sortIndex[sort.Name]; !exists {
			collector.add(CodeInvalidElement, path, "sort is not available")
			continue
		}
		if _, duplicate := seen[sort.Name]; duplicate {
			collector.add(CodeConflict, path, "sort is duplicated")
			continue
		}
		seen[sort.Name] = struct{}{}
		if sort.Direction != Ascending && sort.Direction != Descending {
			collector.add(CodeInvalidElement, path, "sort direction is invalid")
			continue
		}
		if !authorized(ctx, options, CapabilitySort, sort.Name) {
			collector.add(CodeAuthorization, path, "capability is not available")
			continue
		}
		if sort.Nulls != "" && sort.Nulls != NullsFirst && sort.Nulls != NullsLast {
			collector.add(CodeInvalidElement, path, "sort null ordering is invalid")
			continue
		}
		declaredNulls := schema.sorts[schema.sortIndex[sort.Name]].Nulls
		if sort.Nulls != "" && sort.Nulls != declaredNulls {
			collector.add(CodeUnsupported, path, "sort null ordering is not available")
			continue
		}
		if sort.Nulls == "" {
			sort.Nulls = declaredNulls
		}
		result = append(result, sort)
	}
	return result
}

func compilePage(ctx context.Context, schema *Schema, page *PageRequest, sorts []SortTerm, options CompileOptions, collector *violationCollector) []SortTerm {
	if page.Mode == "" {
		page.Mode = PageNone
	}
	if page.Mode == PageCursor && !schema.pagination.Cursor {
		collector.add(CodeUnsupported, "page.mode", "cursor pagination is not available")
	}
	if page.Mode == PageOffset && !schema.pagination.Offset {
		collector.add(CodeUnsupported, "page.mode", "offset pagination is not available")
	}
	if page.Mode != PageNone && page.Size == 0 {
		page.Size = schema.pagination.DefaultPageSize
	}
	if page.Mode == PageOffset && (page.Offset < 0 || page.Offset > schema.pagination.MaxOffset) {
		collector.add(CodeLimitExceeded, "page.offset", "offset is outside its bounds")
	}
	if page.Size < 0 || page.Size > schema.bounds.MaxPageSize {
		collector.add(CodeLimitExceeded, "page.size", "page size is outside its bounds")
	}
	if page.After != "" && page.Before != "" {
		collector.add(CodeConflict, "page", "after and before cursors conflict")
	}
	if len(page.After) > schema.bounds.MaxCursorBytes {
		collector.add(CodeLimitExceeded, "page.after", "cursor exceeds the size limit")
	}
	if len(page.Before) > schema.bounds.MaxCursorBytes {
		collector.add(CodeLimitExceeded, "page.before", "cursor exceeds the size limit")
	}
	if page.Mode != PageNone && page.Mode != PageCursor && page.Mode != PageOffset {
		collector.add(CodeUnsupported, "page.mode", "pagination mode is not available")
	}
	switch page.Mode {
	case PageNone:
		if page.Size != 0 || page.After != "" || page.Before != "" || page.Offset != 0 {
			collector.add(CodeConflict, "page", "pagination state requires an active mode")
		}
	case PageCursor:
		if page.Offset != 0 {
			collector.add(CodeConflict, "page.offset", "offset conflicts with cursor pagination")
		}
	case PageOffset:
		if page.After != "" || page.Before != "" {
			collector.add(CodeConflict, "page", "cursor conflicts with offset pagination")
		}
	}
	if page.Mode == PageCursor {
		seen := make(map[string]struct{}, len(sorts))
		for _, sort := range sorts {
			seen[sort.Name] = struct{}{}
		}
		for _, definition := range schema.sorts {
			if definition.TieBreaker {
				if _, exists := seen[definition.Name]; !exists {
					if len(sorts) >= schema.bounds.MaxSorts {
						collector.add(CodeLimitExceeded, "sorts", "cursor total order exceeds the sort limit")
						return sorts
					}
					if !authorized(ctx, options, CapabilitySort, definition.Name) {
						collector.add(CodeAuthorization, "sorts", "capability is not available")
						return sorts
					}
					sorts = append(sorts, SortTerm{Name: definition.Name,
						Direction: Ascending, Nulls: definition.Nulls})
				}
				return sorts
			}
		}
	}
	return sorts
}

func compileCursor(ctx context.Context, schema *Schema, plan *Plan, options CompileOptions, collector *violationCollector) {
	if plan.page.Mode != PageCursor || (plan.page.After == "" && plan.page.Before == "") ||
		(plan.page.After != "" && plan.page.Before != "") {
		return
	}
	token := plan.page.After
	expectedDirection := CursorForward
	if plan.page.Before != "" {
		token = plan.page.Before
		expectedDirection = CursorBackward
	}
	if options.CursorDecoder == nil {
		collector.add(CodeCursorFailure, "page", "cursor could not be verified")
		return
	}
	state, err := options.CursorDecoder.DecodeCursor(ctx, token, schema.revision,
		append([]SortTerm(nil), plan.sorts...))
	if err != nil || state.Direction != expectedDirection || len(state.Positions) != len(plan.sorts) ||
		len(state.Policy) > schema.bounds.MaxStringBytes {
		collector.add(CodeCursorFailure, "page", "cursor could not be verified")
		return
	}
	for index, position := range state.Positions {
		definition := schema.sorts[schema.sortIndex[plan.sorts[index].Name]]
		if len(position.String()) > schema.bounds.MaxStringBytes ||
			(position.Type() != TypeNull && position.Type() != definition.Type) {
			collector.add(CodeCursorFailure, "page", "cursor could not be verified")
			return
		}
	}
	plan.cursor = &CursorState{Direction: state.Direction,
		Positions: append([]Value(nil), state.Positions...), Policy: state.Policy}
	plan.page.After = ""
	plan.page.Before = ""
}

func addPlanCost(schema *Schema, plan *Plan, amount int) {
	if plan.costExceeded || amount > schema.bounds.MaxCost-plan.cost {
		plan.costExceeded = true
		return
	}
	plan.cost += amount
}

func filterCost(schema *Schema, filter *FilterExpr) int {
	if filter == nil {
		return 0
	}
	if filter.Predicate != nil {
		return schema.filters[schema.filterIndex[filter.Predicate.Name]].Cost
	}
	cost := 0
	for index := range filter.Children {
		childCost := filterCost(schema, &filter.Children[index])
		if childCost > math.MaxInt-cost {
			return math.MaxInt
		}
		cost += childCost
	}
	return cost
}

func containsOperator(operators []Operator, wanted Operator) bool {
	for _, operator := range operators {
		if operator == wanted {
			return true
		}
	}
	return false
}

func authorized(ctx context.Context, options CompileOptions, kind CapabilityKind, name string) bool {
	return options.Authorize == nil || options.Authorize(ctx, Capability{Kind: kind, Name: name})
}
