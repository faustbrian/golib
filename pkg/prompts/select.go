package prompts

import (
	"context"
	"fmt"
	"strings"
)

// OptionConfig defines one stable selection identity and display value.
type OptionConfig[T any] struct {
	ID          string
	Label       string
	Description string
	Group       string
	Value       T
	Disabled    bool
}

// Option keeps stable identity separate from its display label and value.
type Option[T any] struct {
	id          string
	label       string
	description string
	group       string
	value       T
	disabled    bool
}

// NewOption creates an immutable option value.
func NewOption[T any](config OptionConfig[T]) (Option[T], error) {
	if config.ID == "" || config.Label == "" {
		return Option[T]{}, invalidBehaviorDefinition("define option", config.ID, fmt.Errorf("%w: option identity and label are required", ErrInvalidDefinition))
	}

	return Option[T]{
		id: config.ID, label: config.Label, description: config.Description,
		group: config.Group, value: config.Value, disabled: config.Disabled,
	}, nil
}

func (option Option[T]) ID() string          { return option.id }
func (option Option[T]) Label() string       { return option.label }
func (option Option[T]) Description() string { return option.description }
func (option Option[T]) Group() string       { return option.group }
func (option Option[T]) Value() T            { return option.value }
func (option Option[T]) Disabled() bool      { return option.disabled }

// SelectConfig defines a single selection prompt.
type SelectConfig[T any] struct {
	ID, Label, Description, Placeholder, Hint, Help string
	DefaultID, FallbackID                           Optional[string]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[T]
	Transform                                       []Transformer[T]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Options                                         []Option[T]
	InitialID                                       string
	MaxOptions                                      int
}

type selectionOption struct {
	id, label, description, group string
	disabled                      bool
}

type selectionDetails struct {
	options      []selectionOption
	initialIDs   []string
	multiple     bool
	minimum      int
	maximum      int
	searchPolicy SearchPolicy
}

// NewSelect creates a stable-identity single selection prompt.
func NewSelect[T any](config SelectConfig[T]) (Prompt[T], error) {
	return newSelect(KindSelect, config)
}

func newSelect[T any](kind PromptKind, config SelectConfig[T]) (Prompt[T], error) {
	options, byID, err := ownOptions(config.Options, config.MaxOptions)
	if err != nil {
		return Prompt[T]{}, invalidBehaviorDefinition("define select prompt", config.ID, err)
	}
	defaultValue, err := resolveOption(config.DefaultID, byID)
	if err != nil {
		return Prompt[T]{}, invalidBehaviorDefinition("define select prompt", config.ID, err)
	}
	fallbackValue, err := resolveOption(config.FallbackID, byID)
	if err != nil {
		return Prompt[T]{}, invalidBehaviorDefinition("define select prompt", config.ID, err)
	}
	if config.InitialID != "" {
		option, exists := byID[config.InitialID]
		if !exists || option.disabled {
			return Prompt[T]{}, invalidBehaviorDefinition("define select prompt", config.ID, fmt.Errorf("%w: invalid initial option", ErrInvalidDefinition))
		}
	}
	parser := func(input string) (T, error) {
		option, exists := byID[strings.TrimSpace(input)]
		if !exists || option.disabled {
			var zero T
			return zero, parseIssue("selection")
		}

		return option.value, nil
	}
	prompt, err := newTypedPrompt(kind, "define select prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, defaultValue,
		fallbackValue, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, SecretNone, parser)
	if err != nil {
		return Prompt[T]{}, err
	}
	details := selectionDetails{
		options: selectionOptions(options), initialIDs: []string{config.InitialID},
		minimum: 1, maximum: 1,
	}
	prompt.definition.selection = &details

	return prompt, nil
}

func ownOptions[T any](source []Option[T], maximum int) ([]Option[T], map[string]Option[T], error) {
	if maximum == 0 {
		maximum = 10_000
	}
	if maximum < 1 || len(source) == 0 || len(source) > maximum {
		return nil, nil, fmt.Errorf("%w: option count is outside configured bounds", ErrInvalidDefinition)
	}
	options := append([]Option[T](nil), source...)
	byID := make(map[string]Option[T], len(options))
	for _, option := range options {
		if option.id == "" || option.label == "" {
			return nil, nil, fmt.Errorf("%w: invalid option", ErrInvalidDefinition)
		}
		if _, duplicate := byID[option.id]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate option identity", ErrInvalidDefinition)
		}
		byID[option.id] = option
	}

	return options, byID, nil
}

func resolveOption[T any](identity Optional[string], byID map[string]Option[T]) (Optional[T], error) {
	id, present := identity.Get()
	if !present {
		return Optional[T]{}, nil
	}
	option, exists := byID[id]
	if !exists || option.disabled {
		return Optional[T]{}, fmt.Errorf("%w: default or fallback option is unavailable", ErrInvalidDefinition)
	}

	return Some(option.value), nil
}

