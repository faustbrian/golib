// Package engine contains the replaceable Cobra parsing adapter.
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command is the engine-neutral subset required for token parsing.
type Command struct {
	ID       int
	Name     string
	Aliases  []string
	Summary  string
	Version  string
	Options  []Option
	Children []Command
}

// Completion generates a deterministic Cobra completion script.
func Completion(root Command, shell string) (string, error) {
	var output bytes.Buffer
	if err := generateCompletion(root, shell, &output); err != nil {
		return "", err
	}

	return output.String(), nil
}

func generateCompletion(command Command, shell string, writer io.Writer) error {
	function := completionFunction(command.Name)
	var template string
	switch shell {
	case "bash":
		template = bashCompletion
	case "zsh":
		template = zshCompletion
	case "fish":
		template = fishCompletion
	case "powershell":
		template = powerShellCompletion
	default:
		return &UnsupportedShellError{Shell: shell}
	}
	template = strings.ReplaceAll(template, "{{FUNCTION}}", function)
	template = strings.ReplaceAll(template, "{{POSIX_COMMAND}}", posixQuote(command.Name))
	template = strings.ReplaceAll(template, "{{FISH_COMMAND}}", fishQuote(command.Name))
	template = strings.ReplaceAll(template, "{{POWERSHELL_COMMAND}}", powerShellQuote(command.Name))
	written, err := io.WriteString(writer, template)
	if err != nil {
		return err
	}
	if written != len(template) {
		return io.ErrShortWrite
	}
	return nil
}

