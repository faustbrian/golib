package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cli/internal/engine"
)

func TestErrorAndExitContractsCoverNilAndEveryClassification(t *testing.T) {
	t.Parallel()

	var nilError *Error
	if nilError.Kind() != "" || nilError.Error() != "<nil>" || nilError.Unwrap() != nil || nilError.Is(ErrCommand) {
		t.Fatal("nil classified error contract changed")
	}
	for kind, sentinel := range map[ErrorKind]error{
		ErrorKindHelp: ErrHelp, ErrorKindVersion: ErrVersion,
		ErrorKindUnknownCommand: ErrUnknownCommand, ErrorKindUnknownOption: ErrUnknownOption,
		ErrorKindMissingValue: ErrMissingValue, ErrorKindUsage: ErrUsage,
		ErrorKindMalformedValue: ErrMalformedValue, ErrorKindCommand: ErrCommand,
		ErrorKindValidation: ErrValidation, ErrorKindCleanup: ErrCleanup,
		ErrorKindOutput: ErrOutput, ErrorKindCompletion: ErrCompletion,
		ErrorKindCanceled: ErrCanceled, ErrorKindDeadline: ErrDeadline,
		ErrorKindInternal: ErrInternal,
	} {
		err := newClassifiedError(kind, string(kind), sentinel, false)
		if !errors.Is(err, sentinel) || !errors.Is(sentinelForKind(kind), sentinel) {
			t.Fatalf("kind %q does not match sentinel", kind)
		}
	}
	if sentinelForKind("other") != nil || (&Error{kind: "other"}).Is(errors.New("other")) {
		t.Fatal("unknown error kind matched")
	}
	if !(&Error{kind: ErrorKindCanceled}).Is(context.Canceled) || !(&Error{kind: ErrorKindDeadline}).Is(context.DeadlineExceeded) {
		t.Fatal("context sentinel matching changed")
	}

	policy := defaultExitCodePolicy()
	if policy.code(errors.New("plain")) != policy.Command ||
		policy.code(&Error{kind: "other"}) != policy.Command ||
		policy.code(newClassifiedError(ErrorKindCommand, "", nil, false)) != policy.Command ||
		policy.code(newClassifiedError(ErrorKindCanceled, "", nil, false)) != policy.Canceled ||
		policy.code(newClassifiedError(ErrorKindDeadline, "", nil, false)) != policy.Deadline ||
		policy.code(newClassifiedError(ErrorKindInternal, "", nil, false)) != policy.Internal {
		t.Fatal("exit policy mapping changed")
	}
}

func TestOutputBoundariesAreExplicitAndWriterSafe(t *testing.T) {
	t.Parallel()

	var nilOutput *Output
	if !errors.Is(nilOutput.Info("x"), ErrInternal) || !errors.Is(nilOutput.SetData("x"), ErrInternal) {
		t.Fatal("nil output did not return an internal error")
	}
	if snapshot := nilOutput.snapshot(); snapshot.hasData || len(snapshot.infos) != 0 {
		t.Fatalf("nil snapshot = %#v", snapshot)
	}
	output := &Output{}
	if err := output.Info(strings.Repeat("x", maximumOutputBytes+1)); !errors.Is(err, ErrOutput) {
		t.Fatalf("oversized info error = %v", err)
	}
	output.infos = make([]string, maximumOutputRecords)
	if err := output.Info("x"); !errors.Is(err, ErrOutput) {
		t.Fatalf("record limit error = %v", err)
	}
	if err := output.SetData(make(chan int)); !errors.Is(err, ErrOutput) {
		t.Fatalf("encoding error = %v", err)
	}
	if err := output.SetData(strings.Repeat("x", maximumOutputBytes+1)); !errors.Is(err, ErrOutput) {
		t.Fatalf("data limit error = %v", err)
	}
	if err := renderSuccess(io.Discard, OutputPolicy{Mode: 99}, output); !errors.Is(err, ErrInternal) {
		t.Fatalf("invalid render mode error = %v", err)
	}
	if classifiedError(errors.New("plain")) != nil {
		t.Fatal("plain error was classified")
	}
	if err := encodeAndWrite(io.Discard, make(chan int)); err == nil {
		t.Fatal("unsupported JSON value encoded")
	}
	if got := sanitizeTerminalMultiline("a\x00\nb\tc"); got != "a\nb\tc" {
		t.Fatalf("sanitized multiline = %q", got)
	}
}