// MultiSelectConfig defines a bounded multiple-selection prompt. Results are
// always returned in option declaration order.
type MultiSelectConfig[T any] struct {
	ID, Label, Description, Placeholder, Hint, Help string
	DefaultIDs, FallbackIDs                         Optional[[]string]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[[]T]
	Transform                                       []Transformer[[]T]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Options                                         []Option[T]
	InitialIDs                                      []string
	Min, Max                                        int
	MaxOptions                                      int
}

// NewMultiSelect creates a bounded multiple-selection prompt.
func NewMultiSelect[T any](config MultiSelectConfig[T]) (Prompt[[]T], error) {
	options, byID, err := ownOptions(config.Options, config.MaxOptions)
	if err != nil {
		return Prompt[[]T]{}, invalidBehaviorDefinition("define multi-select prompt", config.ID, err)
	}
	maximum := config.Max
	if maximum == 0 {
		maximum = len(options)
	}
	if config.Min < 0 || maximum < config.Min || maximum > len(options) || config.Min > len(options) {
		return Prompt[[]T]{}, invalidBehaviorDefinition("define multi-select prompt", config.ID, fmt.Errorf("%w: invalid selection bounds", ErrInvalidDefinition))
	}
	resolve := func(identities []string) ([]T, error) {
		return resolveOptions(identities, options, byID, config.Min, maximum)
	}
	defaultValue, err := resolveOptionIDs(config.DefaultIDs, resolve)
	if err != nil {
		return Prompt[[]T]{}, invalidBehaviorDefinition("define multi-select prompt", config.ID, err)
	}
	fallbackValue, err := resolveOptionIDs(config.FallbackIDs, resolve)
	if err != nil {
		return Prompt[[]T]{}, invalidBehaviorDefinition("define multi-select prompt", config.ID, err)
	}
	if len(config.InitialIDs) > 0 {
		if _, err := resolve(config.InitialIDs); err != nil {
			return Prompt[[]T]{}, invalidBehaviorDefinition("define multi-select prompt", config.ID, err)
		}
	}
	parser := func(input string) ([]T, error) {
		identities := []string{}
		if strings.TrimSpace(input) != "" {
			for identity := range strings.SplitSeq(input, ",") {
				identities = append(identities, strings.TrimSpace(identity))
			}
		}

		return resolveOptions(identities, options, byID, 0, len(options))
	}
	post := append([]Validator[[]T](nil), config.PostValidate...)
	post = append(post, func(_ context.Context, values []T, _ ValidationContext) error {
		if len(values) < config.Min || len(values) > maximum {
			return NewValidationIssue("selection_count", "Invalid number of selections", config.ID)
		}

		return nil
	})
	prompt, err := newTypedPrompt(KindMultiSelect, "define multi-select prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, defaultValue,
		fallbackValue, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, post, config.Retry, config.Cancel, config.EndOfInput,
		SecretNone, parser)
	if err != nil {
		return Prompt[[]T]{}, err
	}
	prompt.definition.clone = func(values []T) []T { return append([]T(nil), values...) }
	details := selectionDetails{
		options: selectionOptions(options), initialIDs: append([]string(nil), config.InitialIDs...),
		multiple: true, minimum: config.Min, maximum: maximum,
	}
	prompt.definition.selection = &details

	return prompt, nil
}

func selectionOptions[T any](options []Option[T]) []selectionOption {
	result := make([]selectionOption, len(options))
	for index, option := range options {
		result[index] = selectionOption{
			id: option.id, label: option.label, description: option.description,
			group: option.group, disabled: option.disabled,
		}
	}

	return result
}

func resolveOptionIDs[T any](identities Optional[[]string], resolve func([]string) ([]T, error)) (Optional[[]T], error) {
	values, present := identities.Get()
	if !present {
		return Optional[[]T]{}, nil
	}
	resolved, err := resolve(append([]string(nil), values...))
	if err != nil {
		return Optional[[]T]{}, err
	}

	return Some(resolved), nil
}

func resolveOptions[T any](identities []string, options []Option[T], byID map[string]Option[T], minimum, maximum int) ([]T, error) {
	selected := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		option, exists := byID[identity]
		if !exists || option.disabled {
			return nil, parseIssue("selection")
		}
		if _, duplicate := selected[identity]; duplicate {
			return nil, parseIssue("selection")
		}
		selected[identity] = struct{}{}
	}
	if len(selected) < minimum || len(selected) > maximum {
		return nil, parseIssue("selection")
	}
	values := make([]T, 0, len(selected))
	for _, option := range options {
		if _, exists := selected[option.id]; exists {
			values = append(values, option.value)
		}
	}

	return values, nil
}
