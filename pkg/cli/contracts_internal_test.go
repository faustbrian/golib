package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTypedDeclarationsExposeEverySupportedContract(t *testing.T) {
	t.Parallel()

	arguments := []ArgumentDefinition{
		StringArgument("string"), StringsArgument("strings"), IntArgument("int"),
		UintArgument("uint"), FloatArgument("float"), DurationArgument("duration"),
		TimeArgument("time", time.RFC3339), EnumArgument("enum", "one"),
		TypedArgument("typed", "upper", func(value string) (string, error) { return value, nil }),
	}
	options := []OptionDefinition{
		StringOption("string"), BoolOption("bool"), IntOption("int"),
		UintOption("uint"), FloatOption("float"), DurationOption("duration"),
		TimeOption("time", time.RFC3339), EnumOption("enum", "one"),
		StringsOption("strings"), KeyValuesOption("pairs"),
		TypedOption("typed", "upper", func(value string) (string, error) { return value, nil }),
	}
	for _, declaration := range arguments {
		if declaration.argumentSpecification().parse == nil {
			t.Fatal("argument constructor installed no parser")
		}
	}
	for _, declaration := range options {
		if declaration.optionSpecification().parse == nil {
			t.Fatal("option constructor installed no parser")
		}
	}

	var nilArgument *Argument[string]
	if nilArgument.Optional() != nil || nilArgument.Remainder() != nil ||
		nilArgument.Secret() != nil || nilArgument.Description("x") != nil ||
		nilArgument.Completion(nil) != nil || nilArgument.argumentSpecification().binding != nil {
		t.Fatal("nil argument builder is not safely inert")
	}
	var nilOption *Option[string]
	if nilOption.Short('x') != nil || nilOption.Persistent() != nil ||
		nilOption.Secret() != nil || nilOption.Description("x") != nil ||
		nilOption.Completion(nil) != nil || nilOption.Default("x") != nil ||
		nilOption.Required() != nil || nilOption.optionSpecification().binding != nil {
		t.Fatal("nil option builder is not safely inert")
	}

	argument := StringArgument("value").Optional().Remainder().Secret().Description("value")
	argument.Completion(func(context.Context, CompletionRequest) ([]CompletionCandidate, error) { return nil, nil })
	if specification := argument.argumentSpecification(); !specification.secret || specification.description != "value" || specification.completion == nil {
		t.Fatalf("argument specification = %#v", specification)
	}
	option := StringOption("value").Short('v').Persistent().Secret().Description("value").Default("default").Required()
	option.Completion(func(context.Context, CompletionRequest) ([]CompletionCandidate, error) { return nil, nil })
	if specification := option.optionSpecification(); !specification.secret || !specification.persistent || !specification.required || !specification.hasDefault || specification.completion == nil {
		t.Fatalf("option specification = %#v", specification)
	}
	metadata := OptionMetadata{spec: option.spec}
	if metadata.Name() != "value" || metadata.Short() != 'v' || !metadata.Persistent() || !metadata.Secret() || metadata.ValueType() != "string" {
		t.Fatalf("option metadata = %#v", metadata)
	}
}

func TestScalarParserPreservesExactTokensAndFailures(t *testing.T) {
	t.Parallel()

	if scalarParser[string](nil) != nil {
		t.Fatal("nil typed parser was not preserved")
	}
	parserFailure := errors.New("parser")
	parser := scalarParser(func(value string) (string, error) {
		if value == "bad" {
			return "", parserFailure
		}
		return value, nil
	})
	if _, err := parser(nil); err == nil {
		t.Fatal("empty scalar input succeeded")
	}
	if _, err := parser([]string{"bad"}); !errors.Is(err, parserFailure) {
		t.Fatalf("parser error = %v", err)
	}
	if value, err := parser([]string{"first", "last"}); err != nil || value != "last" {
		t.Fatalf("value/error = %q/%v", value, err)
	}
}

