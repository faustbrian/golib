package cli

import "context"

// Command is a mutable construction node. Compile publishes an immutable copy
// and later changes to a Command do not affect an Application.
type Command struct {
	name          string
	version       string
	aliases       []string
	summary       string
	description   string
	examples      []string
	documentation string
	hidden        bool
	experimental  bool
	deprecated    string
	replacement   string
	children      []*Command
	arguments     []ArgumentDefinition
	options       []OptionDefinition
	handler       Handler
	validations   []Validation
	middlewares   []Middleware
	preRun        []Handler
	postRun       []Handler
	cleanup       []Handler
	interaction   Interaction
	groups        []optionGroupDefinition
}

// CommandOption configures a command construction node.
type CommandOption func(*Command)

// NewCommand creates an explicit command construction node.
func NewCommand(name string, options ...CommandOption) *Command {
	command := &Command{name: name}
	for _, option := range options {
		if option != nil {
			option(command)
		}
	}

	return command
}

// AddSubcommands appends subcommands in registration order.
func (command *Command) AddSubcommands(children ...*Command) error {
	if command == nil {
		return newInternalError("add subcommands to a nil command", nil)
	}
	for _, child := range children {
		if child == nil {
			return newInternalError("add a nil subcommand", nil)
		}
	}
	command.children = append(command.children, children...)

	return nil
}

// WithAliases declares alternate command tokens.
func WithAliases(aliases ...string) CommandOption {
	return func(command *Command) {
		command.aliases = append(command.aliases, aliases...)
	}
}

// WithSummary declares a one-line command summary.
func WithSummary(summary string) CommandOption {
	return func(command *Command) { command.summary = summary }
}

// WithVersion declares command version metadata, normally on the root.
func WithVersion(version string) CommandOption {
	return func(command *Command) { command.version = version }
}

// WithDescription declares the long command description.
func WithDescription(description string) CommandOption {
	return func(command *Command) { command.description = description }
}

// WithExamples declares complete command examples in display order.
func WithExamples(examples ...string) CommandOption {
	return func(command *Command) {
		command.examples = append(command.examples, examples...)
	}
}

// WithDocumentation links the command to additional documentation.
func WithDocumentation(documentation string) CommandOption {
	return func(command *Command) { command.documentation = documentation }
}

// WithHidden controls whether generated discovery surfaces omit the command.
func WithHidden(hidden bool) CommandOption {
	return func(command *Command) { command.hidden = hidden }
}

// WithExperimental marks a command whose compatibility is not yet stable.
func WithExperimental(experimental bool) CommandOption {
	return func(command *Command) { command.experimental = experimental }
}

// WithDeprecated records a deprecation message.
func WithDeprecated(message string) CommandOption {
	return func(command *Command) { command.deprecated = message }
}

// WithReplacement records the preferred replacement command path.
func WithReplacement(path string) CommandOption {
	return func(command *Command) { command.replacement = path }
}

// WithSubcommands registers children in deterministic display order.
func WithSubcommands(children ...*Command) CommandOption {
	return func(command *Command) {
		command.children = append(command.children, children...)
	}
}

// WithArguments registers positional arguments in parse order.
func WithArguments(arguments ...ArgumentDefinition) CommandOption {
	return func(command *Command) {
		command.arguments = append(command.arguments, arguments...)
	}
}

// WithOptions registers command options in display order.
func WithOptions(options ...OptionDefinition) CommandOption {
	return func(command *Command) {
		command.options = append(command.options, options...)
	}
}

// Handler receives parsed input, caller context, and request-owned IO.
type Handler func(context.Context, Invocation) error

// Validation checks parsed input before lifecycle middleware or side effects.
type Validation func(context.Context, Input) error

// Next continues an explicit middleware chain with the supplied context.
type Next func(context.Context) error

// Middleware observes safe command metadata and controls lifecycle execution.
type Middleware func(context.Context, CommandMetadata, Next) error

// Interaction declares whether a command may require an interactive terminal.
type Interaction uint8

const (
	// InteractionOptional allows an application to add optional prompts.
	InteractionOptional Interaction = iota
	// InteractionRequired rejects explicit non-interactive execution.
	InteractionRequired
	// InteractionForbidden declares that the command must never prompt.
	InteractionForbidden
)

// WithHandler declares the command execution handler.
func WithHandler(handler Handler) CommandOption {
	return func(command *Command) { command.handler = handler }
}

// WithValidation appends input validation in execution order.
func WithValidation(validations ...Validation) CommandOption {
	return func(command *Command) {
		command.validations = append(command.validations, validations...)
	}
}

// WithMiddleware appends lifecycle middleware in outer-to-inner order.
func WithMiddleware(middlewares ...Middleware) CommandOption {
	return func(command *Command) {
		command.middlewares = append(command.middlewares, middlewares...)
	}
}

// WithPreRun appends behavior that runs before the command handler.
func WithPreRun(hooks ...Handler) CommandOption {
	return func(command *Command) { command.preRun = append(command.preRun, hooks...) }
}

// WithPostRun appends behavior that runs after a successful command handler.
func WithPostRun(hooks ...Handler) CommandOption {
	return func(command *Command) { command.postRun = append(command.postRun, hooks...) }
}

// WithCleanup appends cleanup behavior. Cleanup runs in reverse order.
func WithCleanup(hooks ...Handler) CommandOption {
	return func(command *Command) { command.cleanup = append(command.cleanup, hooks...) }
}

// WithInteraction declares the command's interactive capability.
func WithInteraction(interaction Interaction) CommandOption {
	return func(command *Command) { command.interaction = interaction }
}

type optionGroupKind uint8

const (
	optionGroupExclusive optionGroupKind = iota + 1
	optionGroupTogether
)

type optionGroupDefinition struct {
	kind    optionGroupKind
	options []OptionDefinition
}

// WithMutuallyExclusive requires at most one grouped option to resolve.
func WithMutuallyExclusive(options ...OptionDefinition) CommandOption {
	return func(command *Command) {
		command.groups = append(command.groups, optionGroupDefinition{
			kind: optionGroupExclusive, options: append([]OptionDefinition(nil), options...),
		})
	}
}

// WithRequiredTogether requires all grouped options when any one resolves.
func WithRequiredTogether(options ...OptionDefinition) CommandOption {
	return func(command *Command) {
		command.groups = append(command.groups, optionGroupDefinition{
			kind: optionGroupTogether, options: append([]OptionDefinition(nil), options...),
		})
	}
}
