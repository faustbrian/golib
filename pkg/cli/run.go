package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/cli/internal/engine"
)

const defaultCleanupTimeout = 30 * time.Second

// IO contains streams owned by one invocation.
type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Request describes one already-tokenized invocation.
type Request struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// NonInteractive prevents commands from requiring terminal input.
	NonInteractive bool
	Output         OutputPolicy
}

// Invocation supplies parsed input and explicit IO to a handler.
type Invocation struct {
	input       Input
	io          IO
	interactive bool
	output      *Output
}

// Input returns invocation-local typed values.
func (invocation Invocation) Input() Input { return invocation.input }

// IO returns the caller-owned streams for this invocation.
func (invocation Invocation) IO() IO { return invocation.io }

// Interactive reports whether application-provided prompting is permitted.
func (invocation Invocation) Interactive() bool { return invocation.interactive }

// Output returns the invocation-local bounded output collector.
func (invocation Invocation) Output() *Output { return invocation.output }

// Result is the terminal outcome of one in-process invocation.
type Result struct {
	ExitCode int
	Err      error
	Command  CommandMetadata
}

// Run parses and executes one invocation without changing process-global state.
func (application *Application) Run(ctx context.Context, request Request) Result {
	result := application.run(ctx, request)
	exitCodes := defaultExitCodePolicy()
	if application != nil {
		exitCodes = application.exitCodes
	}
	if result.Err != nil {
		result.ExitCode = exitCodes.code(result.Err)
	}
	return result
}

func (application *Application) run(ctx context.Context, request Request) Result {
	streams := normalizeIO(request)
	output := &Output{}
	if application == nil || application.root == nil {
		return finalize(streams, request.Output, nil, output, newInternalError("run a nil application", nil))
	}
	if ctx == nil {
		return finalize(streams, request.Output, nil, output, newInternalError("run with a nil context", nil))
	}
	if request.Output.Mode > OutputQuiet {
		return finalize(streams, OutputPolicy{}, nil, output, newInternalError("invalid output mode", nil))
	}
	if err := contextError(ctx); err != nil {
		return finalize(streams, request.Output, nil, output, err)
	}
	if err := validateArgv(request.Args, application.limits); err != nil {
		return finalize(streams, request.Output, nil, output, err)
	}
	if len(request.Args) > 0 &&
		(request.Args[0] == "__complete" || request.Args[0] == "__completeNoDesc") {
		return application.runCompletionBoundary(
			ctx,
			request.Args[1:],
			request.Args[0] == "__completeNoDesc",
			streams,
		)
	}

	parsed, err := engine.Parse(ctx, engineCommand(application.root), request.Args)
	if err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return finalize(streams, request.Output, nil, output, contextErr)
		}
		kind := classifyParseFailure(err)
		return finalize(streams, request.Output, nil, output, newClassifiedError(
			kind,
			"invalid command invocation",
			err,
			true,
		))
	}
	selected := application.commands[parsed.CommandID]
	if selected == nil {
		return finalize(streams, request.Output, nil, output, newInternalError("parser selected an unknown command", nil))
	}
	if parsed.Action == engine.ActionHelp {
		path := application.commandPath(selected.id)
		help, _ := application.Help(path, HelpOptions{Width: request.Output.Width})
		if setErr := output.SetData(strings.TrimSuffix(help, "\n")); setErr != nil {
			return finalize(streams, request.Output, selected, output, setErr)
		}
		return finalizeSignal(streams, request.Output, selected, output, ErrorKindHelp)
	}
	if parsed.Action == engine.ActionVersion {
		if setErr := output.SetData(application.root.name + " " + application.root.version); setErr != nil {
			return finalize(streams, request.Output, selected, output, setErr)
		}
		return finalizeSignal(streams, request.Output, selected, output, ErrorKindVersion)
	}
	input, err := resolveInput(selected, parsed)
	if err != nil {
		return finalize(streams, request.Output, selected, output, err)
	}
	if selected.interaction == InteractionRequired && request.NonInteractive {
		return finalize(streams, request.Output, selected, output, newClassifiedError(
			ErrorKindUsage,
			"command requires interaction in non-interactive mode",
			nil,
			false,
		))
	}
	invocation := Invocation{
		input:       input,
		io:          invocationIO(streams, request.Output),
		interactive: !request.NonInteractive && selected.interaction != InteractionForbidden,
		output:      output,
	}
	for _, validation := range selected.validations {
		if err := validation(ctx, input); err != nil {
			if contextErr := classifyPhaseContextError(ctx, err); contextErr != nil {
				return finalize(streams, request.Output, selected, output, contextErr)
			}
			return finalize(streams, request.Output, selected, output, newClassifiedError(
				ErrorKindValidation,
				"command validation failed",
				err,
				true,
			))
		}
		if err := contextError(ctx); err != nil {
			return finalize(streams, request.Output, selected, output, err)
		}
	}

	lifecycleContext, primary := executeLifecycle(ctx, selected, invocation)
	cleanupErr := executeCleanup(lifecycleContext, selected, invocation)
	terminalErr := joinFailures(primary, cleanupErr)
	if terminalErr != nil {
		return finalize(streams, request.Output, selected, output, terminalErr)
	}

	return finalize(streams, request.Output, selected, output, nil)
}