func TestValueConversionFailuresAndDefensiveCopies(t *testing.T) {
	t.Parallel()

	input := Input{values: map[any]resolvedValue{
		"wrong": {value: int64(1), state: ValueExplicit},
		"slice": {value: []string{"one"}, state: ValueExplicit},
		"map":   {value: map[string]string{"one": "1"}, state: ValueExplicit},
	}}
	if bindingValue[string](input, "missing") != "" || bindingValue[string](input, "wrong") != "" {
		t.Fatal("missing or mismatched binding returned a value")
	}
	slice := bindingValue[[]string](input, "slice")
	slice[0] = "changed"
	pairs := bindingValue[map[string]string](input, "map")
	pairs["one"] = "changed"
	if input.values["slice"].value.([]string)[0] != "one" || input.values["map"].value.(map[string]string)["one"] != "1" {
		t.Fatal("binding values alias stored mutable input")
	}

	parsers := []func() error{
		func() error { _, err := parseBool(nil); return err },
		func() error { _, err := parseInt(nil); return err },
		func() error { _, err := parseUint(nil); return err },
		func() error { _, err := parseFloat(nil); return err },
		func() error { _, err := parseDuration(nil); return err },
		func() error { _, err := parseTime(time.RFC3339)(nil); return err },
		func() error { _, err := parseEnum([]string{"one"})(nil); return err },
		func() error { _, err := parseBool([]string{"not-bool"}); return err },
		func() error { _, err := parseInt([]string{"x"}); return err },
		func() error { _, err := parseUint([]string{"-1"}); return err },
		func() error { _, err := parseFloat([]string{"x"}); return err },
		func() error { _, err := parseDuration([]string{"x"}); return err },
		func() error { _, err := parseTime(time.RFC3339)([]string{"x"}); return err },
		func() error { _, err := parseEnum([]string{"one"})([]string{"two"}); return err },
		func() error { _, err := parseKeyValues([]string{"missing-separator"}); return err },
	}
	for index, parser := range parsers {
		if err := parser(); err == nil {
			t.Fatalf("parser %d accepted malformed input", index)
		}
	}
}

