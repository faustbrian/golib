// Package structplan provides optional typed and startup-compiled struct plans.
package structplan

import (
	"errors"
	"fmt"

	validation "github.com/faustbrian/golib/pkg/validation"
)

var (
	// ErrDuplicateField marks a repeated typed-plan field name.
	ErrDuplicateField = errors.New("duplicate validation field")
	// ErrUnknownRule marks an unsupported strict tag rule.
	ErrUnknownRule = errors.New("unknown validation rule")
	// ErrDuplicateRule marks a repeated rule in one tag.
	ErrDuplicateRule = errors.New("duplicate validation rule")
	// ErrInvalidTag marks malformed tag grammar or parameters.
	ErrInvalidTag = errors.New("invalid validation tag")
	// ErrCycle marks a recursive reflective struct graph.
	ErrCycle = errors.New("cyclic validation type")
	// ErrUnsupportedKind marks a type a tag rule cannot evaluate.
	ErrUnsupportedKind = errors.New("unsupported validation kind")
	// ErrInvalidPlan marks malformed typed plan construction.
	ErrInvalidPlan = errors.New("invalid validation plan")
)

type fieldPlan[T any] func(validation.Context, T) validation.Report

// Builder incrementally defines a reflection-free typed struct plan.
type Builder[T any] struct {
	limits validation.Limits
	fields []fieldPlan[T]
	names  map[string]struct{}
}

// New creates a typed plan builder.
func New[T any](limits validation.Limits) *Builder[T] {
	return &Builder[T]{limits: limits, names: make(map[string]struct{})}
}

// Add appends a named typed accessor and validator to builder.
func Add[T, V any](builder *Builder[T], name string, accessor func(T) V,
	validator validation.Validator[V],
) error {
	if builder == nil || name == "" || accessor == nil || validator == nil {
		return ErrInvalidPlan
	}
	if len(name) > builder.limits.MaxPathLength {
		return fmt.Errorf("%w: field path", validation.ErrLimitExceeded)
	}
	if _, exists := builder.names[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateField, name)
	}
	if len(builder.fields) >= builder.limits.MaxStructFields {
		return fmt.Errorf("%w: struct fields", validation.ErrLimitExceeded)
	}
	builder.names[name] = struct{}{}
	builder.fields = append(builder.fields, func(ctx validation.Context,
		value T,
	) validation.Report {
		fieldContext := ctx.WithPath(validation.Field(name))
		safeField := validation.ValidatorFunc[T](func(validation.Context,
			T,
		) validation.Report {
			return validator.Validate(fieldContext, accessor(value))
		})
		return safeField.Validate(fieldContext, value)
	})
	return nil
}

// Plan is an immutable reflection-free typed struct plan.
type Plan[T any] struct {
	limits validation.Limits
	fields []fieldPlan[T]
}

// Compile snapshots the current builder into an immutable plan.
func (builder *Builder[T]) Compile() (*Plan[T], error) {
	if _, err := validation.NewContext(builder.limits); err != nil {
		return nil, fmt.Errorf("compile typed plan: %w", err)
	}
	return &Plan[T]{limits: builder.limits,
		fields: append([]fieldPlan[T](nil), builder.fields...)}, nil
}

// Validate evaluates fields in declaration order and collects all findings.
func (plan *Plan[T]) Validate(ctx validation.Context, value T) validation.Report {
	report := validation.NewReport(ctx.Limits())
	for _, field := range plan.fields {
		report = report.Merge(field(ctx, value))
	}
	return report
}
