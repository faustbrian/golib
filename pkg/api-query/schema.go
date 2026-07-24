package apiquery

import (
	"fmt"
	"unicode/utf8"
)

// Bounds contains hard compilation and resource limits. Zero values receive
// conservative defaults rather than disabling a bound.
type Bounds struct {
	MaxRequestBytes   int
	MaxFields         int
	MaxIncludes       int
	MaxIncludeDepth   int
	MaxFilterDepth    int
	MaxFilterNodes    int
	MaxValues         int
	MaxMembership     int
	MaxStringBytes    int
	MaxSorts          int
	MaxPageSize       int
	MaxCursorBytes    int
	MaxCanonicalBytes int
	MaxErrors         int
	MaxCost           int
}

// FieldDefinition declares one selectable field.
type FieldDefinition struct {
	Name       string
	Type       ValueType
	Default    bool
	Required   bool
	Deprecated bool
	Cost       int
}

// FilterDefinition declares the only operations accepted for one filter.
type FilterDefinition struct {
	Name       string
	Type       ValueType
	Operators  []Operator
	Protected  bool
	Deprecated bool
	Nullable   bool
	AllowEmpty bool
	Cost       int
}

// PaginationDefinition explicitly enables bounded page modes.
type PaginationDefinition struct {
	Cursor          bool
	Offset          bool
	DefaultPageSize int
	MaxOffset       int
}

// SortDefinition declares one sortable field and its ordering role.
type SortDefinition struct {
	Name       string
	Type       ValueType
	TieBreaker bool
	Nulls      NullOrder
	Cost       int
}

// RelationshipDefinition declares one includable edge.
type RelationshipDefinition struct {
	Name          string
	Resource      string
	Cost          int
	Relationships []RelationshipDefinition
}

// SchemaConfig is mutable caller input used only while constructing a Schema.
type SchemaConfig struct {
	Resource      string
	Revision      string
	Fields        []FieldDefinition
	Filters       []FilterDefinition
	Sorts         []SortDefinition
	Relationships []RelationshipDefinition
	DefaultSort   []SortTerm
	AllowedLogic  []Logic
	Pagination    PaginationDefinition
	Bounds        Bounds
}

// Schema is an immutable server-declared query capability set.
type Schema struct {
	resource      string
	revision      string
	fields        []FieldDefinition
	fieldIndex    map[string]int
	filters       []FilterDefinition
	filterIndex   map[string]int
	sorts         []SortDefinition
	sortIndex     map[string]int
	relationships []RelationshipDefinition
	relationIndex map[string]relationshipNode
	defaultSort   []SortTerm
	allowedLogic  map[Logic]struct{}
	bounds        Bounds
	pagination    PaginationDefinition
}

// NewSchema validates and defensively copies a declared schema.
func NewSchema(config SchemaConfig) (*Schema, error) {
	collector := violationCollector{limit: normalizedBounds(config.Bounds).MaxErrors}
	validateDeclaredBounds(config.Bounds, &collector)
	if !validName(config.Resource) {
		collector.add(CodeInvalidElement, "schema.resource", "resource must be valid and non-empty")
	}
	if config.Revision == "" || !utf8.ValidString(config.Revision) {
		collector.add(CodeInvalidElement, "schema.revision", "revision must be valid and non-empty")
	}
	schema := &Schema{
		resource: config.Resource, revision: config.Revision,
		fields:  append([]FieldDefinition(nil), config.Fields...),
		filters: cloneFilters(config.Filters), sorts: append([]SortDefinition(nil), config.Sorts...),
		relationships: append([]RelationshipDefinition(nil), config.Relationships...),
		defaultSort:   append([]SortTerm(nil), config.DefaultSort...),
		fieldIndex:    make(map[string]int, len(config.Fields)),
		filterIndex:   make(map[string]int, len(config.Filters)),
		sortIndex:     make(map[string]int, len(config.Sorts)),
		relationIndex: make(map[string]relationshipNode, len(config.Relationships)),
		allowedLogic:  make(map[Logic]struct{}, len(config.AllowedLogic)),
		bounds:        normalizedBounds(config.Bounds),
		pagination:    config.Pagination,
	}
	allowedLogic := config.AllowedLogic
	if len(allowedLogic) == 0 {
		allowedLogic = []Logic{LogicAnd}
	}
	for index, logic := range allowedLogic {
		path := fmt.Sprintf("schema.allowed_logic[%d]", index)
		if logic != LogicAnd && logic != LogicOr && logic != LogicNot {
			collector.add(CodeInvalidElement, path, "logical operation is invalid")
			continue
		}
		if _, duplicate := schema.allowedLogic[logic]; duplicate {
			collector.add(CodeConflict, path, "logical operation is duplicated")
			continue
		}
		schema.allowedLogic[logic] = struct{}{}
	}
	validateDefinitions(schema, &collector)
	validatePagination(schema, &collector)
	if err := collector.err(); err != nil {
		return nil, err
	}
	return schema, nil
}

