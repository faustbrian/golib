package cli

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/faustbrian/golib/pkg/cli/internal/engine"
)

// HelpOptions controls plain-text help rendering.
type HelpOptions struct {
	Width int
}

// Shell identifies a supported completion script target.
type Shell string

const (
	// ShellBash generates Bash completion.
	ShellBash Shell = "bash"
	// ShellZsh generates Zsh completion.
	ShellZsh Shell = "zsh"
	// ShellFish generates Fish completion.
	ShellFish Shell = "fish"
	// ShellPowerShell generates PowerShell completion.
	ShellPowerShell Shell = "powershell"
)

// Manifest is the stable machine-readable command model.
type Manifest struct {
	Schema   string            `json:"schema"`
	Name     string            `json:"name"`
	Version  string            `json:"version,omitempty"`
	Root     ManifestCommand   `json:"root"`
	Commands []ManifestCommand `json:"commands,omitempty"`
}

// ManifestCommand describes one command and its descendants.
type ManifestCommand struct {
	Name             string             `json:"name"`
	Path             string             `json:"path"`
	Aliases          []string           `json:"aliases,omitempty"`
	Summary          string             `json:"summary,omitempty"`
	Description      string             `json:"description,omitempty"`
	Examples         []string           `json:"examples,omitempty"`
	Documentation    string             `json:"documentation,omitempty"`
	Hidden           bool               `json:"hidden,omitempty"`
	Experimental     bool               `json:"experimental,omitempty"`
	Deprecated       string             `json:"deprecated,omitempty"`
	Replacement      string             `json:"replacement,omitempty"`
	Arguments        []ManifestArgument `json:"arguments,omitempty"`
	Options          []ManifestOption   `json:"options,omitempty"`
	InheritedOptions []ManifestOption   `json:"inherited_options,omitempty"`
	Commands         []ManifestCommand  `json:"commands,omitempty"`
}

// ManifestArgument describes a positional argument.
type ManifestArgument struct {
	Name          string              `json:"name"`
	Type          string              `json:"type"`
	Cardinality   ArgumentCardinality `json:"cardinality"`
	Description   string              `json:"description,omitempty"`
	Secret        bool                `json:"secret,omitempty"`
	AllowedValues []string            `json:"allowed_values,omitempty"`
	Format        string              `json:"format,omitempty"`
}

// ManifestOption describes a local or inherited option.
type ManifestOption struct {
	Name          string   `json:"name"`
	Short         string   `json:"short,omitempty"`
	Type          string   `json:"type"`
	Description   string   `json:"description,omitempty"`
	Persistent    bool     `json:"persistent,omitempty"`
	Secret        bool     `json:"secret,omitempty"`
	Defaulted     bool     `json:"defaulted,omitempty"`
	Required      bool     `json:"required,omitempty"`
	Source        string   `json:"source,omitempty"`
	AllowedValues []string `json:"allowed_values,omitempty"`
	Format        string   `json:"format,omitempty"`
}

