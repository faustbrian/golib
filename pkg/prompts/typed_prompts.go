package prompts

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type stringDefinitionConfig struct {
	id, label, description, placeholder, hint, help string
	defaultValue, fallbackValue                     Optional[string]
	headless                                        HeadlessBehavior
	accessibility                                   Accessibility
	pre                                             []Validator[string]
	transform                                       []Transformer[string]
	post                                            []Validator[string]
	retry                                           RetryPolicy
	cancel                                          CancelBehavior
	eof                                             EOFBehavior
	secret                                          SecretClass
}

// MultilineConfig defines a multiline text prompt.
type MultilineConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[string]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[string]
	Transform                                       []Transformer[string]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Secret                                          SecretClass
}

// NewMultiline creates a multiline text prompt.
func NewMultiline(config MultilineConfig) (Prompt[string], error) {
	return newStringPrompt(KindMultiline, "define multiline prompt", stringDefinitionConfig{
		config.ID, config.Label, config.Description, config.Placeholder, config.Hint, config.Help,
		config.Default, config.Fallback, config.Headless, config.Accessibility,
		config.PreValidate, config.Transform, config.PostValidate, config.Retry, config.Cancel, config.EndOfInput, config.Secret,
	}, func(input string) (string, error) { return input, nil })
}

func parseSingleLine(input string) (string, error) {
	if strings.ContainsAny(input, "\r\n") {
		return "", parseIssue("text")
	}

	return input, nil
}

func newStringPrompt(kind PromptKind, operation string, config stringDefinitionConfig, parser func(string) (string, error)) (Prompt[string], error) {
	if config.id == "" || config.label == "" {
		return Prompt[string]{}, invalidBehaviorDefinition(operation, config.id, fmt.Errorf("%w: identity and label are required", ErrInvalidDefinition))
	}
	for _, callback := range config.pre {
		if callback == nil {
			return Prompt[string]{}, invalidBehaviorDefinition(operation, config.id, fmt.Errorf("%w: nil pre-validator", ErrInvalidDefinition))
		}
	}
	for _, callback := range config.transform {
		if callback == nil {
			return Prompt[string]{}, invalidBehaviorDefinition(operation, config.id, fmt.Errorf("%w: nil transformer", ErrInvalidDefinition))
		}
	}
	for _, callback := range config.post {
		if callback == nil {
			return Prompt[string]{}, invalidBehaviorDefinition(operation, config.id, fmt.Errorf("%w: nil post-validator", ErrInvalidDefinition))
		}
	}
	retry, err := normalizeBehavior(config.retry, config.cancel, config.eof, config.secret)
	if err != nil {
		return Prompt[string]{}, invalidBehaviorDefinition(operation, config.id, err)
	}

	return Prompt[string]{definition: definition[string]{
		kind: kind, id: config.id, label: config.label, description: config.description,
		placeholder: config.placeholder, hint: config.hint, help: config.help,
		defaultValue: config.defaultValue, fallbackValue: config.fallbackValue,
		headless: config.headless, accessibility: config.accessibility,
		preValidate:  append([]Validator[string](nil), config.pre...),
		transform:    append([]Transformer[string](nil), config.transform...),
		postValidate: append([]Validator[string](nil), config.post...),
		retry:        retry, cancel: config.cancel, endOfInput: config.eof, secret: config.secret,
		parse: parser,
	}}, nil
}

// IntegerConfig defines a signed 64-bit integer prompt.
type IntegerConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[int64]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[int64]
	Transform                                       []Transformer[int64]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
}

// NewInteger creates a signed 64-bit integer prompt.
func NewInteger(config IntegerConfig) (Prompt[int64], error) {
	return newTypedPrompt(KindInteger, "define integer prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parseInteger)
}

// DecimalConfig defines an exact base-10 prompt.
type DecimalConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[Decimal]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[Decimal]
	Transform                                       []Transformer[Decimal]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
}

// NewDecimal creates an exact base-10 decimal prompt.
func NewDecimal(config DecimalConfig) (Prompt[Decimal], error) {
	return newTypedPrompt(KindDecimal, "define decimal prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parseDecimal)
}

// DurationConfig defines a Go duration prompt.
type DurationConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[time.Duration]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[time.Duration]
	Transform                                       []Transformer[time.Duration]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
}

// NewDuration creates a Go duration prompt.
func NewDuration(config DurationConfig) (Prompt[time.Duration], error) {
	parser := func(input string) (time.Duration, error) {
		value, err := time.ParseDuration(input)
		if err != nil {
			return 0, parseIssue("duration")
		}

		return value, nil
	}

	return newTypedPrompt(KindDuration, "define duration prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parser)
}

// DateConfig defines an ISO calendar-date prompt.
type DateConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[Date]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[Date]
	Transform                                       []Transformer[Date]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
}

// NewDate creates an ISO calendar-date prompt.
func NewDate(config DateConfig) (Prompt[Date], error) {
	return newTypedPrompt(KindDate, "define date prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parseDate)
}

// TimeConfig defines a wall-clock time prompt.
type TimeConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[TimeOfDay]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[TimeOfDay]
	Transform                                       []Transformer[TimeOfDay]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
}