func TestCompilationRejectsEveryMalformedDeclarationClass(t *testing.T) {
	t.Parallel()

	noop := func(context.Context, Invocation) error { return nil }
	validation := func(context.Context, Input) error { return nil }
	middleware := func(ctx context.Context, _ CommandMetadata, next Next) error { return next(ctx) }
	validOption := StringOption("valid")
	validArgument := StringArgument("valid")

	cases := []struct {
		name string
		root *Command
	}{
		{"nil validation", NewCommand("tool", WithValidation(validation, nil))},
		{"nil middleware", NewCommand("tool", WithMiddleware(middleware, nil))},
		{"nil pre-run", NewCommand("tool", WithPreRun(noop, nil))},
		{"nil post-run", NewCommand("tool", WithPostRun(noop, nil))},
		{"nil cleanup", NewCommand("tool", WithCleanup(noop, nil))},
		{"duplicate root alias", NewCommand("tool", WithAliases("tool"))},
		{"duplicate aliases", NewCommand("tool", WithAliases("alias", "alias"))},
		{"invalid alias", NewCommand("tool", WithAliases("bad alias"))},
		{"nil child", NewCommand("tool", WithSubcommands(nil))},
		{"duplicate child", NewCommand("tool", WithSubcommands(NewCommand("child"), NewCommand("child")))},
		{"child alias shadows name", NewCommand("tool", WithSubcommands(NewCommand("one", WithAliases("two")), NewCommand("two")))},
		{"nil argument", NewCommand("tool", WithArguments(nil))},
		{"argument nil parser", NewCommand("tool", WithArguments(argumentStub{spec: argumentSpec{name: "arg", valueType: "string", binding: struct{}{}, cardinality: ArgumentRequired}}))},
		{"secret argument completion", NewCommand("tool", WithArguments(argumentStub{spec: argumentSpec{name: "arg", valueType: "string", binding: struct{}{}, cardinality: ArgumentRequired, secret: true, completion: completionStub, parse: parseString}}))},
		{"argument missing binding", NewCommand("tool", WithArguments(argumentStub{spec: argumentSpec{name: "arg", valueType: "string", cardinality: ArgumentRequired, parse: parseString}}))},
		{"duplicate argument", NewCommand("tool", WithArguments(StringArgument("arg"), StringArgument("arg")))},
		{"required after optional", NewCommand("tool", WithArguments(StringArgument("first").Optional(), StringArgument("second")))},
		{"repeated not final", NewCommand("tool", WithArguments(StringsArgument("first"), StringArgument("second")))},
		{"remainder not final", NewCommand("tool", WithArguments(StringsArgument("first").Remainder(), StringArgument("second")))},
		{"invalid cardinality", NewCommand("tool", WithArguments(argumentStub{spec: argumentSpec{name: "arg", valueType: "string", binding: struct{}{}, cardinality: 99, parse: parseString}}))},
		{"nil option", NewCommand("tool", WithOptions(nil))},
		{"option nil parser", NewCommand("tool", WithOptions(optionStub{spec: optionSpec{name: "flag", valueType: "string", binding: struct{}{}}}))},
		{"secret option completion", NewCommand("tool", WithOptions(optionStub{spec: optionSpec{name: "flag", valueType: "string", binding: struct{}{}, secret: true, completion: completionStub, parse: parseString}}))},
		{"option missing binding", NewCommand("tool", WithOptions(optionStub{spec: optionSpec{name: "flag", valueType: "string", parse: parseString}}))},
		{"reserved help", NewCommand("tool", WithOptions(StringOption("help")))},
		{"reserved version", NewCommand("tool", WithOptions(StringOption("version")))},
		{"reserved short help", NewCommand("tool", WithOptions(StringOption("flag").Short('h')))},
		{"duplicate option", NewCommand("tool", WithOptions(StringOption("flag"), StringOption("flag")))},
		{"invalid short", NewCommand("tool", WithOptions(StringOption("flag").Short('-')))},
		{"duplicate short", NewCommand("tool", WithOptions(StringOption("one").Short('x'), StringOption("two").Short('x')))},
		{"short shadows inherited", NewCommand("tool", WithOptions(StringOption("root").Short('x').Persistent()), WithSubcommands(NewCommand("child", WithOptions(StringOption("child").Short('x')))))},
		{"group too small", NewCommand("tool", WithOptions(validOption), WithMutuallyExclusive(validOption))},
		{"group nil", NewCommand("tool", WithOptions(validOption), WithMutuallyExclusive(validOption, nil))},
		{"group duplicate", NewCommand("tool", WithOptions(validOption), WithMutuallyExclusive(validOption, validOption))},
		{"invalid group kind", NewCommand("tool", WithOptions(validOption), func(command *Command) {
			command.groups = []optionGroupDefinition{{kind: 99, options: []OptionDefinition{validOption, StringOption("other")}}}
			command.options = append(command.options, command.groups[0].options[1])
		})},
		{"invalid interaction", NewCommand("tool", WithInteraction(99))},
		{"reused argument binding", NewCommand("tool", WithSubcommands(
			NewCommand("one", WithArguments(validArgument)),
			NewCommand("two", WithArguments(validArgument)),
		))},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(test.root); !errors.Is(err, ErrInternal) {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}
}

func TestCompileOptionsAndMetadataBoundaries(t *testing.T) {
	t.Parallel()

	optionFailure := errors.New("option")
	if _, err := Compile(NewCommand("tool"), nil, func(*compileConfiguration) error { return optionFailure }); !errors.Is(err, optionFailure) {
		t.Fatalf("compile option error = %v", err)
	}
	overrides := Limits{
		MaximumCommandDepth: 2, MaximumCommands: 3,
		MaximumOptionsPerCommand: 4, MaximumArgumentsPerCommand: 5,
		MaximumArguments: 6, MaximumArgvBytes: 7, MaximumMetadataBytes: 8,
		MaximumCompletionResults: 9, MaximumCompletionBytes: 10,
	}
	configuration := compileConfiguration{limits: defaultLimits()}
	if err := WithLimits(overrides)(&configuration); err != nil || configuration.limits != overrides {
		t.Fatalf("limits = %#v, error = %v", configuration.limits, err)
	}
	for index := range 9 {
		invalid := overrides
		fields := []*int{
			&invalid.MaximumCommandDepth, &invalid.MaximumCommands,
			&invalid.MaximumOptionsPerCommand, &invalid.MaximumArgumentsPerCommand,
			&invalid.MaximumArguments, &invalid.MaximumArgvBytes,
			&invalid.MaximumMetadataBytes,
			&invalid.MaximumCompletionResults, &invalid.MaximumCompletionBytes,
		}
		*fields[index] = -1
		if err := validateLimits(invalid); !errors.Is(err, ErrInternal) {
			t.Fatalf("invalid limit %d error = %v", index, err)
		}
	}

	child := NewCommand("child", WithAliases("alias"), WithSummary("summary"), WithDescription("description"), WithExamples("example"), WithDocumentation("docs"), WithHidden(true), WithExperimental(true), WithDeprecated("deprecated"), WithReplacement("replacement"), WithOptions(StringOption("flag").Short('f').Persistent().Secret()))
	application, err := Compile(NewCommand("tool", WithSubcommands(child)))
	if err != nil {
		t.Fatal(err)
	}
	metadata := application.Root().Children()[0]
	if metadata.Name() != "child" || metadata.Aliases()[0] != "alias" || metadata.Summary() != "summary" || metadata.Description() != "description" || metadata.Examples()[0] != "example" || metadata.Documentation() != "docs" || !metadata.Hidden() || !metadata.Experimental() || metadata.Deprecated() != "deprecated" || metadata.Replacement() != "replacement" || len(metadata.Options()) != 1 {
		t.Fatalf("metadata = %#v", metadata)
	}
	var nilApplication *Application
	empty := nilApplication.Root()
	if empty.Name() != "" || empty.Aliases() != nil || empty.Summary() != "" || empty.Description() != "" || empty.Examples() != nil || empty.Documentation() != "" || empty.Hidden() || empty.Experimental() || empty.Deprecated() != "" || empty.Replacement() != "" || empty.Children() != nil || empty.Options() != nil {
		t.Fatalf("empty metadata = %#v", empty)
	}
}

func TestUnknownArgumentCardinalityHasStableDocumentation(t *testing.T) {
	t.Parallel()

	if got := argumentCardinalityName(99); got != "unknown" {
		t.Fatalf("argumentCardinalityName(99) = %q, want unknown", got)
	}
}

func TestConstructionPrimitiveBoundaries(t *testing.T) {
	t.Parallel()

	var nilCommand *Command
	if err := nilCommand.AddSubcommands(NewCommand("child")); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil command error = %v", err)
	}
	if err := NewCommand("tool").AddSubcommands(nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil child error = %v", err)
	}
	if _, err := Compile(nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil root error = %v", err)
	}
	state := &compilationState{nodes: map[*Command]uint8{}, bindings: map[any]string{}, limits: defaultLimits()}
	if _, err := compileCommand(nil, nil, nil, nil, state, 1); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil internal command error = %v", err)
	}
	tooManyArguments := NewCommand("tool", WithArguments(StringArgument("one"), StringArgument("two")))
	if _, err := Compile(tooManyArguments, WithLimits(Limits{MaximumArgumentsPerCommand: 1})); !errors.Is(err, ErrInternal) {
		t.Fatalf("argument definition limit error = %v", err)
	}
	tooManyOptions := NewCommand("tool", WithOptions(StringOption("one"), StringOption("two")))
	if _, err := Compile(tooManyOptions, WithLimits(Limits{MaximumOptionsPerCommand: 1})); !errors.Is(err, ErrInternal) {
		t.Fatalf("option definition limit error = %v", err)
	}
	for _, root := range []*Command{
		NewCommand("tool", WithArguments(StringArgument("bad name"))),
		NewCommand("tool", WithOptions(StringOption("bad_name"))),
		NewCommand(""), NewCommand("-tool"), NewCommand(string([]byte{0xff})),
	} {
		if _, err := Compile(root); !errors.Is(err, ErrInternal) {
			t.Fatalf("malformed declaration error = %v", err)
		}
	}
}

type argumentStub struct{ spec argumentSpec }

func (stub argumentStub) argumentSpecification() argumentSpec { return stub.spec }

type optionStub struct{ spec optionSpec }

func (stub optionStub) optionSpecification() optionSpec { return stub.spec }

func completionStub(context.Context, CompletionRequest) ([]CompletionCandidate, error) {
	return nil, nil
}
