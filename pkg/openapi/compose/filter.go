// Package compose provides bounded, immutable OpenAPI composition operations.
package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var (
	// ErrInvalidInput reports nil filtering inputs.
	ErrInvalidInput = errors.New("invalid OpenAPI composition input")
	// ErrInvalidOptions reports unusable filtering limits.
	ErrInvalidOptions = errors.New("invalid OpenAPI composition options")
	// ErrLimitExceeded reports filtering work beyond caller policy.
	ErrLimitExceeded = errors.New("OpenAPI composition limit exceeded")
)

// Operation is an immutable operation and its source provenance.
type Operation struct {
	pointer string
	method  string
	source  jsonvalue.Value
}

// Pointer returns the escaped JSON Pointer in the source document.
func (operation Operation) Pointer() string {
	return operation.pointer
}

// Method returns the dialect-defined operation field or additional method.
func (operation Operation) Method() string {
	return operation.method
}

// Source returns the immutable source Operation Object.
func (operation Operation) Source() jsonvalue.Value {
	return operation.source
}

// Predicate decides whether an operation remains in the result.
type Predicate func(Operation) (bool, error)

// FilterOptions bounds independently growing filtering work.
type FilterOptions struct {
	MaxOperations int
	MaxDepth      int
}

// DefaultFilterOptions returns conservative untrusted-document bounds.
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{MaxOperations: 100_000, MaxDepth: 256}
}

// FilterResult owns the filtered document and removal provenance.
type FilterResult struct {
	document openapi.Document
	removed  []Operation
}

// Document returns the immutable filtered document.
func (result FilterResult) Document() openapi.Document {
	return result.document
}

// Removed returns caller-owned removal provenance in source traversal order.
func (result FilterResult) Removed() []Operation {
	return append([]Operation(nil), result.removed...)
}

// FilterOperations retains operations accepted by predicate across paths,
// webhooks, component path items, and callbacks. It does not resolve reference
// targets; local operation siblings remain filterable without following them.
func FilterOperations(
	ctx context.Context,
	document openapi.Document,
	predicate Predicate,
	options FilterOptions,
) (FilterResult, error) {
	if ctx == nil || document == nil || predicate == nil {
		return FilterResult{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return FilterResult{}, err
	}
	if options.MaxOperations < 0 || options.MaxDepth < 0 {
		return FilterResult{}, ErrInvalidOptions
	}
	defaults := DefaultFilterOptions()
	if options.MaxOperations == 0 {
		options.MaxOperations = defaults.MaxOperations
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaults.MaxDepth
	}
	filter := operationFilter{
		ctx:       ctx,
		dialect:   document.SpecificationVersion().Dialect(),
		predicate: predicate,
		options:   options,
	}
	root, err := filter.root(document.Raw(), 1)
	if err != nil {
		return FilterResult{}, err
	}
	// Filtering only removes operation members and therefore preserves the
	// already decoded root marker invariant.
	filtered, _ := openapi.Decode(root)
	return FilterResult{document: filtered, removed: filter.removed}, nil
}

type operationFilter struct {
	ctx       context.Context
	dialect   specversion.Dialect
	predicate Predicate
	options   FilterOptions
	visited   int
	removed   []Operation
}

func (filter *operationFilter) root(value jsonvalue.Value, depth int) (jsonvalue.Value, error) {
	return filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		var transformed jsonvalue.Value
		var err error
		switch member.Name {
		case "paths":
			transformed, err = filter.pathItemCollection(member.Value, "/paths", nextDepth(depth), true)
		case "webhooks":
			if filter.dialect != specversion.DialectOAS31 &&
				filter.dialect != specversion.DialectOAS32 {
				return member, nil
			}
			transformed, err = filter.pathItemCollection(member.Value, "/webhooks", nextDepth(depth), false)
		case "components":
			if filter.dialect == specversion.DialectSwagger20 {
				return member, nil
			}
			transformed, err = filter.components(member.Value, "/components", nextDepth(depth))
		default:
			return member, nil
		}
		member.Value = transformed
		return member, err
	})
}

func (filter *operationFilter) components(
	value jsonvalue.Value,
	pointer string,
	depth int,
) (jsonvalue.Value, error) {
	return filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		var transformed jsonvalue.Value
		var err error
		switch member.Name {
		case "pathItems":
			if filter.dialect == specversion.DialectOAS30 {
				return member, nil
			}
			transformed, err = filter.pathItemCollection(
				member.Value, pointer+"/pathItems", nextDepth(depth), false,
			)
		case "callbacks":
			transformed, err = filter.callbackCollection(
				member.Value, pointer+"/callbacks", nextDepth(depth),
			)
		default:
			return member, nil
		}
		member.Value = transformed
		return member, err
	})
}

func (filter *operationFilter) pathItemCollection(
	value jsonvalue.Value,
	pointer string,
	depth int,
	requirePath bool,
) (jsonvalue.Value, error) {
	return filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") ||
			(requirePath && !strings.HasPrefix(member.Name, "/")) ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			return member, nil
		}
		transformed, err := filter.pathItem(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
			nextDepth(depth),
		)
		member.Value = transformed
		return member, err
	})
}