// NewTime creates a wall-clock time prompt.
func NewTime(config TimeConfig) (Prompt[TimeOfDay], error) {
	return newTypedPrompt(KindTime, "define time prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parseTimeOfDay)
}

// PathConfig defines a path prompt without filesystem access.
type PathConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[Path]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[Path]
	Transform                                       []Transformer[Path]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Kind                                            PathKind
}

// NewPath creates a path prompt that performs no filesystem access.
func NewPath(config PathConfig) (Prompt[Path], error) {
	if config.Kind > PathDirectory {
		return Prompt[Path]{}, invalidBehaviorDefinition("define path prompt", config.ID, fmt.Errorf("%w: invalid path kind", ErrInvalidDefinition))
	}
	parser := func(input string) (Path, error) {
		if input == "" || strings.ContainsRune(input, '\x00') {
			return Path{}, parseIssue("path")
		}

		return Path{value: input, kind: config.Kind}, nil
	}

	return newTypedPrompt(KindPath, "define path prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parser)
}

// ConfirmConfig defines a localized yes/no prompt.
type ConfirmConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[bool]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[bool]
	Transform                                       []Transformer[bool]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Accept, Reject                                  []string
}

// NewConfirm creates a localized yes/no prompt.
func NewConfirm(config ConfirmConfig) (Prompt[bool], error) {
	accept := append([]string(nil), config.Accept...)
	reject := append([]string(nil), config.Reject...)
	if len(accept) == 0 {
		accept = []string{"y", "yes", "true", "1"}
	}
	if len(reject) == 0 {
		reject = []string{"n", "no", "false", "0"}
	}
	values := make(map[string]bool, len(accept)+len(reject))
	for _, value := range accept {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if _, duplicate := values[normalized]; normalized == "" || duplicate {
			return Prompt[bool]{}, invalidBehaviorDefinition("define confirm prompt", config.ID, fmt.Errorf("%w: invalid or duplicate confirmation value", ErrInvalidDefinition))
		}
		values[normalized] = true
	}
	for _, value := range reject {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if _, duplicate := values[normalized]; normalized == "" || duplicate {
			return Prompt[bool]{}, invalidBehaviorDefinition("define confirm prompt", config.ID, fmt.Errorf("%w: overlapping confirmation values", ErrInvalidDefinition))
		}
		values[normalized] = false
	}
	parser := func(input string) (bool, error) {
		value, ok := values[strings.ToLower(strings.TrimSpace(input))]
		if !ok {
			return false, parseIssue("confirmation")
		}

		return value, nil
	}

	return newTypedPrompt(KindConfirm, "define confirm prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parser)
}

func newTypedPrompt[T any](kind PromptKind, operation, id, label, description, placeholder, hint, help string,
	defaultValue, fallbackValue Optional[T], headless HeadlessBehavior, accessibility Accessibility,
	pre []Validator[T], transform []Transformer[T], post []Validator[T], retry RetryPolicy,
	cancel CancelBehavior, eof EOFBehavior, secret SecretClass, parser func(string) (T, error),
) (Prompt[T], error) {
	if id == "" || label == "" {
		return Prompt[T]{}, invalidBehaviorDefinition(operation, id, fmt.Errorf("%w: identity and label are required", ErrInvalidDefinition))
	}
	for _, callback := range pre {
		if callback == nil {
			return Prompt[T]{}, invalidBehaviorDefinition(operation, id, fmt.Errorf("%w: nil pre-validator", ErrInvalidDefinition))
		}
	}
	for _, callback := range transform {
		if callback == nil {
			return Prompt[T]{}, invalidBehaviorDefinition(operation, id, fmt.Errorf("%w: nil transformer", ErrInvalidDefinition))
		}
	}
	for _, callback := range post {
		if callback == nil {
			return Prompt[T]{}, invalidBehaviorDefinition(operation, id, fmt.Errorf("%w: nil post-validator", ErrInvalidDefinition))
		}
	}
	normalizedRetry, err := normalizeBehavior(retry, cancel, eof, secret)
	if err != nil {
		return Prompt[T]{}, invalidBehaviorDefinition(operation, id, err)
	}

	return Prompt[T]{definition: definition[T]{
		kind: kind, id: id, label: label, description: description, placeholder: placeholder,
		hint: hint, help: help, defaultValue: defaultValue, fallbackValue: fallbackValue,
		headless: headless, accessibility: accessibility,
		preValidate: append([]Validator[T](nil), pre...), transform: append([]Transformer[T](nil), transform...),
		postValidate: append([]Validator[T](nil), post...), retry: normalizedRetry,
		cancel: cancel, endOfInput: eof, secret: secret, parse: parser,
	}}, nil
}

// Parse converts explicit caller-supplied input and applies the prompt's typed
// validation pipeline without acquiring terminal input.
func Parse[T any](ctx context.Context, prompt Prompt[T], input string, dependencies any) (T, error) {
	var zero T
	if ctx == nil {
		return zero, invalidBehaviorDefinition("parse prompt", prompt.ID(), ErrInvalidDefinition)
	}
	if err := ctx.Err(); err != nil {
		return zero, contextFailure(prompt.ID(), err)
	}
	if prompt.definition.parse == nil {
		return zero, promptFailure(prompt.ID(), ErrUnsupported)
	}
	value, err := prompt.definition.parse(input)
	if err != nil {
		return zero, validationFailure(prompt.ID(), err, prompt.definition.secret)
	}

	return applyPipeline(ctx, prompt.definition, value, dependencies, false)
}
