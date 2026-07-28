package cli

import (
	"context"
	"strings"
)

// CommandSet declares a bounded root command with direct executable children.
// It is intended for one-binary service processes that do not expose options,
// positional arguments, aliases, nested commands, lifecycle hooks, or shell
// completion.
type CommandSet struct {
	// Name is the stable root command token.
	Name string
	// Version is the optional version rendered by --version and version.
	Version string
	// Commands are the direct executable children in help order.
	Commands []CommandSpec
}

// CommandSpec declares one direct command in a CommandSet.
type CommandSpec struct {
	// Name is the stable command token.
	Name string
	// Summary is the one-line help description.
	Summary string
	// Handler executes the selected command.
	Handler Handler
}

// CommandSetApplication is an immutable command set safe for concurrent
// invocation.
type CommandSetApplication struct {
	root      *compiledCommand
	limits    Limits
	exitCodes ExitCodePolicy
}

// CompileCommandSet validates and snapshots a bounded command set.
func CompileCommandSet(
	set CommandSet,
	options ...CompileOption,
) (*CommandSetApplication, error) {
	configuration := compileConfiguration{
		limits: defaultLimits(), exitCodes: defaultExitCodePolicy(),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	if err := validateName("command name", set.Name); err != nil {
		return nil, err
	}
	if len(set.Commands)+1 > configuration.limits.MaximumCommands {
		return nil, newInternalError("command tree exceeds maximum command count", nil)
	}
	if len(set.Commands) > 0 && configuration.limits.MaximumCommandDepth < 2 {
		return nil, newInternalError("command tree exceeds maximum depth", nil)
	}
	metadataBytes := len(set.Name) + len(set.Version)
	if metadataBytes > configuration.limits.MaximumMetadataBytes {
		return nil, newInternalError("command metadata exceeds maximum bytes", nil)
	}
	root := &compiledCommand{
		id:       0,
		name:     set.Name,
		version:  set.Version,
		children: make([]*compiledCommand, 0, len(set.Commands)),
	}
	names := make(map[string]struct{}, len(set.Commands))
	for index, spec := range set.Commands {
		if err := validateName("command name", spec.Name); err != nil {
			return nil, err
		}
		if spec.Name == "help" || spec.Name == "version" {
			return nil, newInternalError("reserved command name", nil)
		}
		if spec.Handler == nil {
			return nil, newInternalError("command requires a handler", nil)
		}
		if _, duplicate := names[spec.Name]; duplicate {
			return nil, newInternalError("duplicate command name", nil)
		}
		metadataBytes += len(spec.Name) + len(spec.Summary)
		if metadataBytes > configuration.limits.MaximumMetadataBytes {
			return nil, newInternalError("command metadata exceeds maximum bytes", nil)
		}
		command := &compiledCommand{
			id:          index + 1,
			name:        spec.Name,
			summary:     spec.Summary,
			handler:     spec.Handler,
			interaction: InteractionForbidden,
		}
		root.children = append(root.children, command)
		names[spec.Name] = struct{}{}
	}

	return &CommandSetApplication{
		root:      root,
		limits:    configuration.limits,
		exitCodes: configuration.exitCodes,
	}, nil
}

// RunCommand validates, selects, and executes one command-set invocation.
func (application *CommandSetApplication) RunCommand(
	ctx context.Context,
	request Request,
) Result {
	streams := normalizeIO(request)
	output := &Output{}
	if application == nil || application.root == nil {
		return application.withExitCode(finalize(
			streams,
			request.Output,
			nil,
			output,
			newInternalError("run a nil command-set application", nil),
		))
	}
	if ctx == nil {
		return application.withExitCode(finalize(
			streams,
			request.Output,
			nil,
			output,
			newInternalError("run with a nil context", nil),
		))
	}
	if request.Output.Mode > OutputQuiet {
		return application.withExitCode(finalize(
			streams,
			OutputPolicy{},
			nil,
			output,
			newInternalError("invalid output mode", nil),
		))
	}
	if err := contextError(ctx); err != nil {
		return application.withExitCode(finalize(
			streams,
			request.Output,
			nil,
			output,
			err,
		))
	}
	if err := validateArgv(request.Args, application.limits); err != nil {
		return application.withExitCode(finalize(
			streams,
			request.Output,
			nil,
			output,
			err,
		))
	}

	return application.withExitCode(
		application.runCommand(ctx, request, streams, output),
	)
}

func (application *CommandSetApplication) withExitCode(result Result) Result {
	exitCodes := defaultExitCodePolicy()
	if application != nil {
		exitCodes = application.exitCodes
	}
	if result.Err != nil {
		result.ExitCode = exitCodes.code(result.Err)
	}

	return result
}

func (application *CommandSetApplication) runCommand(
	ctx context.Context,
	request Request,
	streams IO,
	output *Output,
) Result {
	selected := application.root
	if len(request.Args) == 0 {
		return executeCommandSetHandler(ctx, request, streams, output, selected)
	}

	first := request.Args[0]
	switch {
	case first == "--help" || first == "-h":
		return application.help(request, streams, output, selected)
	case application.root.version != "" &&
		(first == "--version" || first == "version"):
		if err := output.SetData(
			application.root.name + " " + application.root.version,
		); err != nil {
			return finalize(streams, request.Output, selected, output, err)
		}

		return finalizeSignal(
			streams,
			request.Output,
			selected,
			output,
			ErrorKindVersion,
		)
	case strings.HasPrefix(first, "-"):
		return commandSetUnknownOption(request, streams, output, selected)
	}

	selected = commandSetChild(application.root, first)
	if selected == nil {
		message := "unknown command " + safeToken(first)
		if suggestion := suggestCommand(application.root, first); suggestion != "" {
			message += "; did you mean " + safeToken(suggestion) + "?"
		}

		return finalize(streams, request.Output, application.root, output,
			newClassifiedError(
				ErrorKindUnknownCommand,
				message,
				nil,
				false,
			),
		)
	}
	for _, token := range request.Args[1:] {
		switch {
		case token == "--help" || token == "-h":
			return application.help(request, streams, output, selected)
		case strings.HasPrefix(token, "-"):
			return commandSetUnknownOption(request, streams, output, selected)
		default:
			return finalize(streams, request.Output, selected, output,
				newClassifiedError(
					ErrorKindUsage,
					"unexpected positional argument "+safeToken(token),
					nil,
					false,
				),
			)
		}
	}

	return executeCommandSetHandler(ctx, request, streams, output, selected)
}

func commandSetChild(root *compiledCommand, name string) *compiledCommand {
	for _, child := range root.children {
		if child.name == name {
			return child
		}
	}

	return nil
}

func (application *CommandSetApplication) help(
	request Request,
	streams IO,
	output *Output,
	command *compiledCommand,
) Result {
	help := commandSetHelpText(
		application.root,
		command,
		request.Output.Width,
	)
	if err := output.SetData(strings.TrimSuffix(help, "\n")); err != nil {
		return finalize(streams, request.Output, command, output, err)
	}

	return finalizeSignal(
		streams,
		request.Output,
		command,
		output,
		ErrorKindHelp,
	)
}

func commandSetHelpText(
	root *compiledCommand,
	command *compiledCommand,
	width int,
) string {
	var output strings.Builder
	if command.summary != "" {
		output.WriteString(sanitizeTerminal(command.summary))
		output.WriteString("\n\n")
	}
	output.WriteString("Usage:\n  ")
	output.WriteString(root.name)
	if command != root {
		output.WriteByte(' ')
		output.WriteString(command.name)
	} else if len(root.children) > 0 {
		output.WriteString(" <command>")
	}
	output.WriteByte('\n')
	if command == root && len(root.children) > 0 {
		output.WriteString("\nCommands:\n")
		for _, child := range root.children {
			output.WriteString("  ")
			output.WriteString(child.name)
			padding := max(1, 13-len(child.name))
			output.WriteString(strings.Repeat(" ", padding))
			output.WriteString(sanitizeTerminal(child.summary))
			output.WriteByte('\n')
		}
	}

	return wrapHelp(output.String(), width)
}

func commandSetUnknownOption(
	request Request,
	streams IO,
	output *Output,
	command *compiledCommand,
) Result {
	return finalize(streams, request.Output, command, output,
		newClassifiedError(
			ErrorKindUnknownOption,
			"invalid command invocation: unknown option",
			nil,
			false,
		),
	)
}

func executeCommandSetHandler(
	ctx context.Context,
	request Request,
	streams IO,
	output *Output,
	command *compiledCommand,
) Result {
	if err := contextError(ctx); err != nil {
		return finalize(streams, request.Output, command, output, err)
	}
	invocation := Invocation{
		input:       Input{values: map[any]resolvedValue{}},
		io:          invocationIO(streams, request.Output),
		interactive: false,
		output:      output,
	}
	if command.handler != nil {
		if err := command.handler(ctx, invocation); err != nil {
			return finalize(
				streams,
				request.Output,
				command,
				output,
				classifyPhaseError(ctx, "command execution failed", err),
			)
		}
	}
	if err := contextError(ctx); err != nil {
		return finalize(streams, request.Output, command, output, err)
	}

	return finalize(streams, request.Output, command, output, nil)
}