func completionFunction(name string) string {
	digest := sha256.Sum256([]byte(name))
	return fmt.Sprintf("__go_cli_%x_complete", digest[:6])
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func fishQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

const bashCompletion = `# bash completion for {{POSIX_COMMAND}}
{{FUNCTION}}() {
    local line
    COMPREPLY=()
    while IFS= read -r line; do
        if [[ "$line" =~ ^:[0-9]+$ ]]; then
            continue
        fi
        COMPREPLY+=("${line%%$'\t'*}")
    done < <(command "${COMP_WORDS[0]}" __complete \
        "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
complete -o default -F {{FUNCTION}} -- {{POSIX_COMMAND}}
`

const zshCompletion = `#compdef {{POSIX_COMMAND}}
{{FUNCTION}}() {
    local -a arguments output candidates descriptions
    local line candidate description
    arguments=("${words[@]:0:$CURRENT}")
    arguments[1]=()
    output=("${(@f)$(command "$words[1]" __complete \
        "${arguments[@]}" 2>/dev/null)}")
    for line in "${output[@]}"; do
        if [[ "$line" == :<-> ]]; then
            continue
        fi
        candidate="${line%%$'\t'*}"
        description="${line#*$'\t'}"
        if [[ "$description" == "$line" ]]; then
            description=""
        fi
        candidates+=("$candidate")
        descriptions+=("$description")
    done
    compadd -Q -d descriptions -- "${candidates[@]}"
}
compdef {{FUNCTION}} -- {{POSIX_COMMAND}}
`

const fishCompletion = `function {{FUNCTION}}
    set -l tokens (commandline -opc)
    set -l current (commandline -ct)
    set -a tokens "$current"
    command $tokens[1] __complete $tokens[2..-1] 2>/dev/null |
        string match -rv '^:[0-9]+$'
end
complete -c {{FISH_COMMAND}} -f -a '({{FUNCTION}})'
`

const powerShellCompletion = `$script:{{FUNCTION}} = {
    param($CommandName, $ParameterName, $WordToComplete, $CommandAst, $FakeBoundParameters)
    $arguments = @('__complete')
    foreach ($element in $CommandAst.CommandElements | Select-Object -Skip 1) {
        if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
            $arguments += $element.Value
        } else {
            $arguments += $element.Extent.Text
        }
    }
    $output = & {{POWERSHELL_COMMAND}} @arguments 2>$null
    foreach ($line in $output) {
        if ($line -match '^:[0-9]+$') {
            continue
        }
        $parts = $line -split ([char]9), 2
        $candidate = $parts[0]
        $description = ''
        if ($parts.Length -gt 1) {
            $description = $parts[1]
        }
        [System.Management.Automation.CompletionResult]::new(
            $candidate, $candidate, 'ParameterValue', $description
        )
    }
}
Register-ArgumentCompleter -CommandName {{POWERSHELL_COMMAND}} -ScriptBlock $script:{{FUNCTION}}
`

// UnsupportedShellError identifies an unsupported generator name.
type UnsupportedShellError struct{ Shell string }

func (err *UnsupportedShellError) Error() string { return "unsupported shell: " + err.Shell }

// Option declares one parser option.
type Option struct {
	Key        int
	Name       string
	Short      rune
	Persistent bool
	Boolean    bool
}

// Result contains raw values for owned typed conversion.
type Result struct {
	CommandID int
	Action    Action
	Arguments []string
	Options   map[int][]string
}

// Action identifies parser-owned terminal requests.
type Action uint8

const (
	// ActionRun dispatches the selected command.
	ActionRun Action = iota
	// ActionHelp requests framework-generated help.
	ActionHelp
	// ActionVersion requests root version output.
	ActionVersion
)

// FailureKind is a stable adapter-level parse classification.
type FailureKind uint8

const (
	// FailureUsage identifies a general parsing failure.
	FailureUsage FailureKind = iota
	// FailureUnknownCommand identifies an unknown command token.
	FailureUnknownCommand
	// FailureUnknownOption identifies an unknown option token.
	FailureUnknownOption
	// FailureMissingValue identifies an option missing its value.
	FailureMissingValue
)

// ParseError insulates public classification from Cobra error strings.
type ParseError struct {
	Kind FailureKind
}

func (err *ParseError) Error() string {
	switch err.Kind {
	case FailureUsage:
		return "invalid arguments"
	case FailureUnknownCommand:
		return "unknown command"
	case FailureUnknownOption:
		return "unknown option"
	case FailureMissingValue:
		return "option requires a value"
	default:
		return "invalid arguments"
	}
}

// Parse builds fresh mutable Cobra state and parses one invocation.
func Parse(ctx context.Context, root Command, argv []string) (Result, error) {
	if hasDigitShorthand(root) {
		result, err := parse(ctx, root, argv)
		if err == nil || !shouldRetryNegativePositionals(err, argv) {
			return result, err
		}
	}

	return parse(ctx, root, encodeNegativePositionals(root, argv))
}

func parse(ctx context.Context, root Command, argv []string) (Result, error) {
	result := Result{CommandID: -1, Options: make(map[int][]string)}
	values := make(map[int]*rawValue)
	command := build(root, values, &result)
	if root.Version != "" {
		version := &rawValue{boolean: true}
		values[-1] = version
		command.PersistentFlags().Var(version, "version", "")
		command.PersistentFlags().Lookup("version").NoOptDefVal = "true"
	}
	command.SetArgs(argv)
	command.SetIn(nil)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.CompletionOptions.DisableDefaultCmd = true
	command.SetHelpCommand(&cobra.Command{Hidden: true})

	if err := command.ExecuteContext(ctx); err != nil {
		return Result{}, classifyFailure(err)
	}
	if version := values[-1]; version != nil && len(version.values) > 0 {
		result.Action = ActionVersion
		result.CommandID = root.ID
	}
	for key, value := range values {
		if key >= 0 && len(value.values) > 0 {
			result.Options[key] = append([]string(nil), value.values...)
		}
	}

	return result, nil
}

func hasDigitShorthand(command Command) bool {
	for _, option := range command.Options {
		if option.Short >= '0' && option.Short <= '9' {
			return true
		}
	}
	for _, child := range command.Children {
		if hasDigitShorthand(child) {
			return true
		}
	}

	return false
}

func shouldRetryNegativePositionals(err error, argv []string) bool {
	var parseErr *ParseError
	if !errors.As(err, &parseErr) || parseErr.Kind != FailureUnknownOption {
		return false
	}
	for _, token := range argv {
		if looksNegativeValue(token) {
			return true
		}
	}

	return false
}

func build(definition Command, values map[int]*rawValue, result *Result) *cobra.Command {
	command := &cobra.Command{
		Use:                definition.Name,
		Short:              definition.Summary,
		Aliases:            append([]string(nil), definition.Aliases...),
		DisableSuggestions: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			result.CommandID = definition.ID
			result.Arguments = decodeNegativePositionals(args)
			return nil
		},
	}
	command.SetHelpFunc(func(*cobra.Command, []string) {
		result.Action = ActionHelp
		result.CommandID = definition.ID
	})
	for _, option := range definition.Options {
		value := &rawValue{boolean: option.Boolean}
		values[option.Key] = value
		set := command.Flags()
		if option.Persistent {
			set = command.PersistentFlags()
		}
		short := ""
		if option.Short != 0 {
			short = string(option.Short)
		}
		set.VarP(value, option.Name, short, "")
		if option.Boolean {
			set.Lookup(option.Name).NoOptDefVal = "true"
		}
	}
	for _, child := range definition.Children {
		command.AddCommand(build(child, values, result))
	}

	return command
}

