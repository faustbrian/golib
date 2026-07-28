package benchmarks

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/cli/internal/engine"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestOwnedParserMatchesTheFormerCobraAdapter(t *testing.T) {
	root := differentialCommand()
	cases := map[string][]string{
		"root local option":                {"--local", "value"},
		"long boolean":                     {"--verbose"},
		"assigned long boolean":            {"--verbose=false"},
		"child separate value":             {"deploy", "-n", "value"},
		"child attached value":             {"deploy", "-nvalue"},
		"combined boolean shorthands":      {"deploy", "-vf"},
		"persistent before child":          {"-v", "deploy"},
		"persistent after child":           {"deploy", "-v"},
		"command alias":                    {"ship"},
		"nested persistent before child":   {"workflow", "--region", "eu", "release"},
		"nested persistent after child":    {"workflow", "release", "--region", "eu"},
		"repeated assigned empty value":    {"deploy", "--name=first", "--name="},
		"interspersed option":              {"deploy", "target", "--force"},
		"option terminator":                {"deploy", "--", "--force"},
		"negative sibling positional":      {"number", "-1"},
		"digit shorthand":                  {"digits", "-1"},
		"child help":                       {"deploy", "--help"},
		"version option":                   {"--version"},
		"version command":                  {"version"},
		"unknown command":                  {"deply"},
		"unknown option":                   {"--missing"},
		"missing value":                    {"--local"},
		"parent local before child":        {"--local", "value", "deploy"},
		"parent local before grandchild":   {"workflow", "--force", "release"},
		"unknown option with negative arg": {"number", "--missing", "-1"},
	}
	for name, argv := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := engine.Parse(context.Background(), root, argv)
			want, wantErr := parseWithFormerCobraAdapter(root, argv)
			if gotKind, wantKind := differentialFailure(gotErr), differentialFailure(wantErr); gotKind != wantKind {
				t.Fatalf(
					"failure kind = %v, want %v; got error %v, Cobra error %v",
					gotKind,
					wantKind,
					gotErr,
					wantErr,
				)
			}
			if gotErr == nil && !differentialResultsEqual(got, want) {
				t.Fatalf("result = %#v, want former Cobra result %#v", got, want)
			}
		})
	}
}

func differentialCommand() engine.Command {
	return engine.Command{
		ID: 1, Name: "tool", Version: "1.2.3",
		Options: []engine.Option{
			{Key: 1, Name: "local"},
			{Key: 2, Name: "verbose", Short: 'v', Persistent: true, Boolean: true},
		},
		Children: []engine.Command{
			{
				ID: 2, Name: "deploy", Aliases: []string{"ship"},
				Options: []engine.Option{
					{Key: 3, Name: "name", Short: 'n'},
					{Key: 4, Name: "force", Short: 'f', Boolean: true},
				},
			},
			{
				ID: 3, Name: "workflow",
				Options: []engine.Option{
					{Key: 5, Name: "region", Persistent: true},
					{Key: 7, Name: "force", Boolean: true},
				},
				Children: []engine.Command{{ID: 4, Name: "release"}},
			},
			{ID: 5, Name: "number"},
			{ID: 6, Name: "digits", Options: []engine.Option{
				{Key: 6, Name: "one", Short: '1', Boolean: true},
			}},
		},
	}
}

func differentialResultsEqual(left, right engine.Result) bool {
	return left.CommandID == right.CommandID &&
		left.Action == right.Action &&
		slices.Equal(left.Arguments, right.Arguments) &&
		maps.EqualFunc(left.Options, right.Options, slices.Equal)
}

func parseWithFormerCobraAdapter(
	root engine.Command,
	argv []string,
) (engine.Result, error) {
	if differentialHasDigitShorthand(root) {
		result, err := parseCobra(root, argv)
		if err == nil || !differentialShouldRetryNegative(err, argv) {
			return result, err
		}
	}

	return parseCobra(root, differentialEncodeNegative(root, argv))
}

func parseCobra(
	root engine.Command,
	argv []string,
) (engine.Result, error) {
	result := engine.Result{CommandID: -1, Options: make(map[int][]string)}
	values := make(map[int]*differentialRawValue)
	command := differentialCobraCommand(root, values, &result)
	if root.Version != "" {
		version := &differentialRawValue{boolean: true}
		values[-1] = version
		command.PersistentFlags().Var(version, "version", "")
		command.PersistentFlags().Lookup("version").NoOptDefVal = "true"
	}
	if root.Version != "" && len(root.Children) > 0 &&
		len(argv) > 0 && argv[0] == "version" {
		argv = append([]string(nil), argv...)
		argv[0] = "--version"
	}
	command.SetArgs(argv)
	command.SetIn(nil)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.CompletionOptions.DisableDefaultCmd = true
	command.SetHelpCommand(&cobra.Command{Hidden: true})

	if err := command.ExecuteContext(context.Background()); err != nil {
		return engine.Result{}, differentialClassifyCobraFailure(err)
	}
	if version := values[-1]; version != nil && len(version.values) > 0 {
		result.Action = engine.ActionVersion
		result.CommandID = root.ID
	}
	for key, value := range values {
		if key >= 0 && len(value.values) > 0 {
			result.Options[key] = append([]string(nil), value.values...)
		}
	}

	return result, nil
}

