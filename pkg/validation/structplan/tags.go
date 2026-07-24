package structplan

import (
	"fmt"
	"math"
	"net/mail"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	validation "github.com/faustbrian/golib/pkg/validation"
)

type tagRule struct {
	name      string
	parameter int
}

type compiledField struct {
	index []int
	path  []string
	rules []tagRule
}

// TagPlan is an immutable startup-compiled reflective plan.
type TagPlan[T any] struct {
	limits validation.Limits
	fields []compiledField
}

// CompileTags strictly compiles validate tags for T.
func CompileTags[T any](limits validation.Limits) (*TagPlan[T], error) {
	if _, err := validation.NewContext(limits); err != nil {
		return nil, fmt.Errorf("compile tag plan: %w", err)
	}
	typeOf := reflect.TypeFor[T]()
	if typeOf.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: root %s", ErrUnsupportedKind, typeOf.Kind())
	}
	fields := make([]compiledField, 0)
	visited := 0
	if err := compileType(typeOf, nil, nil, 0, limits,
		make(map[reflect.Type]bool), &visited, &fields); err != nil {
		return nil, err
	}
	return &TagPlan[T]{limits: limits, fields: fields}, nil
}

func compileType(current reflect.Type, indexes []int, path []string, depth int,
	limits validation.Limits, stack map[reflect.Type]bool,
	visited *int, fields *[]compiledField,
) error {
	if depth > limits.MaxDepth {
		return fmt.Errorf("%w: struct depth", validation.ErrLimitExceeded)
	}
	if stack[current] {
		return fmt.Errorf("%w: %s", ErrCycle, current)
	}
	stack[current] = true
	defer delete(stack, current)
	for position := range current.NumField() {
		if *visited >= limits.MaxStructFields {
			return fmt.Errorf("%w: struct fields", validation.ErrLimitExceeded)
		}
		*visited = *visited + 1
		field := current.Field(position)
		tag := field.Tag.Get("validate")
		if len(tag) > limits.MaxTagLength {
			return fmt.Errorf("%w: tag length", validation.ErrLimitExceeded)
		}
		if field.PkgPath != "" {
			if tag != "" && tag != "-" {
				return fmt.Errorf("%w: inaccessible field %s", ErrInvalidTag, field.Name)
			}
			continue
		}
		if tag == "-" {
			continue
		}
		fieldIndexes := appendCopy(indexes, position)
		fieldPath := appendCopy(path, field.Name)
		base := field.Type
		for base.Kind() == reflect.Pointer {
			base = base.Elem()
		}
		if tag == "" && base.Kind() == reflect.Struct {
			if err := compileType(base, fieldIndexes, fieldPath, depth+1,
				limits, stack, visited, fields); err != nil {
				return err
			}
			continue
		}
		if tag == "" {
			continue
		}
		rules, err := parseTag(tag)
		if err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
		*fields = append(*fields, compiledField{
			index: fieldIndexes, path: fieldPath, rules: rules,
		})
	}
	return nil
}

func appendCopy[T any](values []T, value T) []T {
	result := make([]T, len(values)+1)
	copy(result, values)
	result[len(values)] = value
	return result
}

