package validation

// Validator is the deterministic, side-effect-free validation contract.
type Validator[T any] interface {
	Validate(Context, T) Report
}

// ValidatorFunc adapts an ordinary function to Validator.
type ValidatorFunc[T any] func(Context, T) Report

// Validate calls the underlying validation function.
func (f ValidatorFunc[T]) Validate(ctx Context, value T) (report Report) {
	defer func() {
		if recover() != nil {
			report = panicReport(ctx)
		}
	}()
	return f(ctx, value)
}

// IsolatePanics explicitly wraps a custom validator with panic containment.
// The panic payload and rejected value are deliberately discarded.
func IsolatePanics[T any](validator Validator[T]) Validator[T] {
	return ValidatorFunc[T](func(ctx Context, value T) (report Report) {
		defer func() {
			if recover() != nil {
				report = panicReport(ctx)
			}
		}()
		return validator.Validate(ctx, value)
	})
}

func panicReport(ctx Context) Report {
	return NewReport(ctx.Limits()).Add(NewViolation(
		ctx.Path(), "validator_panic", Error, nil, ErrValidatorPanic,
	))
}