func TestGenerationAndCompletionDefensiveBoundaries(t *testing.T) {
	t.Parallel()

	var nilApplication *Application
	if _, err := nilApplication.Help(nil, HelpOptions{}); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil help error = %v", err)
	}
	if _, err := nilApplication.Markdown(); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil Markdown error = %v", err)
	}
	if _, err := nilApplication.Completion(ShellBash); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil completion script error = %v", err)
	}
	if _, err := nilApplication.Complete(context.Background(), nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil dynamic completion error = %v", err)
	}

	provider := func(_ context.Context, request CompletionRequest) ([]CompletionCandidate, error) {
		return []CompletionCandidate{{Value: request.Partial + "one"}}, nil
	}
	rootOption := StringOption("root").Persistent().Completion(provider)
	argument := StringsArgument("values").Completion(provider)
	application, err := Compile(NewCommand(
		"tool",
		WithOptions(rootOption),
		WithSubcommands(
			NewCommand("visible", WithAliases("alias"), WithArguments(argument), WithOptions(StringOption("a-very-long-option-name"))),
			NewCommand("hidden", WithHidden(true)),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if _, err := application.Complete(nilContext, nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil context completion error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.Complete(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled completion error = %v", err)
	}
	if _, err := application.Complete(context.Background(), []string{string([]byte{0xff})}); !errors.Is(err, ErrUsage) {
		t.Fatalf("hostile completion error = %v", err)
	}
	for _, argv := range [][]string{
		{"--root"}, {"--root", "value", ""}, {"--root=value", ""}, {"visible", "first", ""},
		{"alias", "--", "first", ""}, {"unknown", ""},
	} {
		if _, err := application.Complete(context.Background(), argv); err != nil {
			t.Fatalf("Complete(%q) error = %v", argv, err)
		}
	}
	if _, err := application.dynamicCandidates(context.Background(), nil, CompletionRequest{}); !errors.Is(err, ErrInternal) {
		t.Fatalf("nil provider error = %v", err)
	}
	providerFailure := errors.New("provider")
	if _, err := application.dynamicCandidates(context.Background(), func(context.Context, CompletionRequest) ([]CompletionCandidate, error) { return nil, providerFailure }, CompletionRequest{}); !errors.Is(err, ErrCompletion) {
		t.Fatalf("provider error = %v", err)
	}
	providerContext, cancelProvider := context.WithCancel(context.Background())
	if _, err := application.dynamicCandidates(providerContext, func(context.Context, CompletionRequest) ([]CompletionCandidate, error) {
		cancelProvider()
		return nil, nil
	}, CompletionRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-provider cancellation error = %v", err)
	}
	if findCompletionOption(application.root, "missing") != nil {
		t.Fatal("missing completion option resolved")
	}
	if _, err := application.dynamicCandidates(context.Background(), func(context.Context, CompletionRequest) ([]CompletionCandidate, error) {
		return nil, context.Canceled
	}, CompletionRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider cancellation error = %v", err)
	}
	bounded := application.boundCandidates([]CompletionCandidate{
		{Value: ""}, {Value: "one"}, {Value: "one"},
		{Value: strings.Repeat("x", application.limits.MaximumCompletionBytes+1)},
	})
	if len(bounded) != 1 || bounded[0].Value != "one" {
		t.Fatalf("bounded candidates = %#v", bounded)
	}
	if _, err := application.Completion("unknown"); !errors.Is(err, ErrUsage) {
		t.Fatalf("unknown shell error = %v", err)
	}
	if _, _, err := application.findCommand([]string{"missing"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("unknown help path error = %v", err)
	}
	if !contains([]string{"one"}, "one") || contains([]string{"one"}, "two") {
		t.Fatal("alias lookup changed")
	}
	for cardinality, expected := range map[ArgumentCardinality]string{
		ArgumentRequired: "<value>", ArgumentOptional: "[value]",
		ArgumentRepeated: "[value...]", ArgumentRemainder: "[value...]",
	} {
		if actual := argumentUsage(argumentSpec{name: "value", cardinality: cardinality}); actual != expected {
			t.Fatalf("usage %d = %q", cardinality, actual)
		}
	}
	if actual := argumentUsage(argumentSpec{name: "value", cardinality: 99}); actual != "<value>" {
		t.Fatalf("fallback usage = %q", actual)
	}
	command := application.root.children[0]
	if commandPath(command) != "tool visible" || optionLabel(command.options[0]) != "      --a-very-long-option-name" {
		t.Fatal("generation labels changed")
	}
	if help, helpErr := application.Help(nil, HelpOptions{}); helpErr != nil || !strings.Contains(help, "Commands:") || strings.Contains(help, "hidden ") {
		t.Fatalf("root help/error = %q/%v", help, helpErr)
	}
	if lines := wrapHelpLine("-x  abcdefghijkl", 8); len(lines) < 2 {
		t.Fatalf("narrow wrapping = %q", lines)
	}
	if lines := wrapHelpLine("          word", 5); len(lines) < 2 {
		t.Fatalf("indented narrow wrapping = %q", lines)
	}
	optionless := &compiledCommand{name: "child", effective: []optionSpec{{origin: "tool child"}}}
	if commandPath(optionless) != "tool child" || commandPath(&compiledCommand{name: "plain"}) != "plain" {
		t.Fatal("optionless command path changed")
	}
	manifest := manifestCommand(&compiledCommand{name: "parent", children: []*compiledCommand{{name: "nested"}}}, "tool parent")
	if len(manifest.Commands) != 1 || manifest.Commands[0].Path != "tool parent nested" {
		t.Fatalf("nested manifest = %#v", manifest)
	}
}

func TestRunDefensiveAndHeadlessBoundaries(t *testing.T) {
	t.Parallel()

	var nilApplication *Application
	if result := nilApplication.Run(context.Background(), Request{}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("nil application result = %#v", result)
	}
	application, err := Compile(NewCommand("tool", WithVersion("1"), WithHandler(func(_ context.Context, invocation Invocation) error {
		if !invocation.Interactive() {
			return errors.New("interaction unexpectedly disabled")
		}
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if result := application.Run(nilContext, Request{}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("nil context result = %#v", result)
	}
	if result := application.Run(context.Background(), Request{Output: OutputPolicy{Mode: 99}}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("invalid output result = %#v", result)
	}
	canceled, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("stop")
	cancel(cause)
	if result := application.Run(canceled, Request{}); !errors.Is(result.Err, cause) || result.ExitCode != 130 {
		t.Fatalf("canceled result = %#v", result)
	}
	if result := application.Run(context.Background(), Request{}); result.Err != nil {
		t.Fatalf("interactive result = %#v", result)
	}
	longHelp, err := Compile(
		NewCommand("tool", WithDescription(strings.Repeat("x", maximumOutputBytes+1))),
		WithLimits(Limits{MaximumMetadataBytes: maximumOutputBytes + 16}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := longHelp.Run(context.Background(), Request{Args: []string{"--help"}}); !errors.Is(result.Err, ErrOutput) {
		t.Fatalf("oversized help result = %#v", result)
	}

	corrupt := *application
	corrupt.commands = map[int]*compiledCommand{}
	if result := corrupt.Run(context.Background(), Request{}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("unknown parser selection = %#v", result)
	}

	longApplication, err := Compile(
		NewCommand("tool", WithVersion(strings.Repeat("x", maximumOutputBytes+1))),
		WithLimits(Limits{MaximumMetadataBytes: maximumOutputBytes + 16}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := longApplication.Run(context.Background(), Request{Args: []string{"--version"}}); !errors.Is(result.Err, ErrOutput) {
		t.Fatalf("oversized version result = %#v", result)
	}

	failingWriter := writerFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe })
	if result := application.Run(context.Background(), Request{Args: []string{"__complete"}, Stdout: failingWriter}); !errors.Is(result.Err, ErrOutput) {
		t.Fatalf("completion writer result = %#v", result)
	}
	completionFailure, err := Compile(NewCommand("tool", WithArguments(StringArgument("value").Completion(func(context.Context, CompletionRequest) ([]CompletionCandidate, error) {
		return nil, errors.New("completion")
	}))))
	if err != nil {
		t.Fatal(err)
	}
	if result := completionFailure.Run(context.Background(), Request{Args: []string{"__complete", ""}}); !errors.Is(result.Err, ErrCompletion) {
		t.Fatalf("completion provider result = %#v", result)
	}
	canceledCompletion, cancelCompletion := context.WithCancel(context.Background())
	cancelCompletion()
	if result := application.runCompletionBoundary(canceledCompletion, nil, false, normalizeIO(Request{})); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("canceled completion boundary = %#v", result)
	}
	middlewareApplication, err := Compile(NewCommand("tool", WithMiddleware(func(_ context.Context, _ CommandMetadata, next Next) error {
		return next(nil)
	})))
	if err != nil {
		t.Fatal(err)
	}
	if result := middlewareApplication.Run(context.Background(), Request{}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("nil middleware context result = %#v", result)
	}
}

func TestResolutionSuggestionAndContextInternals(t *testing.T) {
	t.Parallel()

	optional := StringArgument("optional").Optional()
	repeated := StringsArgument("repeated")
	application, err := Compile(NewCommand("tool", WithArguments(optional, repeated)))
	if err != nil {
		t.Fatal(err)
	}
	selected := application.root
	if _, err := resolveInput(selected, engine.Result{Arguments: []string{"one", "two", "three"}}); err != nil {
		t.Fatalf("optional/repeated input error = %v", err)
	}
	if _, err := resolveInput(&compiledCommand{arguments: []argumentSpec{{name: "value", cardinality: ArgumentOptional, binding: "value", parse: parseString}}}, engine.Result{}); err != nil {
		t.Fatalf("omitted optional input error = %v", err)
	}
	if _, err := resolveInput(&compiledCommand{arguments: []argumentSpec{{name: "value", cardinality: ArgumentRequired, binding: "value", parse: parseString}}}, engine.Result{}); !errors.Is(err, ErrUsage) {
		t.Fatalf("missing required input error = %v", err)
	}
	if _, err := resolveInput(&compiledCommand{arguments: []argumentSpec{{name: "value", cardinality: ArgumentRepeated, binding: "value", parse: parseStrings}}}, engine.Result{}); err != nil {
		t.Fatalf("empty repeated input error = %v", err)
	}
	if _, err := resolveInput(&compiledCommand{}, engine.Result{Arguments: []string{"extra"}}); !errors.Is(err, ErrUsage) {
		t.Fatalf("unexpected input error = %v", err)
	}
	children := make([]*compiledCommand, 102)
	for index := range children {
		children[index] = &compiledCommand{name: strings.Repeat("x", 65)}
	}
	children[0].hidden = true
	command := &compiledCommand{children: children}
	if suggestCommand(command, "") != "" || suggestCommand(command, strings.Repeat("x", 65)) != "" || suggestCommand(command, "no-match") != "" {
		t.Fatal("bounded suggestions returned an unsafe candidate")
	}
	if suggestCommand(&compiledCommand{}, "ab") != "" {
		t.Fatal("short suggestion threshold returned a candidate")
	}
	if boundedEditDistance([]rune("a"), []rune("long"), 1) != 2 || boundedEditDistance([]rune("abc"), []rune("xyz"), 1) != 2 {
		t.Fatal("bounded edit distance did not stop early")
	}
	if err := validateArgv([]string{"nul\x00"}, defaultLimits()); !errors.Is(err, ErrUsage) {
		t.Fatalf("NUL argv error = %v", err)
	}
	if err := validateOptionName(""); !errors.Is(err, ErrInternal) {
		t.Fatalf("empty option name error = %v", err)
	}
	for failure, expected := range map[error]ErrorKind{
		errors.New("plain"):                                    ErrorKindUsage,
		&engine.ParseError{Kind: engine.FailureUsage}:          ErrorKindUsage,
		&engine.ParseError{Kind: engine.FailureUnknownCommand}: ErrorKindUnknownCommand,
		&engine.ParseError{Kind: engine.FailureUnknownOption}:  ErrorKindUnknownOption,
		&engine.ParseError{Kind: engine.FailureMissingValue}:   ErrorKindMissingValue,
		&engine.ParseError{Kind: 99}:                           ErrorKindUsage,
	} {
		if actual := classifyParseFailure(failure); actual != expected {
			t.Fatalf("parse failure %v classified as %q", failure, actual)
		}
	}
	if result := application.run(&changingContext{}, Request{Args: []string{"--missing"}}); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("parse-time cancellation result = %#v", result)
	}
	if path := application.commandPath(999); path != nil {
		t.Fatalf("missing command path = %v", path)
	}
	if contextErrorWithCause(context.Background()) != nil {
		t.Fatal("background context has a cause")
	}
	if !errors.Is(classifyContextError(context.DeadlineExceeded), ErrDeadline) {
		t.Fatal("deadline was not classified")
	}
	values := []any{[]string{"one"}, map[string]string{"one": "1"}, 1}
	for _, value := range values {
		if cloneDynamicValue(value) == nil {
			t.Fatal("dynamic clone lost its value")
		}
	}
	output := &Output{}
	if err := output.SetData("help"); err != nil {
		t.Fatal(err)
	}
	if result := finalizeSignal(IO{Stdout: writerFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe })}, OutputPolicy{}, application.root, output, ErrorKindHelp); !errors.Is(result.Err, ErrOutput) {
		t.Fatalf("signal writer result = %#v", result)
	}
}

func TestShutdownNilReceiverAndRepeatedSignals(t *testing.T) {
	t.Parallel()

	var controller *ShutdownController
	if controller.Context() != nil || controller.Forced() != nil || controller.Signal(nil) != ShutdownAlreadyForced {
		t.Fatal("nil shutdown controller contract changed")
	}
	controller, err := NewShutdownController(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if controller.Signal(nil) != ShutdownGraceful || !errors.Is(context.Cause(controller.Context()), ErrSignal) {
		t.Fatal("default signal cause was not retained")
	}
	if controller.Signal(nil) != ShutdownForced || controller.Signal(nil) != ShutdownAlreadyForced {
		t.Fatal("repeated signal policy changed")
	}
	select {
	case <-controller.Forced():
	default:
		t.Fatal("forced channel is open")
	}
}

func TestMiddlewareContinuationWaitsForInFlightWork(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	continuation := newMiddlewareContinuation(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	nextDone := make(chan error, 1)
	go func() { nextDone <- continuation.next(context.Background()) }()
	<-started

	timer := time.AfterFunc(25*time.Millisecond, func() { close(release) })
	defer timer.Stop()
	continuation.closeAndWait()
	if err := <-nextDone; err != nil {
		t.Fatalf("Next() error = %v", err)
	}
}

type writerFunc func([]byte) (int, error)

func (function writerFunc) Write(value []byte) (int, error) { return function(value) }

type changingContext struct{ calls int }

func (ctx *changingContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *changingContext) Done() <-chan struct{}       { return nil }
func (ctx *changingContext) Value(any) any               { return nil }
func (ctx *changingContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}
