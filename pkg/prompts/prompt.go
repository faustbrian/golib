package prompts

// Optional distinguishes an explicit zero value from an omitted value.
type Optional[T any] struct {
	value T
	set   bool
}

// Some returns an explicitly present value.
func Some[T any](value T) Optional[T] {
	return Optional[T]{value: value, set: true}
}

// Get returns the value and whether it is present.
func (optional Optional[T]) Get() (T, bool) {
	return optional.value, optional.set
}

// Accessibility provides caller-localized linear rendering metadata.
type Accessibility struct {
	Label       string
	Description string
	TextualHint string
}

// TextConfig defines an immutable single-line text prompt.
type TextConfig struct {
	ID            string
	Label         string
	Description   string
	Placeholder   string
	Hint          string
	Help          string
	Default       Optional[string]
	Fallback      Optional[string]
	Headless      HeadlessBehavior
	Accessibility Accessibility
	PreValidate   []Validator[string]
	Transform     []Transformer[string]
	PostValidate  []Validator[string]
	Retry         RetryPolicy
	Cancel        CancelBehavior
	EndOfInput    EOFBehavior
	Secret        SecretClass
}

type definition[T any] struct {
	kind          PromptKind
	id            string
	label         string
	description   string
	placeholder   string
	hint          string
	help          string
	defaultValue  Optional[T]
	fallbackValue Optional[T]
	headless      HeadlessBehavior
	accessibility Accessibility
	preValidate   []Validator[T]
	transform     []Transformer[T]
	postValidate  []Validator[T]
	retry         RetryPolicy
	cancel        CancelBehavior
	endOfInput    EOFBehavior
	secret        SecretClass
	parse         func(string) (T, error)
	parseBytes    func([]byte) (T, error)
	clone         func(T) T
	destroy       func(T)
	selection     *selectionDetails
}

// Prompt is an immutable typed prompt definition.
type Prompt[T any] struct {
	definition definition[T]
}

// NewText creates a single-line text prompt.
func NewText(config TextConfig) (Prompt[string], error) {
	return newStringPrompt(KindText, "define text prompt", stringDefinitionConfig{
		config.ID, config.Label, config.Description, config.Placeholder, config.Hint, config.Help,
		config.Default, config.Fallback, config.Headless, config.Accessibility,
		config.PreValidate, config.Transform, config.PostValidate, config.Retry,
		config.Cancel, config.EndOfInput, config.Secret,
	}, parseSingleLine)
}

// ID returns the stable prompt identity.
func (prompt Prompt[T]) ID() string {
	return prompt.definition.id
}

// Describe returns a value snapshot of the prompt's execution contract.
func (prompt Prompt[T]) Describe() Descriptor {
	definition := prompt.definition

	return Descriptor{
		Kind:          definition.kind,
		ID:            definition.id,
		Label:         definition.label,
		Description:   definition.description,
		Placeholder:   definition.placeholder,
		Hint:          definition.hint,
		Help:          definition.help,
		Retry:         definition.retry,
		Cancel:        definition.cancel,
		EndOfInput:    definition.endOfInput,
		Secret:        definition.secret,
		Headless:      definition.headless,
		Accessibility: definition.accessibility,
	}
}
