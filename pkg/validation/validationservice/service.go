// Package validationservice provides transport-neutral service hook contracts.
package validationservice

import (
	"context"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Validator is a cancellation-aware service-boundary validation contract.
type Validator[T any] interface {
	Validate(context.Context, validation.Context, T) validation.Report
}

// Hook adapts a service-boundary function to Validator.
type Hook[T any] func(context.Context, validation.Context, T) validation.Report

// Validate invokes the service hook.
func (hook Hook[T]) Validate(ctx context.Context,
	validationContext validation.Context, value T,
) validation.Report {
	return hook(ctx, validationContext, value)
}

// Chain evaluates service hooks in declaration order.
func Chain[T any](mode validation.Mode, validators ...Validator[T]) Validator[T] {
	return Hook[T](func(ctx context.Context,
		validationContext validation.Context, value T,
	) validation.Report {
		report := validation.NewReport(validationContext.Limits())
		for _, validator := range validators {
			if validator == nil {
				continue
			}
			current := validator.Validate(ctx, validationContext, value)
			report = report.Merge(current)
			if mode == validation.ShortCircuit && current.Err() != nil {
				break
			}
		}
		return report
	})
}