// Help renders deterministic plain-text help for a canonical or alias path.
func (application *Application) Help(path []string, options HelpOptions) (string, error) {
	command, canonical, err := application.findCommand(path)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	description := command.description
	if description == "" {
		description = command.summary
	}
	if description != "" {
		output.WriteString(sanitizeTerminal(description))
		output.WriteString("\n\n")
	}
	if command.experimental || command.deprecated != "" || command.replacement != "" {
		output.WriteString("Status:\n")
		if command.experimental {
			output.WriteString("  Experimental: yes\n")
		}
		if command.deprecated != "" {
			output.WriteString("  Deprecated: " + sanitizeTerminal(command.deprecated) + "\n")
		}
		if command.replacement != "" {
			output.WriteString("  Replacement: " + sanitizeTerminal(command.replacement) + "\n")
		}
		output.WriteByte('\n')
	}
	output.WriteString("Usage:\n  ")
	output.WriteString(canonical)
	if len(command.children) > 0 {
		output.WriteString(" <command>")
	}
	if len(command.effective) > 0 {
		output.WriteString(" [options]")
	}
	for _, argument := range command.arguments {
		output.WriteByte(' ')
		output.WriteString(argumentUsage(argument))
	}
	output.WriteByte('\n')
	if len(command.children) > 0 {
		output.WriteString("\nCommands:\n")
		for _, child := range command.children {
			if child.hidden {
				continue
			}
			fmt.Fprintf(&output, "  %-12s %s\n", child.name, sanitizeTerminal(child.summary))
		}
	}
	if len(command.arguments) > 0 {
		output.WriteString("\nArguments:\n")
		for _, argument := range command.arguments {
			fmt.Fprintf(&output, "  %-11s  %s\n", argument.name, sanitizeTerminal(argument.description))
		}
	}
	if len(command.effective) > 0 {
		output.WriteString("\nOptions:\n")
		for _, option := range appendLocalThenInherited(command) {
			label := optionLabel(option)
			description := sanitizeTerminal(option.description)
			if option.origin != canonical {
				description += " (inherited from " + option.origin + ")"
			}
			if len(label) >= 15 {
				output.WriteString(label + "\n               " + description + "\n")
			} else {
				fmt.Fprintf(&output, "%-15s%s\n", label, description)
			}
		}
	}
	if len(command.aliases) > 0 {
		output.WriteString("\nAliases:\n  ")
		output.WriteString(strings.Join(command.aliases, ", "))
		output.WriteByte('\n')
	}
	if len(command.examples) > 0 {
		output.WriteString("\nExamples:\n")
		for _, example := range command.examples {
			output.WriteString("  " + sanitizeTerminal(example) + "\n")
		}
	}
	if command.documentation != "" {
		output.WriteString("\nDocumentation:\n  " + sanitizeTerminal(command.documentation) + "\n")
	}

	return wrapHelp(output.String(), options.Width), nil
}

func wrapHelp(value string, width int) string {
	if width < 1 {
		return value
	}
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapHelpLine(line, width)...)
	}
	return strings.Join(wrapped, "\n") + "\n"
}

