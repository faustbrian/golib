package prompts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// FormValidator performs explicit cross-field validation after field prompts.
type FormValidator func(context.Context, FormResult, ValidationContext) error

// FormField is an immutable type-erased field wrapper created with AsField.
type FormField interface {
	formID() string
	run(context.Context, Execution) (storedFormValue, error)
	condition(FormResult) bool
}

type typedFormField[T any] struct {
	prompt Prompt[T]
}

// AsField retains a prompt's typed execution and cloning behavior in a form.
func AsField[T any](prompt Prompt[T]) FormField { return typedFormField[T]{prompt: prompt} }

// When returns a conditional copy of a field. The predicate observes only
// answers from prior fields in this execution.
func When(field FormField, predicate func(FormResult) bool) FormField {
	if field == nil {
		return nil
	}
	return conditionalFormField{field: field, predicate: predicate}
}

type conditionalFormField struct {
	field     FormField
	predicate func(FormResult) bool
}

func (field typedFormField[T]) formID() string { return field.prompt.ID() }

func (field typedFormField[T]) run(ctx context.Context, execution Execution) (storedFormValue, error) {
	value, err := Run(ctx, field.prompt, execution)
	if err != nil {
		return storedFormValue{}, err
	}
	clone := func() any { return value }
	if field.prompt.definition.clone != nil {
		clone = func() any { return field.prompt.definition.clone(value) }
	}
	return storedFormValue{value: value, clone: clone}, nil
}

func (typedFormField[T]) condition(FormResult) bool { return true }

func (field conditionalFormField) formID() string { return field.field.formID() }
func (field conditionalFormField) run(ctx context.Context, execution Execution) (storedFormValue, error) {
	return field.field.run(ctx, execution)
}
func (field conditionalFormField) condition(result FormResult) bool {
	return field.predicate != nil && field.predicate(result)
}

// FormConfig defines an ordered grouped interaction.
type FormConfig struct {
	ID           string
	Fields       []FormField
	Validate     []FormValidator
	Dependencies any
}

// Form is an immutable grouped prompt definition.
type Form struct {
	id           string
	fields       []FormField
	validators   []FormValidator
	dependencies any
}

// NewForm creates an ordered, reusable form definition.
func NewForm(config FormConfig) (Form, error) {
	if config.ID == "" || len(config.Fields) == 0 {
		return Form{}, invalidBehaviorDefinition("define form", config.ID, fmt.Errorf("%w: form identity and fields are required", ErrInvalidDefinition))
	}
	fields := append([]FormField(nil), config.Fields...)
	identities := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field == nil || field.formID() == "" {
			return Form{}, invalidBehaviorDefinition("define form", config.ID, fmt.Errorf("%w: invalid form field", ErrInvalidDefinition))
		}
		if _, duplicate := identities[field.formID()]; duplicate {
			return Form{}, invalidBehaviorDefinition("define form", config.ID, fmt.Errorf("%w: duplicate form field identity", ErrInvalidDefinition))
		}
		identities[field.formID()] = struct{}{}
	}

	return Form{
		id: config.ID, fields: fields,
		validators:   append([]FormValidator(nil), config.Validate...),
		dependencies: config.Dependencies,
	}, nil
}

type storedFormValue struct {
	value any
	clone func() any
}

// FormResult is an immutable typed answer collection.
type FormResult struct {
	values map[string]storedFormValue
	order  []string
}

// IDs returns answered field identities in execution order.
func (result FormResult) IDs() []string { return append([]string(nil), result.order...) }

// Has reports whether a field ran and produced an answer.
func (result FormResult) Has(identity string) bool {
	_, exists := result.values[identity]
	return exists
}

// DestroySecrets best-effort destroys byte-oriented secrets owned by this
// result. Copies of FormResult share the same owned secret wrappers.
func (result FormResult) DestroySecrets() {
	for _, stored := range result.values {
		if secret, ok := stored.value.(*SecretBytes); ok {
			secret.Destroy()
		}
	}
}

// FormValue returns a typed defensive copy of one answer.
func FormValue[T any](result FormResult, identity string) (T, bool) {
	var zero T
	stored, exists := result.values[identity]
	if !exists {
		return zero, false
	}
	value, ok := stored.clone().(T)
	if !ok {
		return zero, false
	}
	return value, true
}