type rawValue struct {
	boolean bool
	values  []string
}

func (value *rawValue) Set(raw string) error {
	value.values = append(value.values, raw)

	return nil
}

func (value *rawValue) String() string { return "" }

func (value *rawValue) Type() string { return "value" }

func (value *rawValue) IsBoolFlag() bool { return value.boolean }

var _ pflag.Value = (*rawValue)(nil)

func classifyFailure(err error) error {
	message := err.Error()
	kind := FailureUsage
	switch {
	case strings.HasPrefix(message, "unknown command"):
		kind = FailureUnknownCommand
	case strings.Contains(message, "unknown flag"), strings.Contains(message, "unknown shorthand"):
		kind = FailureUnknownOption
	case strings.Contains(message, "needs an argument"):
		kind = FailureMissingValue
	}

	return &ParseError{Kind: kind}
}

const negativePrefix = "\x00cli-negative:"

func encodeNegativePositionals(root Command, argv []string) []string {
	nonBooleanLong := make(map[string]struct{})
	nonBooleanShort := make(map[string]struct{})
	collectValueOptions(root, nonBooleanLong, nonBooleanShort)
	encoded := append([]string(nil), argv...)
	previousConsumesValue := false
	for index, token := range encoded {
		if previousConsumesValue {
			previousConsumesValue = false
			continue
		}
		if strings.HasPrefix(token, "--") && !strings.Contains(token, "=") {
			_, previousConsumesValue = nonBooleanLong[strings.TrimPrefix(token, "--")]
			continue
		}
		if len(token) == 2 && token[0] == '-' {
			if _, consumesValue := nonBooleanShort[token[1:]]; consumesValue {
				previousConsumesValue = true
				continue
			}
		}
		if looksNegativeValue(token) {
			encoded[index] = negativePrefix + token
		}
	}

	return encoded
}

func collectValueOptions(
	command Command,
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
		collectValueOptions(child, long, short)
	}
}

func looksNegativeValue(token string) bool {
	if len(token) < 2 || token[0] != '-' {
		return false
	}
	if token[1] >= '0' && token[1] <= '9' {
		return true
	}

	return len(token) > 2 && token[1] == '.' && token[2] >= '0' && token[2] <= '9'
}

func decodeNegativePositionals(values []string) []string {
	decoded := make([]string, len(values))
	for index, value := range values {
		decoded[index] = strings.TrimPrefix(value, negativePrefix)
	}

	return decoded
}
