package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

const redactedSecret = "[REDACTED]"

// SecretValue is a string-backed secret with redacted formatting. Reveal is
// explicit; Go strings cannot be reliably erased from memory.
type SecretValue struct {
	value string
}

// NewSecretValue copies a string reference into a redacting value wrapper.
func NewSecretValue(value string) SecretValue {
	return SecretValue{value: value}
}

// Reveal returns the underlying immutable Go string explicitly.
func (secret SecretValue) Reveal() string { return secret.value }

func (SecretValue) String() string   { return redactedSecret }
func (SecretValue) GoString() string { return redactedSecret }

// Format redacts every fmt formatting verb.
func (SecretValue) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(redactedSecret))
}

// MarshalText returns a redacted representation.
func (SecretValue) MarshalText() ([]byte, error) {
	return []byte(redactedSecret), nil
}

// MarshalJSON returns a redacted JSON string.
func (SecretValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(redactedSecret)
}

// LogValue returns a redacted structured logging value.
func (SecretValue) LogValue() slog.Value {
	return slog.StringValue(redactedSecret)
}

// SecretConfig defines a string-backed secret prompt.
type SecretConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[SecretValue]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[SecretValue]
	Transform                                       []Transformer[SecretValue]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Class                                           SecretClass
}

// NewSecret creates a classified redacting string secret prompt.
func NewSecret(config SecretConfig) (Prompt[SecretValue], error) {
	if config.Class == SecretNone || config.Class > SecretOther {
		return Prompt[SecretValue]{}, invalidBehaviorDefinition("define secret prompt", config.ID, fmt.Errorf("%w: secret classification is required", ErrInvalidDefinition))
	}
	parser := func(input string) (SecretValue, error) { return NewSecretValue(input), nil }

	return newTypedPrompt(KindSecret, "define secret prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, config.Default,
		config.Fallback, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, config.Class, parser)
}

// SecretBytes owns mutable secret bytes that can be overwritten with Destroy.
type SecretBytes struct {
	mu        sync.RWMutex
	value     []byte
	destroyed bool
}

// NewSecretBytes creates an owned copy of secret bytes.
func NewSecretBytes(value []byte) *SecretBytes {
	return &SecretBytes{value: append([]byte(nil), value...)}
}

// Reveal returns an owned copy of the current secret bytes.
func (secret *SecretBytes) Reveal() []byte {
	if secret == nil {
		return nil
	}
	secret.mu.RLock()
	defer secret.mu.RUnlock()

	return append([]byte(nil), secret.value...)
}

// Destroy overwrites and releases the wrapper's owned byte slice.
func (secret *SecretBytes) Destroy() {
	if secret == nil {
		return
	}
	secret.mu.Lock()
	defer secret.mu.Unlock()
	for index := range secret.value {
		secret.value[index] = 0
	}
	secret.value = nil
	secret.destroyed = true
}

// Len returns the current owned byte count.
func (secret *SecretBytes) Len() int {
	if secret == nil {
		return 0
	}
	secret.mu.RLock()
	defer secret.mu.RUnlock()

	return len(secret.value)
}

// Destroyed reports whether Destroy has been called.
func (secret *SecretBytes) Destroyed() bool {
	if secret == nil {
		return true
	}
	secret.mu.RLock()
	defer secret.mu.RUnlock()

	return secret.destroyed
}

func (*SecretBytes) String() string   { return redactedSecret }
func (*SecretBytes) GoString() string { return redactedSecret }

// Format redacts every fmt formatting verb.
func (*SecretBytes) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(redactedSecret))
}

// MarshalText returns a redacted representation.
func (*SecretBytes) MarshalText() ([]byte, error) {
	return []byte(redactedSecret), nil
}

// MarshalJSON returns a redacted JSON string.
func (*SecretBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(redactedSecret)
}

// LogValue returns a redacted structured logging value.
func (*SecretBytes) LogValue() slog.Value {
	return slog.StringValue(redactedSecret)
}

func (secret *SecretBytes) clone() *SecretBytes {
	return NewSecretBytes(secret.Reveal())
}

// SecretBytesConfig defines a cleanup-capable byte secret prompt.
type SecretBytesConfig struct {
	ID, Label, Description, Placeholder, Hint, Help string
	Default, Fallback                               Optional[*SecretBytes]
	Headless                                        HeadlessBehavior
	Accessibility                                   Accessibility
	PreValidate, PostValidate                       []Validator[*SecretBytes]
	Transform                                       []Transformer[*SecretBytes]
	Retry                                           RetryPolicy
	Cancel                                          CancelBehavior
	EndOfInput                                      EOFBehavior
	Class                                           SecretClass
}

// NewSecretBytesPrompt creates a classified cleanup-capable secret prompt.
func NewSecretBytesPrompt(config SecretBytesConfig) (Prompt[*SecretBytes], error) {
	if config.Class == SecretNone || config.Class > SecretOther {
		return Prompt[*SecretBytes]{}, invalidBehaviorDefinition("define secret bytes prompt", config.ID, fmt.Errorf("%w: secret classification is required", ErrInvalidDefinition))
	}
	defaultValue := cloneOptionalSecret(config.Default)
	fallbackValue := cloneOptionalSecret(config.Fallback)
	parser := func(input string) (*SecretBytes, error) { return NewSecretBytes([]byte(input)), nil }
	prompt, err := newTypedPrompt(KindSecretBytes, "define secret bytes prompt", config.ID, config.Label,
		config.Description, config.Placeholder, config.Hint, config.Help, defaultValue,
		fallbackValue, config.Headless, config.Accessibility, config.PreValidate,
		config.Transform, config.PostValidate, config.Retry, config.Cancel,
		config.EndOfInput, config.Class, parser)
	if err != nil {
		return Prompt[*SecretBytes]{}, err
	}
	prompt.definition.clone = func(secret *SecretBytes) *SecretBytes { return secret.clone() }
	prompt.definition.parseBytes = func(input []byte) (*SecretBytes, error) {
		return NewSecretBytes(input), nil
	}
	prompt.definition.destroy = func(secret *SecretBytes) { secret.Destroy() }

	return prompt, nil
}

func cloneOptionalSecret(optional Optional[*SecretBytes]) Optional[*SecretBytes] {
	secret, present := optional.Get()
	if !present {
		return Optional[*SecretBytes]{}
	}

	return Some(secret.clone())
}

// ParseBytes copies explicit caller-owned bytes and validates them without
// converting the input into an immutable Go string.
func ParseBytes(ctx context.Context, prompt Prompt[*SecretBytes], input []byte, dependencies any) (*SecretBytes, error) {
	if ctx == nil {
		return nil, invalidBehaviorDefinition("parse secret bytes", prompt.ID(), ErrInvalidDefinition)
	}
	if err := ctx.Err(); err != nil {
		return nil, contextFailure(prompt.ID(), err)
	}
	if prompt.definition.kind != KindSecretBytes {
		return nil, promptFailure(prompt.ID(), ErrUnsupported)
	}

	value, err := prompt.definition.parseBytes(input)
	if err != nil {
		return nil, validationFailure(prompt.ID(), err, prompt.definition.secret)
	}
	value, err = applyPipeline(ctx, prompt.definition, value, dependencies, false)
	if err != nil {
		prompt.definition.destroy(value)
	}

	return value, err
}