// RunForm executes fields in declaration order without retaining answers in
// the reusable definition.
func RunForm(ctx context.Context, form Form, execution Execution) (result FormResult, resultErr error) {
	if ctx == nil {
		return FormResult{}, invalidBehaviorDefinition("execute form", form.id, ErrInvalidDefinition)
	}
	if err := ctx.Err(); err != nil {
		return FormResult{}, contextFailure(form.id, err)
	}
	replays := make(map[string]*formReplay, len(form.fields))
	defer func() {
		for _, replay := range replays {
			replay.destroy()
		}
		if recover() != nil {
			resultErr = &Error{
				Kind: ErrorAdapter, Operation: "run form callback",
				PromptID: form.id, Cause: ErrAdapter,
			}
		}
		if resultErr != nil {
			result.DestroySecrets()
			result = FormResult{}
		}
	}()
	result = FormResult{values: make(map[string]storedFormValue, len(form.fields))}
	for index := 0; index < len(form.fields); {
		field := form.fields[index]
		if !field.condition(result) {
			if err := ctx.Err(); err != nil {
				return result, contextFailure(form.id, err)
			}
			index++
			continue
		}
		interaction := &formInteraction{initial: replays[field.formID()]}
		fieldContext := context.WithValue(ctx, formNavigationContextKey{}, interaction)
		stored, err := field.run(fieldContext, execution)
		if interaction.captured != nil {
			replays[field.formID()].destroy()
			replays[field.formID()] = interaction.captured
			interaction.captured = nil
		}
		if errors.Is(err, errFormBack) {
			target := max(0, index-1)
			for target > 0 && !form.fields[target].condition(result) {
				target--
			}
			removeFormResultsFrom(&result, form.fields, target)
			index = target
			continue
		}
		if err != nil {
			return result, err
		}
		result.values[field.formID()] = stored
		result.order = append(result.order, field.formID())
		index++
	}
	dependencies := execution.Dependencies
	if dependencies == nil {
		dependencies = form.dependencies
	}
	validation := ValidationContext{Dependencies: dependencies}
	for _, validator := range form.validators {
		if err := validator(ctx, result, validation); err != nil {
			return result, formValidationFailure(form.id, result, err)
		}
		if err := ctx.Err(); err != nil {
			return result, contextFailure(form.id, err)
		}
	}
	return result, nil
}

func removeFormResultsFrom(result *FormResult, fields []FormField, start int) {
	removed := make(map[string]struct{}, len(fields)-start)
	for _, field := range fields[start:] {
		identity := field.formID()
		if stored, exists := result.values[identity]; exists {
			destroyStoredFormValue(stored)
			delete(result.values, identity)
		}
		removed[identity] = struct{}{}
	}
	order := result.order[:0]
	for _, identity := range result.order {
		if _, exists := removed[identity]; !exists {
			order = append(order, identity)
		}
	}
	result.order = order
}

func destroyStoredFormValue(stored storedFormValue) {
	if secret, ok := stored.value.(*SecretBytes); ok {
		secret.Destroy()
	}
}

func formValidationFailure(formID string, result FormResult, cause error) error {
	if fields, leaked := formSecretLeak(result, cause); leaked {
		cause = NewValidationIssue("form_validation", "Form validation failed", fields...)
	}
	return &Error{
		Kind: ErrorValidationExhausted, Operation: "validate form",
		PromptID: formID, Cause: normalizeIssue(cause, formID),
	}
}

func formSecretLeak(result FormResult, cause error) ([]string, bool) {
	message := cause.Error()
	messageBytes := []byte(message)
	defer clear(messageBytes)
	fields := []string{}
	for identity, stored := range result.values {
		leaked := false
		switch value := stored.value.(type) {
		case SecretValue:
			leaked = value.value != "" && strings.Contains(message, value.value)
		case *SecretBytes:
			secret := value.Reveal()
			leaked = len(secret) > 0 && bytes.Contains(messageBytes, secret)
			for index := range secret {
				secret[index] = 0
			}
		}
		if leaked {
			fields = append(fields, identity)
		}
	}
	if len(fields) == 0 {
		return nil, false
	}
	sort.Strings(fields)
	var issue *ValidationIssue
	if errors.As(cause, &issue) {
		fields = append([]string(nil), issue.fields...)
	}
	return fields, true
}
