package validation

// Mode controls whether composition stops after a decisive result.
type Mode uint8

const (
	// ShortCircuit stops after the first decisive failure or success.
	ShortCircuit Mode = iota + 1
	// CollectAll evaluates every relevant validator in declaration order.
	CollectAll
)

// All requires every validator to pass.
func All[T any](mode Mode, validators ...Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) Report {
		report := NewReport(ctx.Limits())
		for _, validator := range validators {
			if validator == nil {
				continue
			}
			current := validator.Validate(ctx, value)
			report = report.Merge(current)
			if mode == ShortCircuit && current.Err() != nil {
				break
			}
		}
		return report
	})
}

// Any requires at least one validator to pass. Failed alternatives are
// returned only when every alternative fails.
func Any[T any](mode Mode, validators ...Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) Report {
		failures := NewReport(ctx.Limits())
		successes := NewReport(ctx.Limits())
		passed := false
		for _, validator := range validators {
			if validator == nil {
				continue
			}
			current := validator.Validate(ctx, value)
			if current.Err() == nil {
				passed = true
				successes = successes.Merge(current)
				if mode == ShortCircuit {
					break
				}
				continue
			}
			failures = failures.Merge(current)
		}
		if passed {
			return successes
		}
		return failures
	})
}

// Not passes only when validator fails.
func Not[T any](validator Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) Report {
		if validator != nil && validator.Validate(ctx, value).Err() != nil {
			return NewReport(ctx.Limits())
		}
		return NewReport(ctx.Limits()).Add(NewViolation(
			ctx.Path(), "not", Error, nil, nil,
		))
	})
}

// When chooses a validator using a deterministic typed predicate.
func When[T any](predicate func(T) bool, then, otherwise Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) Report {
		selected := otherwise
		if predicate != nil && predicate(value) {
			selected = then
		}
		if selected == nil {
			return NewReport(ctx.Limits())
		}
		return selected.Validate(ctx, value)
	})
}

// Dependent runs dependent only when prerequisite has no blocking errors.
func Dependent[T any](prerequisite, dependent Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) Report {
		report := NewReport(ctx.Limits())
		if prerequisite != nil {
			report = prerequisite.Validate(ctx, value)
			if report.Err() != nil {
				return report
			}
		}
		if dependent == nil {
			return report
		}
		return report.Merge(dependent.Validate(ctx, value))
	})
}
