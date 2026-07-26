package prompts

import (
	"context"
	"errors"
	"io"
	"time"
)

// Timer is an owned, stoppable clock event.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Ticker is an owned, stoppable sequence of clock events.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Clock is the animation and timeout seam used by interactive adapters.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
	NewTicker(time.Duration) Ticker
}

// Execution contains every ambient resource a prompt is allowed to use.
type Execution struct {
	Input        io.Reader
	Output       io.Writer
	Error        io.Writer
	Capabilities Capabilities
	Policy       InteractionPolicy
	Clock        Clock
	Dependencies any
	Events       EventSource
	Terminal     TerminalController
	Renderer     Renderer
	Theme        Theme
	Limits       InputLimits
	Keys         KeyMap
}

// Run executes a typed prompt without consulting process-wide streams or
// environment state.
func Run[T any](ctx context.Context, prompt Prompt[T], execution Execution) (T, error) {
	var zero T
	if ctx == nil {
		return zero, &Error{
			Kind:      ErrorInvalidDefinition,
			Operation: "execute prompt",
			PromptID:  prompt.definition.id,
			Cause:     ErrInvalidDefinition,
		}
	}
	if err := ctx.Err(); err != nil {
		kind := ErrorCanceled
		if err == context.DeadlineExceeded {
			kind = ErrorDeadlineExceeded
		}

		return zero, &Error{
			Kind:      kind,
			Operation: "execute prompt",
			PromptID:  prompt.definition.id,
			Cause:     err,
		}
	}

	interactive, err := interactionAllowed(execution.Policy, execution.Capabilities)
	if err != nil {
		return zero, promptFailure(prompt.definition.id, err)
	}
	if interactive {
		return runInteractive(ctx, prompt, execution)
	}

	switch prompt.definition.headless {
	case HeadlessForbidden:
		return zero, promptFailure(prompt.definition.id, ErrInteractionNotPermitted)
	case HeadlessUseDefault:
		if execution.Policy.PermitDefaults {
			if value, ok := prompt.definition.defaultValue.Get(); ok {
				return applyPipeline(ctx, prompt.definition, value, execution.Dependencies, true)
			}
		}

		return zero, promptFailure(prompt.definition.id, ErrInteractionNotPermitted)
	case HeadlessUseFallback:
		if value, ok := prompt.definition.fallbackValue.Get(); ok {
			return applyPipeline(ctx, prompt.definition, value, execution.Dependencies, true)
		}

		return zero, promptFailure(prompt.definition.id, ErrInteractionNotPermitted)
	default:
		return zero, promptFailure(prompt.definition.id, ErrUnsupported)
	}
}

func applyPipeline[T any](ctx context.Context, definition definition[T], value T, dependencies any, clone bool) (result T, resultErr error) {
	defer func() {
		if recover() != nil {
			var zero T
			result = zero
			resultErr = &Error{
				Kind:      ErrorAdapter,
				Operation: "run prompt callback",
				PromptID:  definition.id,
				Cause:     ErrAdapter,
			}
		}
	}()
	if clone && definition.clone != nil {
		value = definition.clone(value)
	}

	validation := ValidationContext{Dependencies: dependencies}
	for _, validator := range definition.preValidate {
		if err := validator(ctx, value, validation); err != nil {
			return value, validationFailure(definition.id, err, definition.secret)
		}
		if err := ctx.Err(); err != nil {
			return value, contextFailure(definition.id, err)
		}
	}

	for _, transformer := range definition.transform {
		transformed, err := transformer(ctx, value, validation)
		if err != nil {
			return value, validationFailure(definition.id, err, definition.secret)
		}
		value = transformed
		if err := ctx.Err(); err != nil {
			return value, contextFailure(definition.id, err)
		}
	}

	for _, validator := range definition.postValidate {
		if err := validator(ctx, value, validation); err != nil {
			return value, validationFailure(definition.id, err, definition.secret)
		}
		if err := ctx.Err(); err != nil {
			return value, contextFailure(definition.id, err)
		}
	}

	return value, nil
}

func validationFailure(promptID string, cause error, secret SecretClass) error {
	if secret != SecretNone {
		cause = NewValidationIssue("secret_validation", "Secret value was rejected", promptID)
	}

	return &Error{
		Kind:      ErrorValidationExhausted,
		Operation: "validate prompt",
		PromptID:  promptID,
		Cause:     normalizeIssue(cause, promptID),
	}
}

func contextFailure(promptID string, cause error) error {
	kind := ErrorCanceled
	if errors.Is(cause, context.DeadlineExceeded) {
		kind = ErrorDeadlineExceeded
	}

	return &Error{
		Kind:      kind,
		Operation: "execute prompt",
		PromptID:  promptID,
		Cause:     cause,
	}
}

func interactionAllowed(policy InteractionPolicy, capabilities Capabilities) (bool, error) {
	switch policy.Mode {
	case InteractiveRequired:
		if !policy.PermitInteraction {
			return false, ErrInteractionNotPermitted
		}
		if !capabilities.InputTerminal || !capabilities.OutputTerminal {
			return false, ErrTerminalUnavailable
		}

		return true, nil
	case InteractivePreferred:
		return policy.PermitInteraction && capabilities.InputTerminal && capabilities.OutputTerminal, nil
	case NonInteractiveOnly:
		return false, nil
	case AutoDetect:
		if !policy.PermitInteraction {
			return false, nil
		}
		if policy.Auto.RequireInputTerminal && !capabilities.InputTerminal {
			return false, nil
		}
		if policy.Auto.RequireOutputTerminal && !capabilities.OutputTerminal {
			return false, nil
		}

		return true, nil
	default:
		return false, ErrUnsupported
	}
}

func promptFailure(promptID string, target error) error {
	kind := ErrorUnsupported
	switch {
	case errors.Is(target, ErrInteractionNotPermitted):
		kind = ErrorInteractionNotPermitted
	case errors.Is(target, ErrTerminalUnavailable):
		kind = ErrorTerminalUnavailable
	}

	return &Error{
		Kind:      kind,
		Operation: "execute prompt",
		PromptID:  promptID,
		Cause:     target,
	}
}