// Bounds returns the normalized immutable limits used by the schema. Transport
// adapters should use MaxRequestBytes as their decode limit.
func (s *Schema) Bounds() Bounds { return s.bounds }

func cloneFilters(filters []FilterDefinition) []FilterDefinition {
	result := append([]FilterDefinition(nil), filters...)
	for index := range result {
		result[index].Operators = append([]Operator(nil), result[index].Operators...)
	}
	return result
}

func normalizedBounds(bounds Bounds) Bounds {
	defaults := Bounds{MaxRequestBytes: 16 << 10, MaxFields: 64,
		MaxIncludes: 16, MaxIncludeDepth: 4, MaxFilterDepth: 8,
		MaxFilterNodes: 128, MaxValues: 256, MaxMembership: 100,
		MaxStringBytes: 4096, MaxSorts: 8, MaxPageSize: 100,
		MaxCursorBytes: 4096, MaxCanonicalBytes: 64 << 10,
		MaxErrors: 16, MaxCost: 1000}
	values := []*int{&bounds.MaxRequestBytes, &bounds.MaxFields, &bounds.MaxIncludes,
		&bounds.MaxIncludeDepth, &bounds.MaxFilterDepth, &bounds.MaxFilterNodes,
		&bounds.MaxValues, &bounds.MaxMembership, &bounds.MaxStringBytes,
		&bounds.MaxSorts, &bounds.MaxPageSize, &bounds.MaxCursorBytes,
		&bounds.MaxCanonicalBytes, &bounds.MaxErrors, &bounds.MaxCost}
	fallbacks := []int{defaults.MaxRequestBytes, defaults.MaxFields, defaults.MaxIncludes,
		defaults.MaxIncludeDepth, defaults.MaxFilterDepth, defaults.MaxFilterNodes,
		defaults.MaxValues, defaults.MaxMembership, defaults.MaxStringBytes,
		defaults.MaxSorts, defaults.MaxPageSize, defaults.MaxCursorBytes,
		defaults.MaxCanonicalBytes, defaults.MaxErrors, defaults.MaxCost}
	for index, value := range values {
		if *value <= 0 {
			*value = fallbacks[index]
		}
	}
	return bounds
}

func validateDeclaredBounds(bounds Bounds, collector *violationCollector) {
	values := []int{bounds.MaxRequestBytes, bounds.MaxFields, bounds.MaxIncludes,
		bounds.MaxIncludeDepth, bounds.MaxFilterDepth, bounds.MaxFilterNodes,
		bounds.MaxValues, bounds.MaxMembership, bounds.MaxStringBytes,
		bounds.MaxSorts, bounds.MaxPageSize, bounds.MaxCursorBytes,
		bounds.MaxCanonicalBytes, bounds.MaxErrors, bounds.MaxCost}
	for index, value := range values {
		if value < 0 {
			collector.add(CodeInvalidElement, fmt.Sprintf("schema.bounds[%d]", index),
				"bound must not be negative")
		}
	}
}