func invocationIO(streams IO, policy OutputPolicy) IO {
	switch policy.Mode {
	case OutputHuman:
		// Human handlers retain their explicitly configured streams.
	case OutputJSON:
		streams.Stdout = io.Discard
		streams.Stderr = io.Discard
	case OutputQuiet:
		streams.Stdout = io.Discard
	}
	return streams
}

func classifyParseFailure(err error) ErrorKind {
	var parseErr *engine.ParseError
	if !errors.As(err, &parseErr) {
		return ErrorKindUsage
	}
	switch parseErr.Kind {
	case engine.FailureUsage:
		return ErrorKindUsage
	case engine.FailureUnknownCommand:
		return ErrorKindUnknownCommand
	case engine.FailureUnknownOption:
		return ErrorKindUnknownOption
	case engine.FailureMissingValue:
		return ErrorKindMissingValue
	default:
		return ErrorKindUsage
	}
}

func (application *Application) runCompletionBoundary(
	ctx context.Context,
	argv []string,
	withoutDescriptions bool,
	streams IO,
) Result {
	candidates, completionErr := application.Complete(ctx, argv)
	directive := 4
	if completionErr != nil {
		directive = 5
		candidates = nil
	}
	var protocol strings.Builder
	for _, candidate := range candidates {
		protocol.WriteString(candidate.Value)
		if !withoutDescriptions && candidate.Description != "" {
			protocol.WriteByte('\t')
			protocol.WriteString(candidate.Description)
		}
		protocol.WriteByte('\n')
	}
	protocol.WriteByte(':')
	_, _ = fmt.Fprintf(&protocol, "%d", directive)
	protocol.WriteByte('\n')
	terminalErr := completionErr
	if errors.Is(completionErr, context.Canceled) || errors.Is(completionErr, context.DeadlineExceeded) {
		terminalErr = classifyContextError(completionErr)
	}
	if err := writeAll(streams.Stdout, []byte(protocol.String())); err != nil {
		outputErr := newClassifiedError(
			ErrorKindOutput,
			"render completion protocol",
			err,
			true,
		)
		terminalErr = joinFailures(terminalErr, outputErr)
	}
	if terminalErr != nil {
		return failureResult(application.root, terminalErr)
	}

	return Result{Command: CommandMetadata{command: application.root}}
}

func executeLifecycle(
	ctx context.Context,
	command *compiledCommand,
	invocation Invocation,
) (context.Context, error) {
	lifecycleContext := ctx
	core := func(nextContext context.Context) error {
		if nextContext == nil {
			return newInternalError("middleware continued with a nil context", nil)
		}
		lifecycleContext = nextContext
		if err := contextError(nextContext); err != nil {
			return err
		}
		for _, hook := range command.preRun {
			if err := hook(nextContext, invocation); err != nil {
				return classifyPhaseError(nextContext, "pre-run failed", err)
			}
			if err := contextError(nextContext); err != nil {
				return err
			}
		}
		if command.handler != nil {
			if err := command.handler(nextContext, invocation); err != nil {
				return classifyPhaseError(nextContext, "command execution failed", err)
			}
			if err := contextError(nextContext); err != nil {
				return err
			}
		}
		for _, hook := range command.postRun {
			if err := hook(nextContext, invocation); err != nil {
				return classifyPhaseError(nextContext, "post-run failed", err)
			}
			if err := contextError(nextContext); err != nil {
				return err
			}
		}

		return nil
	}

	next := Next(core)
	metadata := CommandMetadata{command: command}
	for index := len(command.middlewares) - 1; index >= 0; index-- {
		middleware := command.middlewares[index]
		downstream := next
		next = func(nextContext context.Context) error {
			if nextContext == nil {
				return newInternalError("invoke middleware with a nil context", nil)
			}
			if err := contextError(nextContext); err != nil {
				return err
			}
			continuation := newMiddlewareContinuation(downstream)
			err := middleware(nextContext, metadata, continuation.next)
			continuation.closeAndWait()
			if err != nil {
				var classified *Error
				if errors.As(err, &classified) {
					return err
				}

				return classifyPhaseError(nextContext, "command middleware failed", err)
			}
			if err := contextError(lifecycleContext); err != nil {
				return err
			}

			return contextError(nextContext)
		}
	}
	if err := next(ctx); err != nil {
		return lifecycleContext, err
	}

	return lifecycleContext, nil
}

