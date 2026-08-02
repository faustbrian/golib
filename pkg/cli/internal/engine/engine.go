// Package engine contains the dependency-free argv parsing boundary.
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// Command is the parser-neutral subset required for token parsing.
type Command struct {
	ID       int
	Name     string
	Aliases  []string
	Summary  string
	Version  string
	Options  []Option
	Children []Command
}

// Completion generates a deterministic completion script.
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

// ParseError insulates public classification from parser implementation
// details.
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

// Parse evaluates one immutable command tree without global parser state.
func Parse(ctx context.Context, root Command, argv []string) (Result, error) {
	if ctx == nil {
		return Result{}, &ParseError{Kind: FailureUsage}
	}
	parser := invocationParser{
		current: &root,
		result: Result{
			CommandID: root.ID,
			Action:    ActionRun,
			Options:   make(map[int][]string),
		},
	}

	return parser.parse(argv)
}

type invocationParser struct {
	root       *Command
	current    *Command
	inherited  []Option
	result     Result
	positional bool
}

func (parser *invocationParser) parse(argv []string) (Result, error) {
	parser.root = parser.current
	remaining := argv
	for len(remaining) > 0 {
		token := remaining[0]
		remaining = remaining[1:]
		if parser.positional {
			parser.result.Arguments = append(parser.result.Arguments, token)
			continue
		}
		if token == "--" {
			parser.positional = true
			continue
		}
		if token == "--help" || token == "-h" {
			parser.result.Action = ActionHelp
			parser.result.CommandID = parser.current.ID

			return parser.result, nil
		}
		if parser.root.Version != "" && parser.current == parser.root &&
			(token == "--version" ||
				(token == "version" && len(parser.root.Children) > 0)) {
			parser.result.Action = ActionVersion
			parser.result.CommandID = parser.root.ID

			return parser.result, nil
		}
		if strings.HasPrefix(token, "--") {
			consumed, err := parser.longOption(token, remaining)
			if err != nil {
				return Result{}, err
			}
			remaining = remaining[consumed:]
			continue
		}
		if len(token) > 1 && token[0] == '-' &&
			(!looksNegativeValue(token) ||
				parser.optionByShort(rune(token[1])) != nil) {
			consumed, err := parser.shortOptions(token[1:], remaining)
			if err != nil {
				return Result{}, err
			}
			remaining = remaining[consumed:]
			continue
		}
		if child := commandChild(parser.current, token); child != nil {
			if err := parser.inheritPersistent(); err != nil {
				return Result{}, err
			}
			parser.current = child
			parser.result.CommandID = child.ID
			continue
		}
		parser.result.Arguments = append(parser.result.Arguments, token)
		if len(parser.current.Children) > 0 {
			return parser.result, nil
		}
	}

	return parser.result, nil
}

func (parser *invocationParser) longOption(
	token string,
	remaining []string,
) (int, error) {
	nameValue := strings.TrimPrefix(token, "--")
	name, raw, assigned := strings.Cut(nameValue, "=")
	option := parser.optionByLong(name)
	if option == nil {
		return 0, &ParseError{Kind: FailureUnknownOption}
	}
	if option.Boolean {
		if !assigned {
			raw = "true"
		}

		parser.addOption(option.Key, raw)

		return 0, nil
	}
	if !assigned {
		if len(remaining) == 0 {
			return 0, &ParseError{Kind: FailureMissingValue}
		}
		raw = remaining[0]
	}
	parser.addOption(option.Key, raw)

	if assigned {
		return 0, nil
	}

	return 1, nil
}

func (parser *invocationParser) shortOptions(
	cluster string,
	remaining []string,
) (int, error) {
	runes := []rune(cluster)
	for index, short := range runes {
		option := parser.optionByShort(short)
		if option == nil {
			return 0, &ParseError{Kind: FailureUnknownOption}
		}
		if option.Boolean {
			parser.addOption(option.Key, "true")
			continue
		}
		if index+1 < len(runes) {
			parser.addOption(option.Key, string(runes[index+1:]))

			return 0, nil
		}
		if len(remaining) == 0 {
			return 0, &ParseError{Kind: FailureMissingValue}
		}
		parser.addOption(option.Key, remaining[0])

		return 1, nil
	}

	return 0, nil
}

func (parser *invocationParser) optionByLong(name string) *Option {
	for index := range parser.current.Options {
		if parser.current.Options[index].Name == name {
			return &parser.current.Options[index]
		}
	}
	for index := range parser.inherited {
		if parser.inherited[index].Name == name {
			return &parser.inherited[index]
		}
	}

	return nil
}

func (parser *invocationParser) optionByShort(short rune) *Option {
	for index := range parser.current.Options {
		if parser.current.Options[index].Short == short {
			return &parser.current.Options[index]
		}
	}
	for index := range parser.inherited {
		if parser.inherited[index].Short == short {
			return &parser.inherited[index]
		}
	}

	return nil
}

func (parser *invocationParser) inheritPersistent() error {
	for _, option := range parser.current.Options {
		if option.Persistent {
			parser.inherited = append(parser.inherited, option)
			continue
		}
		if len(parser.result.Options[option.Key]) > 0 {
			return &ParseError{Kind: FailureUnknownOption}
		}
	}

	return nil
}

func (parser *invocationParser) addOption(key int, value string) {
	parser.result.Options[key] = append(parser.result.Options[key], value)
}

func commandChild(command *Command, token string) *Command {
	for index := range command.Children {
		child := &command.Children[index]
		if child.Name == token {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == token {
				return child
			}
		}
	}

	return nil
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