func validateDefinitions(schema *Schema, collector *violationCollector) {
	for index, field := range schema.fields {
		path := fmt.Sprintf("schema.fields[%d]", index)
		if !validName(field.Name) || !validType(field.Type) {
			collector.add(CodeInvalidElement, path, "field name and type must be valid")
		}
		if field.Cost < 0 || field.Cost > schema.bounds.MaxCost {
			collector.add(CodeInvalidElement, path, "field cost is outside its bounds")
		}
		if _, duplicate := schema.fieldIndex[field.Name]; duplicate {
			collector.add(CodeConflict, path, "field is duplicated")
		} else {
			schema.fieldIndex[field.Name] = index
		}
	}
	for index, filter := range schema.filters {
		path := fmt.Sprintf("schema.filters[%d]", index)
		if !validName(filter.Name) || !validType(filter.Type) || len(filter.Operators) == 0 {
			collector.add(CodeInvalidElement, path, "filter declaration is invalid")
		}
		if filter.Cost < 0 || filter.Cost > schema.bounds.MaxCost {
			collector.add(CodeInvalidElement, path, "filter cost is outside its bounds")
		}
		seenOperators := make(map[Operator]struct{}, len(filter.Operators))
		for _, operator := range filter.Operators {
			if _, duplicate := seenOperators[operator]; duplicate {
				collector.add(CodeConflict, path, "filter operator is duplicated")
			}
			seenOperators[operator] = struct{}{}
			if !operatorSupportsType(operator, filter.Type, filter.Nullable) {
				collector.add(CodeUnsupported, path, "filter operator does not support its type")
			}
		}
		if _, duplicate := schema.filterIndex[filter.Name]; duplicate {
			collector.add(CodeConflict, path, "filter is duplicated")
		} else {
			schema.filterIndex[filter.Name] = index
		}
	}
	for index, sort := range schema.sorts {
		path := fmt.Sprintf("schema.sorts[%d]", index)
		if !validName(sort.Name) || !validType(sort.Type) {
			collector.add(CodeInvalidElement, path, "sort declaration is invalid")
		}
		if sort.Nulls != "" && sort.Nulls != NullsFirst && sort.Nulls != NullsLast {
			collector.add(CodeInvalidElement, path, "sort null ordering is invalid")
		}
		if sort.Cost < 0 || sort.Cost > schema.bounds.MaxCost {
			collector.add(CodeInvalidElement, path, "sort cost is outside its bounds")
		}
		if _, duplicate := schema.sortIndex[sort.Name]; duplicate {
			collector.add(CodeConflict, path, "sort is duplicated")
		} else {
			schema.sortIndex[sort.Name] = index
		}
	}
	ancestors := map[string]struct{}{schema.resource: {}}
	indexRelationships(schema, schema.relationships, "", "schema.relationships", ancestors, collector)
}

func operatorSupportsType(operator Operator, valueType ValueType, nullable bool) bool {
	switch operator {
	case OpEqual, OpNotEqual, OpIn, OpNotIn:
		return true
	case OpLess, OpLessOrEqual, OpGreater, OpGreaterOrEqual, OpBetween:
		return valueType != TypeBool && valueType != TypeBytes
	case OpIsNull:
		return nullable
	case OpContains, OpStartsWith, OpEndsWith:
		return valueType == TypeString
	default:
		return false
	}
}

type relationshipNode struct {
	definition RelationshipDefinition
	depth      int
}

func indexRelationships(schema *Schema, relationships []RelationshipDefinition, prefix, schemaPath string, ancestors map[string]struct{}, collector *violationCollector) {
	for index, relation := range relationships {
		path := fmt.Sprintf("%s[%d]", schemaPath, index)
		fullName := relation.Name
		if prefix != "" {
			fullName = prefix + "." + relation.Name
		}
		if !validName(relation.Name) || !validName(relation.Resource) {
			collector.add(CodeInvalidElement, path, "relationship declaration is invalid")
		}
		if relation.Cost < 0 || relation.Cost > schema.bounds.MaxCost {
			collector.add(CodeInvalidElement, path, "relationship cost is outside its bounds")
		}
		if _, duplicate := schema.relationIndex[fullName]; duplicate {
			collector.add(CodeConflict, path, "relationship is duplicated")
		} else {
			schema.relationIndex[fullName] = relationshipNode{
				definition: cloneRelationship(relation), depth: relationshipDepth(fullName),
			}
		}
		if _, cycle := ancestors[relation.Resource]; cycle {
			collector.add(CodeConflict, path, "relationship creates a resource cycle")
			continue
		}
		nextAncestors := make(map[string]struct{}, len(ancestors)+1)
		for resource := range ancestors {
			nextAncestors[resource] = struct{}{}
		}
		nextAncestors[relation.Resource] = struct{}{}
		indexRelationships(schema, relation.Relationships, fullName,
			path+".relationships", nextAncestors, collector)
	}
}