const (
	middlewareContinuationOpen uint32 = iota
	middlewareContinuationRunning
	middlewareContinuationFinished
	middlewareContinuationClosed
)

type middlewareContinuation struct {
	downstream Next
	done       chan struct{}
	state      atomic.Uint32
}

func newMiddlewareContinuation(downstream Next) *middlewareContinuation {
	return &middlewareContinuation{downstream: downstream, done: make(chan struct{})}
}

func (continuation *middlewareContinuation) next(ctx context.Context) error {
	if !continuation.state.CompareAndSwap(
		middlewareContinuationOpen,
		middlewareContinuationRunning,
	) {
		return newInternalError("middleware continued more than once or after returning", nil)
	}
	err := continuation.downstream(ctx)
	continuation.state.Store(middlewareContinuationFinished)
	close(continuation.done)

	return err
}

func (continuation *middlewareContinuation) closeAndWait() {
	for {
		switch continuation.state.Load() {
		case middlewareContinuationOpen:
			if continuation.state.CompareAndSwap(
				middlewareContinuationOpen,
				middlewareContinuationClosed,
			) {
				return
			}
		case middlewareContinuationRunning:
			<-continuation.done
			return
		case middlewareContinuationFinished, middlewareContinuationClosed:
			return
		}
	}
}

func executeCleanup(
	ctx context.Context,
	command *compiledCommand,
	invocation Invocation,
) error {
	if len(command.cleanup) == 0 {
		return nil
	}
	cleanupContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		defaultCleanupTimeout,
	)
	defer cancel()

	var result error
	for index := len(command.cleanup) - 1; index >= 0; index-- {
		if err := command.cleanup[index](cleanupContext, invocation); err != nil {
			classified := newClassifiedError(
				ErrorKindCleanup,
				"command cleanup failed",
				err,
				true,
			)
			result = joinFailures(result, classified)
		}
	}

	return result
}

func classifyPhaseError(ctx context.Context, message string, err error) error {
	if contextErr := classifyPhaseContextError(ctx, err); contextErr != nil {
		return contextErr
	}
	var classified *Error
	if errors.As(err, &classified) {
		return err
	}

	return newClassifiedError(ErrorKindCommand, message, err, true)
}

func classifyPhaseContextError(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		return nil
	}
	if errors.Is(err, ctx.Err()) || errors.Is(err, contextErrorWithCause(ctx)) {
		return classifyContextError(contextErrorWithCause(ctx))
	}

	return nil
}

func joinFailures(primary, secondary error) error {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}

	return errors.Join(primary, secondary)
}

func resolveInput(command *compiledCommand, parsed engine.Result) (Input, error) {
	values := make(map[any]resolvedValue, len(command.effective)+len(command.arguments))
	for _, option := range command.effective {
		raw := parsed.Options[option.key]
		switch {
		case len(raw) > 0:
			value, err := option.parse(raw)
			if err != nil {
				return Input{}, invalidValue("option --"+option.name, option.secret, err)
			}
			values[option.binding] = resolvedValue{value: value, state: ValueExplicit}
		case option.hasDefault:
			values[option.binding] = resolvedValue{
				value: cloneDynamicValue(option.defaultVal),
				state: ValueDefaulted,
			}
		default:
			values[option.binding] = resolvedValue{state: ValueOmitted}
		}
		if option.required && values[option.binding].state == ValueOmitted {
			return Input{}, newClassifiedError(
				ErrorKindUsage,
				"required option --"+option.name+" is missing",
				nil,
				false,
			)
		}
	}
	if err := validateOptionGroups(command.groups, values); err != nil {
		return Input{}, err
	}

	position := 0
	for _, argument := range command.arguments {
		remaining := len(parsed.Arguments) - position
		var raw []string
		switch argument.cardinality {
		case ArgumentRequired:
			if remaining < 1 {
				return Input{}, newClassifiedError(
					ErrorKindUsage,
					"missing required argument "+argument.name,
					nil,
					false,
				)
			}
			raw = parsed.Arguments[position : position+1]
			position++
		case ArgumentOptional:
			if remaining > 0 {
				raw = parsed.Arguments[position : position+1]
				position++
			}
		case ArgumentRepeated, ArgumentRemainder:
			raw = parsed.Arguments[position:]
			position = len(parsed.Arguments)
		}
		if len(raw) == 0 && argument.cardinality == ArgumentOptional {
			values[argument.binding] = resolvedValue{state: ValueOmitted}
			continue
		}
		value, err := argument.parse(raw)
		if err != nil {
			return Input{}, invalidValue("argument "+argument.name, argument.secret, err)
		}
		values[argument.binding] = resolvedValue{value: value, state: ValueExplicit}
	}
	if position < len(parsed.Arguments) {
		if len(command.children) > 0 && len(command.arguments) == 0 {
			message := "unknown command " + safeToken(parsed.Arguments[position])
			if suggestion := suggestCommand(command, parsed.Arguments[position]); suggestion != "" {
				message += "; did you mean " + safeToken(suggestion) + "?"
			}
			return Input{}, newClassifiedError(
				ErrorKindUnknownCommand,
				message,
				nil,
				false,
			)
		}
		return Input{}, newClassifiedError(
			ErrorKindUsage,
			"unexpected positional argument "+safeToken(parsed.Arguments[position]),
			nil,
			false,
		)
	}

	return Input{values: values}, nil
}

