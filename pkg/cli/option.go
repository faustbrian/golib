package cli

import "time"

// OptionDefinition is a typed named option accepted by commands and groups.
type OptionDefinition interface {
	optionSpecification() optionSpec
}

type optionSpec struct {
	key         int
	binding     any
	name        string
	short       rune
	valueType   string
	persistent  bool
	secret      bool
	description string
	allowed     []string
	format      string
	hasFormat   bool
	origin      string
	hasDefault  bool
	defaultVal  any
	boolean     bool
	required    bool
	parse       func([]string) (any, error)
	completion  CompletionProvider
}

// Option is a typed named-option binding.
type Option[T any] struct {
	spec optionSpec
}

// StringOption creates a string option.
func StringOption(name string) *Option[string] {
	return newOption[string](name, "string", false, parseString)
}

// BoolOption creates a boolean option.
func BoolOption(name string) *Option[bool] {
	return newOption[bool](name, "bool", true, parseBool)
}

// IntOption creates a signed 64-bit integer option.
func IntOption(name string) *Option[int64] {
	return newOption[int64](name, "integer", false, parseInt)
}

// UintOption creates an unsigned 64-bit integer option.
func UintOption(name string) *Option[uint64] {
	return newOption[uint64](name, "unsigned-integer", false, parseUint)
}

// FloatOption creates a 64-bit floating-point option.
func FloatOption(name string) *Option[float64] {
	return newOption[float64](name, "float", false, parseFloat)
}

// DurationOption creates a time.Duration option.
func DurationOption(name string) *Option[time.Duration] {
	return newOption[time.Duration](name, "duration", false, parseDuration)
}

// TimeOption creates a time.Time option parsed with the supplied layout.
func TimeOption(name, layout string) *Option[time.Time] {
	option := newOption[time.Time](name, "time", false, parseTime(layout))
	option.spec.format = layout
	option.spec.hasFormat = true

	return option
}

// EnumOption creates a string option constrained to the supplied values.
func EnumOption(name string, values ...string) *Option[string] {
	option := newOption[string](name, "enum", false, parseEnum(values))
	option.spec.allowed = append([]string{}, values...)

	return option
}

// StringsOption creates a repeatable string-slice option.
func StringsOption(name string) *Option[[]string] {
	return newOption[[]string](name, "string-slice", false, parseStrings)
}

// KeyValuesOption creates a repeatable key/value option.
func KeyValuesOption(name string) *Option[map[string]string] {
	return newOption[map[string]string](name, "key-value", false, parseKeyValues)
}

// TypedOption creates an engine-independent custom scalar option.
func TypedOption[T any](name, valueType string, parser Parser[T]) *Option[T] {
	return newOption[T](name, valueType, false, scalarParser(parser))
}

func newOption[T any](
	name string,
	valueType string,
	boolean bool,
	parse func([]string) (any, error),
) *Option[T] {
	option := &Option[T]{spec: optionSpec{
		name:      name,
		valueType: valueType,
		boolean:   boolean,
		parse:     parse,
	}}
	option.spec.binding = option

	return option
}

// Short declares a single ASCII shorthand token.
func (option *Option[T]) Short(short rune) *Option[T] {
	if option != nil {
		option.spec.short = short
	}

	return option
}

// Persistent makes an option available to all descendant commands.
func (option *Option[T]) Persistent() *Option[T] {
	if option != nil {
		option.spec.persistent = true
	}

	return option
}

// Secret marks option values as sensitive for diagnostics and observability.
func (option *Option[T]) Secret() *Option[T] {
	if option != nil {
		option.spec.secret = true
	}

	return option
}

// Description documents the option in help and generated references.
func (option *Option[T]) Description(description string) *Option[T] {
	if option != nil {
		option.spec.description = description
	}

	return option
}

// Completion declares an explicit dynamic completion provider.
func (option *Option[T]) Completion(provider CompletionProvider) *Option[T] {
	if option != nil {
		option.spec.completion = provider
	}

	return option
}

// Default supplies a value when the option is omitted.
func (option *Option[T]) Default(value T) *Option[T] {
	if option != nil {
		option.spec.hasDefault = true
		option.spec.defaultVal = cloneValue(value)
	}

	return option
}

// Required rejects execution when the option is omitted and has no default.
func (option *Option[T]) Required() *Option[T] {
	if option != nil {
		option.spec.required = true
	}

	return option
}

// Get returns the typed value or its zero value when omitted.
func (option *Option[T]) Get(input Input) T {
	return bindingValue[T](input, option)
}

// State distinguishes omitted, defaulted, and explicit input.
func (option *Option[T]) State(input Input) ValueState {
	return bindingState(input, option)
}

func (option *Option[T]) optionSpecification() optionSpec {
	if option == nil {
		return optionSpec{}
	}

	return option.spec
}

// OptionMetadata is an immutable view of a compiled option.
type OptionMetadata struct {
	spec optionSpec
}

// Name returns the long option name without leading dashes.
func (metadata OptionMetadata) Name() string { return metadata.spec.name }

// Short returns the shorthand rune or zero when none is configured.
func (metadata OptionMetadata) Short() rune { return metadata.spec.short }

// Persistent reports whether descendants inherit the option.
func (metadata OptionMetadata) Persistent() bool { return metadata.spec.persistent }

// Secret reports whether values require redaction.
func (metadata OptionMetadata) Secret() bool { return metadata.spec.secret }

// ValueType returns the stable manifest name of the option value type.
func (metadata OptionMetadata) ValueType() string { return metadata.spec.valueType }

// AllowedValues returns a copy of the declared enum values, if any.
func (metadata OptionMetadata) AllowedValues() []string {
	if metadata.spec.secret {
		return nil
	}
	return cloneStrings(metadata.spec.allowed)
}

// Format returns the declared time layout, if any.
func (metadata OptionMetadata) Format() string { return metadata.spec.format }