func validatePagination(schema *Schema, collector *violationCollector) {
	if len(schema.defaultSort) > schema.bounds.MaxSorts {
		collector.add(CodeInvalidElement, "schema.default_sort", "default sort exceeds the sort limit")
	}
	if schema.pagination.Cursor || schema.pagination.Offset {
		if schema.pagination.DefaultPageSize <= 0 ||
			schema.pagination.DefaultPageSize > schema.bounds.MaxPageSize {
			collector.add(CodeInvalidElement, "schema.pagination", "default page size is outside its bounds")
		}
		if schema.pagination.Offset && schema.pagination.MaxOffset < 0 {
			collector.add(CodeInvalidElement, "schema.pagination", "maximum offset is outside its bounds")
		}
		if schema.pagination.Cursor {
			tieBreakers := 0
			for _, sort := range schema.sorts {
				if sort.TieBreaker {
					tieBreakers++
				}
			}
			if tieBreakers != 1 {
				collector.add(CodeConflict, "schema.pagination", "cursor pagination requires one tie-breaker")
			}
		}
	}
	seen := make(map[string]struct{}, len(schema.defaultSort))
	defaultHasTieBreaker := false
	for index, term := range schema.defaultSort {
		path := fmt.Sprintf("schema.default_sort[%d]", index)
		if _, exists := schema.sortIndex[term.Name]; !exists ||
			(term.Direction != Ascending && term.Direction != Descending) {
			collector.add(CodeInvalidElement, path, "default sort is invalid")
		}
		if term.Nulls != "" && term.Nulls != NullsFirst && term.Nulls != NullsLast {
			collector.add(CodeInvalidElement, path, "default sort null ordering is invalid")
		} else if definitionIndex, exists := schema.sortIndex[term.Name]; exists &&
			term.Nulls != "" && term.Nulls != schema.sorts[definitionIndex].Nulls {
			collector.add(CodeUnsupported, path, "default sort null ordering is not available")
		}
		if _, duplicate := seen[term.Name]; duplicate {
			collector.add(CodeConflict, path, "default sort is duplicated")
		}
		if definitionIndex, exists := schema.sortIndex[term.Name]; exists &&
			schema.sorts[definitionIndex].TieBreaker {
			defaultHasTieBreaker = true
		}
		seen[term.Name] = struct{}{}
	}
	if schema.pagination.Cursor && len(schema.defaultSort) >= schema.bounds.MaxSorts &&
		!defaultHasTieBreaker {
		collector.add(CodeConflict, "schema.default_sort", "default cursor sort leaves no room for its tie-breaker")
	}
}

func cloneRelationship(relation RelationshipDefinition) RelationshipDefinition {
	relation.Relationships = append([]RelationshipDefinition(nil), relation.Relationships...)
	for index := range relation.Relationships {
		relation.Relationships[index] = cloneRelationship(relation.Relationships[index])
	}
	return relation
}

func relationshipDepth(path string) int {
	depth := 1
	for _, char := range path {
		if char == '.' {
			depth++
		}
	}
	return depth
}

func validName(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for index, char := range value {
		if (char < 'a' || char > 'z') && (index == 0 || char < '0' || char > '9') &&
			(index == 0 || char != '_') {
			return false
		}
	}
	return true
}

func validType(value ValueType) bool {
	switch value {
	case TypeString, TypeInt, TypeUint, TypeFloat, TypeBool, TypeTime, TypeBytes:
		return true
	case TypeNull:
		return false
	default:
		return false
	}
}
