package cli

import "time"

// ArgumentCardinality defines how many positional tokens an argument accepts.
type ArgumentCardinality uint8

const (
	// ArgumentRequired consumes exactly one token.
	ArgumentRequired ArgumentCardinality = iota + 1
	// ArgumentOptional consumes zero or one token.
	ArgumentOptional
	// ArgumentRepeated consumes zero or more tokens and must be final.
	ArgumentRepeated
	// ArgumentRemainder consumes every remaining token and must be final.
	ArgumentRemainder
)

// ArgumentDefinition is a typed positional declaration accepted by commands.
type ArgumentDefinition interface {
	argumentSpecification() argumentSpec
}

// Parser converts one exact argv token into an application-owned type.
type Parser[T any] func(string) (T, error)

type argumentSpec struct {
	key         int
	binding     any
	name        string
	valueType   string
	cardinality ArgumentCardinality
	secret      bool
	description string
	allowed     []string
	format      string
	hasFormat   bool
	parse       func([]string) (any, error)
	completion  CompletionProvider
}

// Argument is a typed positional argument binding.
type Argument[T any] struct {
	spec argumentSpec
}

// StringArgument creates a required string argument.
func StringArgument(name string) *Argument[string] {
	return newArgument[string](name, "string", ArgumentRequired, parseString)
}

// StringsArgument creates a repeated string argument.
func StringsArgument(name string) *Argument[[]string] {
	return newArgument[[]string](name, "string-slice", ArgumentRepeated, parseStrings)
}

// IntArgument creates a signed 64-bit integer argument.
func IntArgument(name string) *Argument[int64] {
	return newArgument[int64](name, "integer", ArgumentRequired, parseInt)
}

// UintArgument creates an unsigned 64-bit integer argument.
func UintArgument(name string) *Argument[uint64] {
	return newArgument[uint64](name, "unsigned-integer", ArgumentRequired, parseUint)
}

// FloatArgument creates a 64-bit floating-point argument.
func FloatArgument(name string) *Argument[float64] {
	return newArgument[float64](name, "float", ArgumentRequired, parseFloat)
}

// DurationArgument creates a time.Duration argument.
func DurationArgument(name string) *Argument[time.Duration] {
	return newArgument[time.Duration](name, "duration", ArgumentRequired, parseDuration)
}

// TimeArgument creates a time.Time argument parsed with the supplied layout.
func TimeArgument(name, layout string) *Argument[time.Time] {
	argument := newArgument[time.Time](name, "time", ArgumentRequired, parseTime(layout))
	argument.spec.format = layout
	argument.spec.hasFormat = true

	return argument
}

// EnumArgument creates a string argument constrained to supplied values.
func EnumArgument(name string, values ...string) *Argument[string] {
	argument := newArgument[string](name, "enum", ArgumentRequired, parseEnum(values))
	argument.spec.allowed = append([]string{}, values...)

	return argument
}

// TypedArgument creates an engine-independent custom scalar argument.
func TypedArgument[T any](name, valueType string, parser Parser[T]) *Argument[T] {
	return newArgument[T](name, valueType, ArgumentRequired, scalarParser(parser))
}

func newArgument[T any](
	name string,
	valueType string,
	cardinality ArgumentCardinality,
	parse func([]string) (any, error),
) *Argument[T] {
	argument := &Argument[T]{spec: argumentSpec{
		name:        name,
		valueType:   valueType,
		cardinality: cardinality,
		parse:       parse,
	}}
	argument.spec.binding = argument

	return argument
}

func scalarParser[T any](parser Parser[T]) func([]string) (any, error) {
	if parser == nil {
		return nil
	}
	return func(values []string) (any, error) {
		value, err := last(values)
		if err != nil {
			return nil, err
		}

		return parser(value)
	}
}

// Get returns the typed value or its zero value when omitted.
func (argument *Argument[T]) Get(input Input) T {
	return bindingValue[T](input, argument)
}

// State distinguishes omitted, defaulted, and explicit input.
func (argument *Argument[T]) State(input Input) ValueState {
	return bindingState(input, argument)
}

// Optional makes a scalar argument optional.
func (argument *Argument[T]) Optional() *Argument[T] {
	if argument != nil {
		argument.spec.cardinality = ArgumentOptional
	}

	return argument
}

// Remainder consumes all remaining positional tokens without option parsing.
func (argument *Argument[T]) Remainder() *Argument[T] {
	if argument != nil {
		argument.spec.cardinality = ArgumentRemainder
	}

	return argument
}

// Secret marks values as sensitive for diagnostics and observability.
func (argument *Argument[T]) Secret() *Argument[T] {
	if argument != nil {
		argument.spec.secret = true
	}

	return argument
}

// Description documents the argument in help and generated references.
func (argument *Argument[T]) Description(description string) *Argument[T] {
	if argument != nil {
		argument.spec.description = description
	}

	return argument
}

// Completion declares an explicit dynamic completion provider.
func (argument *Argument[T]) Completion(provider CompletionProvider) *Argument[T] {
	if argument != nil {
		argument.spec.completion = provider
	}

	return argument
}

func (argument *Argument[T]) argumentSpecification() argumentSpec {
	if argument == nil {
		return argumentSpec{}
	}

	return argument.spec
}