func parseTag(tag string) ([]tagRule, error) {
	seen := make(map[string]struct{})
	parts := strings.Split(tag, ",")
	rules := make([]tagRule, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, ErrInvalidTag
		}
		name, parameterText, hasParameter := strings.Cut(part, "=")
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateRule, name)
		}
		seen[name] = struct{}{}
		rule := tagRule{name: name}
		switch name {
		case "required", "email":
			if hasParameter {
				return nil, ErrInvalidTag
			}
		case "min", "max":
			parameter, err := strconv.Atoi(parameterText)
			if !hasParameter || err != nil || parameter < 0 {
				return nil, ErrInvalidTag
			}
			rule.parameter = parameter
		default:
			return nil, fmt.Errorf("%w: %s", ErrUnknownRule, name)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// Validate evaluates precompiled fields in deterministic declaration order.
func (plan *TagPlan[T]) Validate(ctx validation.Context, value T) validation.Report {
	report := validation.NewReport(ctx.Limits())
	root := reflect.ValueOf(value)
	for _, field := range plan.fields {
		pathContext := ctx
		for _, name := range field.path {
			pathContext = pathContext.WithPath(validation.Field(name))
		}
		if len(pathContext.Path().String()) > ctx.Limits().MaxPathLength {
			report = report.Merge(tagFailure(pathContext, "path_limit"))
			continue
		}
		fieldValue := fieldByIndex(root, field.index)
		measuredValue := indirect(fieldValue)
		if measuredValue.IsValid() && isCollection(measuredValue.Kind()) &&
			measuredValue.Len() > ctx.Limits().MaxCollectionSize {
			report = report.Merge(tagFailure(pathContext, "collection_limit"))
			continue
		}
		if measuredValue.IsValid() && measuredValue.Kind() == reflect.String &&
			measuredValue.Len() > ctx.Limits().MaxStringLength {
			report = report.Merge(tagFailure(pathContext, "string_limit"))
			continue
		}
		for _, rule := range field.rules {
			if code := evaluateTagRule(rule, fieldValue); code != "" {
				report = report.Merge(tagFailure(pathContext, code))
			}
		}
	}
	return report
}

func fieldByIndex(value reflect.Value, indexes []int) reflect.Value {
	for _, index := range indexes {
		for value.IsValid() && value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return reflect.Value{}
			}
			value = value.Elem()
		}
		if !value.IsValid() || value.Kind() != reflect.Struct {
			return reflect.Value{}
		}
		value = value.Field(index)
	}
	return value
}

func evaluateTagRule(rule tagRule, value reflect.Value) string {
	if rule.name == "required" {
		if !value.IsValid() || isNil(value) {
			return "required"
		}
		for value.Kind() == reflect.Interface {
			value = value.Elem()
		}
		if reflectiveEmpty(value) {
			return "required"
		}
		return ""
	}
	if !value.IsValid() || isNil(value) {
		return ""
	}
	value = indirect(value)
	switch rule.name {
	case "email":
		if value.Kind() != reflect.String {
			return "email"
		}
		text := value.String()
		address, err := mail.ParseAddress(text)
		if err != nil || address.Address != text || !strings.Contains(text, "@") {
			return "email"
		}
	case "min":
		if !withinBound(value, rule.parameter, true) {
			return "min"
		}
	case "max":
		if !withinBound(value, rule.parameter, false) {
			return "max"
		}
	}
	return ""
}

func indirect(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer ||
		value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func reflectiveEmpty(value reflect.Value) bool {
	empty := false
	switch value.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		empty = value.Len() == 0
	case reflect.Invalid:
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8,
		reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Func, reflect.Interface, reflect.Pointer, reflect.Struct,
		reflect.UnsafePointer:
		empty = value.IsZero()
	}
	return empty
}

func isCollection(kind reflect.Kind) bool {
	return kind == reflect.Array || kind == reflect.Map || kind == reflect.Slice
}

func isNil(value reflect.Value) bool {
	result := false
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		result = value.IsNil()
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8,
		reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64,
		reflect.Complex128, reflect.Array, reflect.String, reflect.Struct,
		reflect.UnsafePointer:
	}
	return result
}

func withinBound(value reflect.Value, parameter int, minimum bool) bool {
	switch value.Kind() {
	case reflect.String:
		return compareBound(int64(utf8.RuneCountInString(value.String())),
			int64(parameter), minimum)
	case reflect.Array, reflect.Map, reflect.Slice:
		return compareBound(int64(value.Len()), int64(parameter), minimum)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return compareBound(value.Int(), int64(parameter), minimum)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bound, err := strconv.ParseUint(strconv.Itoa(parameter), 10, 64)
		return err == nil && compareBound(value.Uint(), bound, minimum)
	case reflect.Float32, reflect.Float64:
		measured := value.Float()
		return !math.IsNaN(measured) && compareBound(measured,
			float64(parameter), minimum)
	case reflect.Invalid, reflect.Bool, reflect.Uintptr, reflect.Complex64,
		reflect.Complex128, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Pointer, reflect.Struct, reflect.UnsafePointer:
	}
	return false
}

func compareBound[T int64 | uint64 | float64](value, bound T, minimum bool) bool {
	if minimum {
		return value >= bound
	}
	return value <= bound
}

func tagFailure(ctx validation.Context, code string) validation.Report {
	return validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		ctx.Path(), code, validation.Error, nil, nil,
	))
}
