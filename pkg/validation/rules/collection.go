package rules

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// SliceSize requires an inclusive slice-size range.
func SliceSize[T any](minimum, maximum int) validation.Validator[[]T] {
	return validation.ValidatorFunc[[]T](func(ctx validation.Context, values []T) validation.Report {
		if len(values) >= minimum && len(values) <= maximum {
			return pass(ctx)
		}
		return fail(ctx, "size", sizeParameters(minimum, maximum))
	})
}

// MapSize requires an inclusive map-size range.
func MapSize[K comparable, V any](minimum, maximum int) validation.Validator[map[K]V] {
	return validation.ValidatorFunc[map[K]V](func(ctx validation.Context, values map[K]V) validation.Report {
		size := len(values)
		if size >= minimum && size <= maximum {
			return pass(ctx)
		}
		return fail(ctx, "size", sizeParameters(minimum, maximum))
	})
}

func sizeParameters(minimum, maximum int) map[string]string {
	return map[string]string{"minimum": strconv.Itoa(minimum), "maximum": strconv.Itoa(maximum)}
}

// Unique requires every comparable slice item to occur once.
func Unique[T comparable]() validation.Validator[[]T] {
	checkStringLimit := isStringType[T]()
	return validation.ValidatorFunc[[]T](func(ctx validation.Context, values []T) validation.Report {
		if len(values) > ctx.Limits().MaxCollectionSize {
			return fail(ctx, "collection_limit", nil)
		}
		seen := make(map[T]struct{}, len(values))
		for index, value := range values {
			if checkStringLimit && valueExceedsStringLimit(ctx, value) {
				return fail(ctx.WithPath(validation.Index(index)),
					"string_limit", nil)
			}
			if _, exists := seen[value]; exists {
				return fail(ctx.WithPath(validation.Index(index)), "unique", nil)
			}
			seen[value] = struct{}{}
		}
		return pass(ctx)
	})
}

// Items applies a validator to slice items in index order.
func Items[T any](mode validation.Mode, validator validation.Validator[T]) validation.Validator[[]T] {
	return validation.ValidatorFunc[[]T](func(ctx validation.Context, values []T) validation.Report {
		if len(values) > ctx.Limits().MaxCollectionSize {
			return fail(ctx, "collection_limit", nil)
		}
		report := pass(ctx)
		for index, value := range values {
			current := validator.Validate(ctx.WithPath(validation.Index(index)), value)
			report = report.Merge(current)
			if mode == validation.ShortCircuit && current.Err() != nil {
				break
			}
		}
		return report
	})
}

// Keys validates ordered map keys in sorted order for deterministic reports.
func Keys[K cmp.Ordered, V any](mode validation.Mode,
	validator validation.Validator[K],
) validation.Validator[map[K]V] {
	checkStringLimit := isStringType[K]()
	return validation.ValidatorFunc[map[K]V](func(ctx validation.Context, values map[K]V) validation.Report {
		if len(values) > ctx.Limits().MaxCollectionSize {
			return fail(ctx, "collection_limit", nil)
		}
		keys := make([]K, 0, len(values))
		oversizedKey := false
		for key := range values {
			if checkStringLimit && valueExceedsStringLimit(ctx, key) {
				oversizedKey = true
			}
			keys = append(keys, key)
		}
		if oversizedKey {
			return fail(ctx, "string_limit", nil)
		}
		slices.Sort(keys)
		report := pass(ctx)
		for _, key := range keys {
			current := validator.Validate(ctx.WithPath(validation.Key(fmt.Sprint(key))), key)
			report = report.Merge(current)
			if mode == validation.ShortCircuit && current.Err() != nil {
				break
			}
		}
		return report
	})
}

// Values validates map values in sorted key order and locates each finding at
// its typed key segment.
func Values[K cmp.Ordered, V any](mode validation.Mode,
	validator validation.Validator[V],
) validation.Validator[map[K]V] {
	checkStringLimit := isStringType[K]()
	return validation.ValidatorFunc[map[K]V](func(ctx validation.Context, values map[K]V) validation.Report {
		if len(values) > ctx.Limits().MaxCollectionSize {
			return fail(ctx, "collection_limit", nil)
		}
		keys := make([]K, 0, len(values))
		oversizedKey := false
		for key := range values {
			if checkStringLimit && valueExceedsStringLimit(ctx, key) {
				oversizedKey = true
			}
			keys = append(keys, key)
		}
		if oversizedKey {
			return fail(ctx, "string_limit", nil)
		}
		slices.Sort(keys)
		report := pass(ctx)
		for _, key := range keys {
			current := validator.Validate(
				ctx.WithPath(validation.Key(fmt.Sprint(key))), values[key],
			)
			report = report.Merge(current)
			if mode == validation.ShortCircuit && current.Err() != nil {
				break
			}
		}
		return report
	})
}
