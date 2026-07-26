// Package merge combines configuration trees without mutating its inputs.
package merge

import (
	"fmt"
	"reflect"
	"sort"
)

// Delete explicitly removes a key inherited from a lower-precedence object.
type Delete struct{}

// TypeConflictError reports an incompatible change between configuration
// layers. Null and Delete are explicit operations and do not cause conflicts.
type TypeConflictError struct {
	Path  string
	Lower string
	Upper string
}

func (e *TypeConflictError) Error() string {
	return fmt.Sprintf(
		"config merge at %q: cannot replace %s with %s",
		e.Path,
		e.Lower,
		e.Upper,
	)
}

// Trees merges lower and upper configuration objects. Objects merge
// recursively; all other values replace. Incompatible type changes fail.
func Trees(lower, upper map[string]any) (map[string]any, error) {
	merged := cloneMap(lower)
	if err := mergeMap(merged, upper, ""); err != nil {
		return nil, err
	}

	return merged, nil
}

func mergeMap(destination, upper map[string]any, parent string) error {
	keys := make([]string, 0, len(upper))
	for key := range upper {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		upperValue := upper[key]
		path := joinPath(parent, key)
		if _, deleted := upperValue.(Delete); deleted {
			delete(destination, key)
			continue
		}

		lowerValue, exists := destination[key]
		if !exists || upperValue == nil {
			destination[key] = cloneValue(upperValue)
			continue
		}

		lowerMap, lowerIsMap := lowerValue.(map[string]any)
		upperMap, upperIsMap := upperValue.(map[string]any)
		if lowerIsMap && upperIsMap {
			merged := cloneMap(lowerMap)
			if err := mergeMap(merged, upperMap, path); err != nil {
				return err
			}
			destination[key] = merged
			continue
		}

		if lowerValue != nil && kind(lowerValue) != kind(upperValue) {
			return &TypeConflictError{
				Path:  path,
				Lower: kind(lowerValue),
				Upper: kind(upperValue),
			}
		}

		destination[key] = cloneValue(upperValue)
	}

	return nil
}

func cloneMap(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = cloneValue(item)
	}

	return clone
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		clone := make([]any, len(value))
		for index, item := range value {
			clone[index] = cloneValue(item)
		}
		return clone
	default:
		return value
	}
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}

	return parent + "." + key
}

func kind(value any) string {
	if _, ok := value.(map[string]any); ok {
		return "object"
	}
	if reflect.TypeOf(value).Kind() == reflect.Slice {
		return "slice"
	}

	return reflect.TypeOf(value).String()
}