func differentialCobraCommand(
	definition engine.Command,
	values map[int]*differentialRawValue,
	result *engine.Result,
) *cobra.Command {
	command := &cobra.Command{
		Use:                definition.Name,
		Short:              definition.Summary,
		Aliases:            append([]string(nil), definition.Aliases...),
		DisableSuggestions: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			result.CommandID = definition.ID
			result.Arguments = differentialDecodeNegative(args)

			return nil
		},
	}
	command.SetHelpFunc(func(*cobra.Command, []string) {
		result.Action = engine.ActionHelp
		result.CommandID = definition.ID
	})
	for _, option := range definition.Options {
		value := &differentialRawValue{boolean: option.Boolean}
		values[option.Key] = value
		flags := command.Flags()
		if option.Persistent {
			flags = command.PersistentFlags()
		}
		short := ""
		if option.Short != 0 {
			short = string(option.Short)
		}
		flags.VarP(value, option.Name, short, "")
		if option.Boolean {
			flags.Lookup(option.Name).NoOptDefVal = "true"
		}
	}
	for _, child := range definition.Children {
		command.AddCommand(differentialCobraCommand(child, values, result))
	}

	return command
}

type differentialRawValue struct {
	boolean bool
	values  []string
}

func (value *differentialRawValue) Set(raw string) error {
	value.values = append(value.values, raw)

	return nil
}

func (value *differentialRawValue) String() string { return "" }

func (value *differentialRawValue) Type() string { return "value" }

func (value *differentialRawValue) IsBoolFlag() bool { return value.boolean }

var _ pflag.Value = (*differentialRawValue)(nil)

func differentialClassifyCobraFailure(err error) error {
	message := err.Error()
	kind := engine.FailureUsage
	switch {
	case strings.HasPrefix(message, "unknown command"):
		kind = engine.FailureUnknownCommand
	case strings.Contains(message, "unknown flag"),
		strings.Contains(message, "unknown shorthand"):
		kind = engine.FailureUnknownOption
	case strings.Contains(message, "needs an argument"):
		kind = engine.FailureMissingValue
	}

	return &engine.ParseError{Kind: kind}
}

func differentialFailure(err error) engine.FailureKind {
	if err == nil {
		return engine.FailureKind(255)
	}
	var parseErr *engine.ParseError
	if !errors.As(err, &parseErr) {
		return engine.FailureUsage
	}

	return parseErr.Kind
}

func differentialHasDigitShorthand(command engine.Command) bool {
	for _, option := range command.Options {
		if option.Short >= '0' && option.Short <= '9' {
			return true
		}
	}
	for _, child := range command.Children {
		if differentialHasDigitShorthand(child) {
			return true
		}
	}

	return false
}

func differentialShouldRetryNegative(err error, argv []string) bool {
	var parseErr *engine.ParseError
	if !errors.As(err, &parseErr) ||
		parseErr.Kind != engine.FailureUnknownOption {
		return false
	}
	for _, token := range argv {
		if differentialLooksNegative(token) {
			return true
		}
	}

	return false
}

const differentialNegativePrefix = "\x00cli-negative:"

func differentialEncodeNegative(
	root engine.Command,
	argv []string,
) []string {
	nonBooleanLong := make(map[string]struct{})
	nonBooleanShort := make(map[string]struct{})
	differentialCollectValueOptions(root, nonBooleanLong, nonBooleanShort)
	encoded := append([]string(nil), argv...)
	previousConsumesValue := false
	for index, token := range encoded {
		if previousConsumesValue {
			previousConsumesValue = false
			continue
		}
		if strings.HasPrefix(token, "--") && !strings.Contains(token, "=") {
			_, previousConsumesValue =
				nonBooleanLong[strings.TrimPrefix(token, "--")]
			continue
		}
		if len(token) == 2 && token[0] == '-' {
			if _, consumesValue := nonBooleanShort[token[1:]]; consumesValue {
				previousConsumesValue = true
				continue
			}
		}
		if differentialLooksNegative(token) {
			encoded[index] = differentialNegativePrefix + token
		}
	}

	return encoded
}

func differentialCollectValueOptions(
	command engine.Command,
	long map[string]struct{},
	short map[string]struct{},
) {
	for _, option := range command.Options {
		if option.Boolean {
			continue
		}
		long[option.Name] = struct{}{}
		if option.Short != 0 {
			short[string(option.Short)] = struct{}{}
		}
	}
	for _, child := range command.Children {
		differentialCollectValueOptions(child, long, short)
	}
}

func differentialLooksNegative(token string) bool {
	if len(token) < 2 || token[0] != '-' {
		return false
	}
	if token[1] >= '0' && token[1] <= '9' {
		return true
	}

	return len(token) > 2 && token[1] == '.' &&
		token[2] >= '0' && token[2] <= '9'
}

func differentialDecodeNegative(values []string) []string {
	decoded := make([]string, len(values))
	for index, value := range values {
		decoded[index] = strings.TrimPrefix(value, differentialNegativePrefix)
	}

	return decoded
}