func suggestCommand(command *compiledCommand, token string) string {
	input := []rune(token)
	if len(input) == 0 || len(input) > 64 {
		return ""
	}
	threshold := 2
	if len(input) <= 3 {
		threshold = 1
	}
	best := threshold + 1
	suggestion := ""
	considered := 0
	for _, child := range command.children {
		if child.hidden || considered >= 100 {
			continue
		}
		considered++
		candidate := []rune(child.name)
		if len(candidate) > 64 {
			continue
		}
		distance := boundedEditDistance(input, candidate, threshold)
		if distance < best {
			best = distance
			suggestion = child.name
		}
	}
	if best > threshold {
		return ""
	}

	return suggestion
}

func boundedEditDistance(left, right []rune, limit int) int {
	if difference := len(left) - len(right); difference > limit || difference < -limit {
		return limit + 1
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range left {
		current[0] = leftIndex + 1
		rowMinimum := current[0]
		for rightIndex, rightRune := range right {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[rightIndex+1] = min(
				min(current[rightIndex]+1, previous[rightIndex+1]+1),
				previous[rightIndex]+cost,
			)
			rowMinimum = min(rowMinimum, current[rightIndex+1])
		}
		if rowMinimum > limit {
			return limit + 1
		}
		previous, current = current, previous
	}

	return previous[len(right)]
}

func validateOptionGroups(
	groups []optionGroupSpec,
	values map[any]resolvedValue,
) error {
	for _, group := range groups {
		resolved := 0
		for _, binding := range group.bindings {
			if values[binding].state != ValueOmitted {
				resolved++
			}
		}
		switch group.kind {
		case optionGroupExclusive:
			if resolved > 1 {
				return newClassifiedError(
					ErrorKindUsage,
					"options --"+strings.Join(group.names, " and --")+" are mutually exclusive",
					nil,
					false,
				)
			}
		case optionGroupTogether:
			if resolved > 0 && resolved != len(group.bindings) {
				return newClassifiedError(
					ErrorKindUsage,
					"options --"+strings.Join(group.names, " and --")+" are required together",
					nil,
					false,
				)
			}
		}
	}

	return nil
}

func invalidValue(subject string, secret bool, cause error) error {
	if secret {
		cause = ErrMalformedValue
	}
	return newClassifiedError(
		ErrorKindMalformedValue,
		"invalid value for "+subject,
		cause,
		!secret,
	)
}

func engineCommand(command *compiledCommand) engine.Command {
	definition := engine.Command{
		ID:       command.id,
		Name:     command.name,
		Aliases:  cloneStrings(command.aliases),
		Summary:  command.summary,
		Version:  command.version,
		Options:  make([]engine.Option, len(command.options)),
		Children: make([]engine.Command, len(command.children)),
	}
	for index, option := range command.options {
		definition.Options[index] = engine.Option{
			Key:        option.key,
			Name:       option.name,
			Short:      option.short,
			Persistent: option.persistent,
			Boolean:    option.boolean,
		}
	}
	for index, child := range command.children {
		definition.Children[index] = engineCommand(child)
	}

	return definition
}

func validateArgv(argv []string, limits Limits) error {
	if len(argv) > limits.MaximumArguments {
		return newClassifiedError(ErrorKindUsage, "argument count exceeds limit", nil, false)
	}
	total := 0
	for _, token := range argv {
		if !utf8.ValidString(token) {
			return newClassifiedError(ErrorKindUsage, "argument is not valid UTF-8", nil, false)
		}
		if !strings.ContainsRune(token, '\x00') {
			total += len(token)
		} else {
			return newClassifiedError(ErrorKindUsage, "argument contains NUL", nil, false)
		}
		if total > limits.MaximumArgvBytes {
			return newClassifiedError(ErrorKindUsage, "argument input exceeds size limit", nil, false)
		}
	}

	return nil
}

func normalizeIO(request Request) IO {
	input := request.Stdin
	if input == nil {
		input = strings.NewReader("")
	}
	output := request.Stdout
	if output == nil {
		output = io.Discard
	}
	errorsOutput := request.Stderr
	if errorsOutput == nil {
		errorsOutput = io.Discard
	}

	return IO{Stdin: input, Stdout: output, Stderr: errorsOutput}
}

func failureResult(command *compiledCommand, err error) Result {
	result := Result{Err: err, ExitCode: exitCode(err)}
	if command != nil {
		result.Command = CommandMetadata{command: command}
	}

	return result
}

func finalize(
	streams IO,
	policy OutputPolicy,
	command *compiledCommand,
	output *Output,
	terminalErr error,
) Result {
	var renderErr error
	if terminalErr != nil {
		renderErr = renderFailure(streams.Stdout, streams.Stderr, policy, terminalErr)
	} else {
		renderErr = renderSuccess(streams.Stdout, policy, output)
	}
	if renderErr != nil {
		outputErr := newClassifiedError(ErrorKindOutput, "render command output", renderErr, true)
		terminalErr = joinFailures(terminalErr, outputErr)
	}
	if terminalErr != nil {
		return failureResult(command, terminalErr)
	}

	return Result{Command: CommandMetadata{command: command}}
}

func exitCode(err error) int {
	return defaultExitCodePolicy().code(err)
}

func (policy ExitCodePolicy) code(err error) int {
	var classified *Error
	if !errors.As(err, &classified) {
		return policy.Command
	}
	switch classified.Kind() {
	case ErrorKindUsage:
		return policy.Usage
	case ErrorKindUnknownCommand, ErrorKindUnknownOption, ErrorKindMissingValue:
		return policy.Usage
	case ErrorKindHelp, ErrorKindVersion:
		return 0
	case ErrorKindMalformedValue:
		return policy.Usage
	case ErrorKindCanceled:
		return policy.Canceled
	case ErrorKindDeadline:
		return policy.Deadline
	case ErrorKindInternal:
		return policy.Internal
	case ErrorKindCommand, ErrorKindValidation, ErrorKindCleanup,
		ErrorKindOutput, ErrorKindCompletion:
		return policy.Command
	default:
		return policy.Command
	}
}

func finalizeSignal(
	streams IO,
	policy OutputPolicy,
	command *compiledCommand,
	output *Output,
	kind ErrorKind,
) Result {
	if renderErr := renderSuccess(streams.Stdout, policy, output); renderErr != nil {
		return failureResult(command, newClassifiedError(
			ErrorKindOutput,
			"render command output",
			renderErr,
			true,
		))
	}
	signal := newClassifiedError(kind, string(kind)+" requested", nil, false)

	return Result{Err: signal, Command: CommandMetadata{command: command}}
}

func (application *Application) commandPath(commandID int) []string {
	var path []string
	var find func(*compiledCommand, []string) bool
	find = func(command *compiledCommand, current []string) bool {
		if command.id == commandID {
			path = append([]string(nil), current...)
			return true
		}
		for _, child := range command.children {
			if find(child, append(current, child.name)) {
				return true
			}
		}
		return false
	}
	find(application.root, nil)

	return path
}

func contextError(ctx context.Context) error {
	if ctx.Err() != nil {
		return classifyContextError(contextErrorWithCause(ctx))
	}

	return nil
}

func contextErrorWithCause(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	return ctx.Err()
}

func classifyContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return newClassifiedError(ErrorKindDeadline, "execution deadline exceeded", err, false)
	}

	return newClassifiedError(ErrorKindCanceled, "execution canceled", err, false)
}

func cloneDynamicValue(value any) any {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case map[string]string:
		clone := make(map[string]string, len(typed))
		for key, item := range typed {
			clone[key] = item
		}
		return clone
	default:
		return value
	}
}