func wrapHelpLine(line string, width int) []string {
	if len([]rune(line)) <= width {
		return []string{line}
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if separator := strings.Index(line, "  "); separator > 0 && strings.HasPrefix(line, "-") {
		indent = separator + 2
	}
	continuation := strings.Repeat(" ", min(indent, width-1))
	remaining := line
	var result []string
	for len([]rune(remaining)) > width {
		runes := []rune(remaining)
		breakAt := width
		leading := len(runes) - len([]rune(strings.TrimLeft(remaining, " ")))
		for index := width; index > leading; index-- {
			if runes[index-1] == ' ' {
				breakAt = index - 1
				break
			}
		}
		result = append(result, strings.TrimRight(string(runes[:breakAt]), " "))
		remaining = continuation + strings.TrimLeft(string(runes[breakAt:]), " ")
	}
	result = append(result, remaining)
	return result
}

// ManifestJSON returns an indented deterministic machine manifest.
func (application *Application) ManifestJSON() ([]byte, error) {
	if application == nil || application.root == nil {
		return nil, newInternalError("generate manifest from a nil application", nil)
	}
	manifest := application.manifest()
	encoded, _ := json.MarshalIndent(manifest, "", "  ")

	return append(encoded, '\n'), nil
}

// Markdown returns a deterministic command reference.
func (application *Application) Markdown() (string, error) {
	if application == nil || application.root == nil {
		return "", newInternalError("generate Markdown from a nil application", nil)
	}
	var output strings.Builder
	fmt.Fprintf(&output, "# `%s` command reference\n\n", application.root.name)
	writeMarkdownCommand(&output, application.root, application.root.name, 2)

	return strings.TrimRight(output.String(), "\n") + "\n", nil
}

// Completion returns a deterministic completion script without side effects.
func (application *Application) Completion(shell Shell) (string, error) {
	if application == nil || application.root == nil {
		return "", newInternalError("generate completion from a nil application", nil)
	}
	completion, err := engine.Completion(engineCommand(application.root), string(shell))
	if err != nil {
		return "", newClassifiedError(ErrorKindUsage, "generate shell completion", err, true)
	}

	return completion, nil
}

func (application *Application) manifest() Manifest {
	root := application.root
	manifestRoot := manifestCommand(root, root.name)
	manifestRoot.Commands = nil
	manifest := Manifest{
		Schema: "go-cli/manifest/v1", Name: root.name, Version: root.version,
		Root: manifestRoot,
	}
	for _, child := range root.children {
		manifest.Commands = append(manifest.Commands, manifestCommand(child, root.name+" "+child.name))
	}

	return manifest
}

func manifestCommand(command *compiledCommand, path string) ManifestCommand {
	result := ManifestCommand{
		Name: command.name, Path: path, Aliases: cloneStrings(command.aliases),
		Summary: command.summary, Description: command.description,
		Examples: cloneStrings(command.examples), Documentation: command.documentation,
		Hidden: command.hidden, Experimental: command.experimental,
		Deprecated: command.deprecated, Replacement: command.replacement,
	}
	for _, argument := range command.arguments {
		result.Arguments = append(result.Arguments, ManifestArgument{
			Name: argument.name, Type: argument.valueType, Cardinality: argument.cardinality,
			Description: argument.description, Secret: argument.secret,
			AllowedValues: publicAllowedValues(argument.allowed, argument.secret),
			Format:        argument.format,
		})
	}
	for _, option := range command.options {
		result.Options = append(result.Options, manifestOption(option))
	}
	for _, option := range command.effective {
		if option.origin != path {
			result.InheritedOptions = append(result.InheritedOptions, manifestOption(option))
		}
	}
	for _, child := range command.children {
		result.Commands = append(result.Commands, manifestCommand(child, path+" "+child.name))
	}

	return result
}

func manifestOption(option optionSpec) ManifestOption {
	short := ""
	if option.short != 0 {
		short = string(option.short)
	}
	return ManifestOption{
		Name: option.name, Short: short, Type: option.valueType,
		Description: option.description, Persistent: option.persistent,
		Secret: option.secret, Defaulted: option.hasDefault, Source: option.origin,
		Required:      option.required,
		AllowedValues: publicAllowedValues(option.allowed, option.secret),
		Format:        option.format,
	}
}

func (application *Application) findCommand(path []string) (*compiledCommand, string, error) {
	if application == nil || application.root == nil {
		return nil, "", newInternalError("read help from a nil application", nil)
	}
	command := application.root
	canonical := command.name
	for _, token := range path {
		var found *compiledCommand
		for _, child := range command.children {
			if child.name == token || contains(child.aliases, token) {
				found = child
				break
			}
		}
		if found == nil {
			return nil, "", newClassifiedError(ErrorKindUsage, "unknown command "+safeToken(token), nil, false)
		}
		command = found
		canonical += " " + command.name
	}

	return command, canonical, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func argumentUsage(argument argumentSpec) string {
	switch argument.cardinality {
	case ArgumentRequired:
		return "<" + argument.name + ">"
	case ArgumentOptional:
		return "[" + argument.name + "]"
	case ArgumentRepeated, ArgumentRemainder:
		return "[" + argument.name + "...]"
	default:
		return "<" + argument.name + ">"
	}
}

func appendLocalThenInherited(command *compiledCommand) []optionSpec {
	options := append([]optionSpec(nil), command.options...)
	for _, option := range command.effective {
		if option.origin != commandPath(command) {
			options = append(options, option)
		}
	}
	return options
}

func commandPath(command *compiledCommand) string {
	if len(command.options) > 0 {
		return command.options[0].origin
	}
	for _, option := range command.effective {
		if strings.HasSuffix(option.origin, " "+command.name) || option.origin == command.name {
			return option.origin
		}
	}
	return command.name
}

func optionLabel(option optionSpec) string {
	if option.short != 0 {
		return "  -" + string(option.short) + ", --" + option.name
	}
	return "      --" + option.name
}

func writeMarkdownCommand(output *strings.Builder, command *compiledCommand, path string, level int) {
	fmt.Fprintf(output, "%s %s\n\n", strings.Repeat("#", level), markdownCode(path))
	if command.summary != "" {
		output.WriteString(markdownText(command.summary) + "\n\n")
	}
	if command.description != "" && command.description != command.summary {
		output.WriteString(markdownText(command.description) + "\n\n")
	}
	if command.experimental || command.hidden || command.deprecated != "" || command.replacement != "" {
		output.WriteString("### Status\n\n")
		if command.experimental {
			output.WriteString("- Experimental\n")
		}
		if command.hidden {
			output.WriteString("- Hidden\n")
		}
		if command.deprecated != "" {
			output.WriteString("- Deprecated: " + markdownText(command.deprecated) + "\n")
		}
		if command.replacement != "" {
			output.WriteString("- Replacement: " + markdownCode(command.replacement) + "\n")
		}
		output.WriteByte('\n')
	}
	if command.documentation != "" {
		fmt.Fprintf(output, "Documentation: %s\n\n", markdownText(command.documentation))
	}
	if len(command.arguments) > 0 {
		output.WriteString("### Arguments\n\n")
		for _, argument := range command.arguments {
			attributes := []string{markdownCode(argument.valueType), argumentCardinalityName(argument.cardinality)}
			if argument.hasFormat {
				attributes = append(attributes, "format "+markdownCode(argument.format))
			}
			if argument.secret {
				attributes = append(attributes, "secret")
			}
			fmt.Fprintf(output, "- %s (%s)", markdownCode(argument.name), strings.Join(attributes, ", "))
			if argument.description != "" {
				output.WriteString(": " + markdownText(argument.description))
			}
			writeMarkdownAllowedValues(output, publicAllowedValues(argument.allowed, argument.secret))
			output.WriteByte('\n')
		}
		output.WriteByte('\n')
	}
	if len(command.options) > 0 || len(command.effective) > len(command.options) {
		output.WriteString("### Options\n\n")
		for _, option := range appendLocalThenInherited(command) {
			label := markdownCode("--" + option.name)
			if option.short != 0 {
				label = markdownCode("-"+string(option.short)) + ", " + label
			}
			attributes := []string{markdownCode(option.valueType)}
			if option.hasFormat {
				attributes = append(attributes, "format "+markdownCode(option.format))
			}
			if option.required {
				attributes = append(attributes, "required")
			}
			if option.hasDefault {
				attributes = append(attributes, "defaulted")
			}
			if option.persistent {
				attributes = append(attributes, "persistent")
			}
			if option.secret {
				attributes = append(attributes, "secret")
			}
			fmt.Fprintf(output, "- %s (%s)", label, strings.Join(attributes, ", "))
			if option.description != "" {
				output.WriteString(": " + markdownText(option.description))
			}
			writeMarkdownAllowedValues(output, publicAllowedValues(option.allowed, option.secret))
			if option.origin != path {
				fmt.Fprintf(output, " Inherited from %s.", markdownCode(option.origin))
			}
			output.WriteString("\n")
		}
		output.WriteString("\n")
	}
	if len(command.aliases) > 0 {
		output.WriteString("### Aliases\n\n")
		for _, alias := range command.aliases {
			output.WriteString("- " + markdownCode(alias) + "\n")
		}
		output.WriteByte('\n')
	}
	if len(command.examples) > 0 {
		output.WriteString("### Examples\n\n")
		for _, example := range command.examples {
			for _, line := range strings.Split(sanitizeTerminalMultiline(example), "\n") {
				output.WriteString("    " + line + "\n")
			}
		}
		output.WriteByte('\n')
	}
	for _, child := range command.children {
		writeMarkdownCommand(output, child, path+" "+child.name, level+1)
	}
}

func argumentCardinalityName(cardinality ArgumentCardinality) string {
	switch cardinality {
	case ArgumentRequired:
		return "required"
	case ArgumentOptional:
		return "optional"
	case ArgumentRepeated:
		return "repeated"
	case ArgumentRemainder:
		return "remainder"
	default:
		return "unknown"
	}
}

func writeMarkdownAllowedValues(output *strings.Builder, allowed []string) {
	if len(allowed) == 0 {
		return
	}
	values := make([]string, 0, len(allowed))
	for _, value := range allowed {
		values = append(values, markdownCode(value))
	}
	output.WriteString(" Allowed values: " + strings.Join(values, ", ") + ".")
}

func publicAllowedValues(allowed []string, secret bool) []string {
	if secret {
		return nil
	}
	return cloneStrings(allowed)
}

func markdownCode(value string) string {
	value = sanitizeTerminal(value)
	if strings.ContainsRune(value, '`') {
		return "<code>" + html.EscapeString(value) + "</code>"
	}

	return "`" + value + "`"
}

func markdownText(value string) string {
	return html.EscapeString(sanitizeTerminal(value))
}