func (filter *operationFilter) pathItem(
	value jsonvalue.Value,
	pointer string,
	depth int,
) (jsonvalue.Value, error) {
	if err := filter.bound(depth); err != nil {
		return jsonvalue.Value{}, err
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if err := filter.ctx.Err(); err != nil {
			return jsonvalue.Value{}, err
		}
		if filter.standardMethod(member.Name) && member.Value.Kind() == jsonvalue.ObjectKind {
			kept, transformed, err := filter.operation(
				member.Value, pointer+"/"+escapePointer(member.Name), member.Name, nextDepth(depth),
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if kept {
				member.Value = transformed
				result = append(result, member)
			}
			continue
		}
		if member.Name == "additionalOperations" &&
			filter.dialect == specversion.DialectOAS32 &&
			member.Value.Kind() == jsonvalue.ObjectKind {
			transformed, err := filter.additionalOperations(
				member.Value, pointer+"/additionalOperations", nextDepth(depth),
			)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			member.Value = transformed
		}
		result = append(result, member)
	}
	return jsonvalue.Object(result)
}

func (filter *operationFilter) additionalOperations(
	value jsonvalue.Value,
	pointer string,
	depth int,
) (jsonvalue.Value, error) {
	if err := filter.bound(depth); err != nil {
		return jsonvalue.Value{}, err
	}
	members, _ := value.Members()
	result := make([]jsonvalue.Member, 0, len(members))
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind {
			result = append(result, member)
			continue
		}
		kept, transformed, err := filter.operation(
			member.Value, pointer+"/"+escapePointer(member.Name), member.Name, nextDepth(depth),
		)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if kept {
			member.Value = transformed
			result = append(result, member)
		}
	}
	return jsonvalue.Object(result)
}

func (filter *operationFilter) operation(
	value jsonvalue.Value,
	pointer string,
	method string,
	depth int,
) (bool, jsonvalue.Value, error) {
	if err := filter.bound(depth); err != nil {
		return false, jsonvalue.Value{}, err
	}
	filter.visited++
	if filter.visited > filter.options.MaxOperations {
		return false, jsonvalue.Value{}, ErrLimitExceeded
	}
	operation := Operation{pointer: pointer, method: method, source: value}
	keep, err := filter.predicate(operation)
	if err != nil {
		return false, jsonvalue.Value{}, fmt.Errorf("filter operation %s: %w", pointer, err)
	}
	if !keep {
		filter.removed = append(filter.removed, operation)
		return false, jsonvalue.Value{}, nil
	}
	transformed, err := filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		if member.Name != "callbacks" {
			return member, nil
		}
		callbacks, err := filter.callbackCollection(
			member.Value, pointer+"/callbacks", nextDepth(depth),
		)
		member.Value = callbacks
		return member, err
	})
	return true, transformed, err
}

func (filter *operationFilter) callbackCollection(
	value jsonvalue.Value,
	pointer string,
	depth int,
) (jsonvalue.Value, error) {
	return filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			return member, nil
		}
		if _, reference := member.Value.Lookup("$ref"); reference {
			return member, nil
		}
		transformed, err := filter.callback(
			member.Value, pointer+"/"+escapePointer(member.Name), nextDepth(depth),
		)
		member.Value = transformed
		return member, err
	})
}

func (filter *operationFilter) callback(
	value jsonvalue.Value,
	pointer string,
	depth int,
) (jsonvalue.Value, error) {
	return filter.transformObject(value, depth, func(member jsonvalue.Member) (jsonvalue.Member, error) {
		if strings.HasPrefix(strings.ToLower(member.Name), "x-") ||
			member.Value.Kind() != jsonvalue.ObjectKind {
			return member, nil
		}
		transformed, err := filter.pathItem(
			member.Value, pointer+"/"+escapePointer(member.Name), nextDepth(depth),
		)
		member.Value = transformed
		return member, err
	})
}

func (filter *operationFilter) transformObject(
	value jsonvalue.Value,
	depth int,
	transform func(jsonvalue.Member) (jsonvalue.Member, error),
) (jsonvalue.Value, error) {
	if value.Kind() != jsonvalue.ObjectKind {
		return value, nil
	}
	if err := filter.bound(depth); err != nil {
		return jsonvalue.Value{}, err
	}
	members, _ := value.Members()
	for index, member := range members {
		if err := filter.ctx.Err(); err != nil {
			return jsonvalue.Value{}, err
		}
		transformed, err := transform(member)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members[index] = transformed
	}
	return jsonvalue.Object(members)
}

func (filter *operationFilter) standardMethod(name string) bool {
	switch name {
	case "get", "put", "post", "delete", "options", "head", "patch":
		return true
	case "trace":
		return filter.dialect != specversion.DialectSwagger20
	case "query":
		return filter.dialect == specversion.DialectOAS32
	default:
		return false
	}
}

func (filter *operationFilter) bound(depth int) error {
	if err := filter.ctx.Err(); err != nil {
		return err
	}
	if depth > filter.options.MaxDepth {
		return ErrLimitExceeded
	}
	return nil
}

func escapePointer(value string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(value)
}

func nextDepth(depth int) int {
	return depth + 1
}
