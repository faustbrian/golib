package cli

import (
	"context"
	"errors"
	"strings"
)

// CompletionRequest contains only safe metadata and hostile partial input.
type CompletionRequest struct {
	Command CommandMetadata
	Partial string
}

// CompletionCandidate is one bounded shell completion value.
type CompletionCandidate struct {
	Value       string
	Description string
}

// CompletionProvider supplies deliberate application-owned dynamic values.
type CompletionProvider func(context.Context, CompletionRequest) ([]CompletionCandidate, error)

// Complete returns bounded candidates for already shell-tokenized partial argv.
func (application *Application) Complete(
	ctx context.Context,
	argv []string,
) ([]CompletionCandidate, error) {
	if application == nil || application.root == nil {
		return nil, newInternalError("complete a nil application", nil)
	}
	if ctx == nil {
		return nil, newInternalError("complete with a nil context", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateArgv(argv, application.limits); err != nil {
		return nil, err
	}
	partial := ""
	tokens := argv
	if len(argv) > 0 {
		partial = argv[len(argv)-1]
		tokens = argv[:len(argv)-1]
	}
	command, positional, pending := completionPosition(application.root, tokens)
	request := CompletionRequest{
		Command: CommandMetadata{command: command}, Partial: partial,
	}
	if option, value, assigned := completionAssignedOption(command, partial); assigned {
		request.Partial = value
		if option != nil && option.completion != nil {
			return application.dynamicCandidates(ctx, option.completion, request)
		}
		if option != nil && !option.secret {
			return application.boundCandidates(enumCandidates(option.allowed, value)), nil
		}

		return nil, nil
	}
	if option, value, attached := completionAttachedShortOption(command, partial); attached {
		request.Partial = value
		if option.completion != nil {
			return application.dynamicCandidates(ctx, option.completion, request)
		}
		if !option.secret {
			return application.boundCandidates(enumCandidates(option.allowed, value)), nil
		}
		return nil, nil
	}
	if pending != nil && pending.completion != nil {
		return application.dynamicCandidates(ctx, pending.completion, request)
	}
	if pending != nil && !pending.secret {
		return application.boundCandidates(enumCandidates(pending.allowed, partial)), nil
	}
	if strings.HasPrefix(partial, "-") {
		candidates := make([]CompletionCandidate, 0, len(command.effective))
		for _, option := range command.effective {
			value := "--" + option.name
			if strings.HasPrefix(value, partial) {
				candidates = append(candidates, CompletionCandidate{
					Value: value, Description: option.description,
				})
			}
		}
		return application.boundCandidates(candidates), nil
	}

	candidates := make([]CompletionCandidate, 0, len(command.children))
	for _, child := range command.children {
		if child.hidden {
			continue
		}
		if strings.HasPrefix(child.name, partial) {
			candidates = append(candidates, CompletionCandidate{
				Value: child.name, Description: child.summary,
			})
		}
		for _, alias := range child.aliases {
			if strings.HasPrefix(alias, partial) {
				candidates = append(candidates, CompletionCandidate{
					Value: alias, Description: "alias for " + child.name,
				})
			}
		}
	}
	if argument := completionArgument(command, positional); argument != nil && argument.completion != nil {
		dynamic, err := application.dynamicCandidates(ctx, argument.completion, request)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, dynamic...)
	} else if argument != nil && !argument.secret {
		candidates = append(candidates, enumCandidates(argument.allowed, partial)...)
	}

	return application.boundCandidates(candidates), nil
}

func completionAttachedShortOption(
	command *compiledCommand,
	token string,
) (*optionSpec, string, bool) {
	if !strings.HasPrefix(token, "-") || strings.HasPrefix(token, "--") || token == "-" {
		return nil, "", false
	}
	payload := strings.TrimPrefix(token, "-")
	for position := 0; position < len(payload); position++ {
		option := findCompletionShortOption(command, rune(payload[position]))
		if option == nil {
			return nil, "", false
		}
		if !option.boolean {
			return option, payload[position+1:], true
		}
	}

	return nil, "", false
}

func enumCandidates(allowed []string, partial string) []CompletionCandidate {
	candidates := make([]CompletionCandidate, 0, len(allowed))
	for _, value := range allowed {
		if strings.HasPrefix(value, partial) {
			candidates = append(candidates, CompletionCandidate{Value: value})
		}
	}
	return candidates
}

func completionPosition(
	root *compiledCommand,
	tokens []string,
) (*compiledCommand, int, *optionSpec) {
	command := root
	positional := 0
	var pending *optionSpec
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if pending != nil {
			pending = nil
			continue
		}
		if token == "--" {
			positional += len(tokens) - index - 1
			break
		}
		if strings.HasPrefix(token, "--") {
			name, _, assigned := strings.Cut(strings.TrimPrefix(token, "--"), "=")
			if option := findCompletionOption(command, name); option != nil && !option.boolean && !assigned {
				pending = option
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			payload := strings.TrimPrefix(token, "-")
			recognized := false
			for position := 0; position < len(payload); position++ {
				option := findCompletionShortOption(command, rune(payload[position]))
				if option == nil {
					break
				}
				recognized = true
				if !option.boolean {
					if position == len(payload)-1 {
						pending = option
					}
					break
				}
			}
			if recognized {
				continue
			}
		}
		if child := findCompletionChild(command, token); child != nil {
			command = child
			positional = 0
			continue
		}
		positional++
	}

	return command, positional, pending
}

func completionAssignedOption(
	command *compiledCommand,
	token string,
) (*optionSpec, string, bool) {
	if !strings.HasPrefix(token, "--") {
		return nil, "", false
	}
	name, value, assigned := strings.Cut(strings.TrimPrefix(token, "--"), "=")
	if !assigned {
		return nil, "", false
	}

	return findCompletionOption(command, name), value, true
}

func findCompletionOption(command *compiledCommand, name string) *optionSpec {
	for index := range command.effective {
		if command.effective[index].name == name {
			return &command.effective[index]
		}
	}
	return nil
}

func findCompletionShortOption(command *compiledCommand, short rune) *optionSpec {
	for index := range command.effective {
		if command.effective[index].short == short {
			return &command.effective[index]
		}
	}

	return nil
}

func findCompletionChild(command *compiledCommand, token string) *compiledCommand {
	for _, child := range command.children {
		if child.name == token || contains(child.aliases, token) {
			return child
		}
	}
	return nil
}

func completionArgument(command *compiledCommand, position int) *argumentSpec {
	if position < len(command.arguments) {
		return &command.arguments[position]
	}
	if len(command.arguments) > 0 {
		last := &command.arguments[len(command.arguments)-1]
		if last.cardinality == ArgumentRepeated || last.cardinality == ArgumentRemainder {
			return last
		}
	}
	return nil
}

func (application *Application) dynamicCandidates(
	ctx context.Context,
	provider CompletionProvider,
	request CompletionRequest,
) ([]CompletionCandidate, error) {
	if provider == nil {
		return nil, newInternalError("invoke a nil completion provider", nil)
	}
	candidates, err := provider(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, newClassifiedError(
			ErrorKindCompletion,
			"dynamic completion failed",
			err,
			false,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return application.boundCandidates(candidates), nil
}

func (application *Application) boundCandidates(
	candidates []CompletionCandidate,
) []CompletionCandidate {
	limit := application.limits.MaximumCompletionResults
	result := make([]CompletionCandidate, 0, min(len(candidates), limit))
	seen := make(map[string]struct{}, len(candidates))
	bytes := 0
	for _, candidate := range candidates {
		candidate.Value = sanitizeTerminal(candidate.Value)
		candidate.Description = sanitizeTerminal(candidate.Description)
		if candidate.Value == "" {
			continue
		}
		if _, duplicate := seen[candidate.Value]; duplicate {
			continue
		}
		size := len(candidate.Value) + len(candidate.Description)
		if len(result) >= limit {
			break
		}
		if bytes+size > application.limits.MaximumCompletionBytes {
			continue
		}
		seen[candidate.Value] = struct{}{}
		bytes += size
		result = append(result, candidate)
	}

	return result
}
