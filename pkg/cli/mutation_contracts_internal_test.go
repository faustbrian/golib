package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompilationExactBoundariesAndAllocatedIdentities(t *testing.T) {
	t.Parallel()

	firstArgument := StringArgument("first")
	secondArgument := StringArgument("second").Optional()
	firstOption := BoolOption("alpha")
	secondOption := BoolOption("zulu")
	root := NewCommand(
		"r",
		WithArguments(firstArgument, secondArgument),
		WithOptions(firstOption, secondOption),
	)
	application, err := Compile(root, WithLimits(Limits{
		MaximumCommandDepth: 1, MaximumCommands: 1,
		MaximumArgumentsPerCommand: 2, MaximumOptionsPerCommand: 2,
	}))
	if err != nil {
		t.Fatalf("Compile(exact root limits) error = %v", err)
	}
	if application.root.id != 0 ||
		application.root.arguments[0].key != 0 || application.root.arguments[1].key != 1 ||
		application.root.options[0].key != 2 || application.root.options[1].key != 3 {
		t.Fatalf("allocated identities = command %d, arguments %d/%d, options %d/%d",
			application.root.id,
			application.root.arguments[0].key, application.root.arguments[1].key,
			application.root.options[0].key, application.root.options[1].key,
		)
	}

	child := NewCommand("c", WithSubcommands(NewCommand("g")))
	tree, err := Compile(NewCommand("r", WithSubcommands(child)), WithLimits(Limits{
		MaximumCommandDepth: 3, MaximumCommands: 3,
	}))
	if err != nil {
		t.Fatalf("Compile(exact tree limits) error = %v", err)
	}
	if tree.root.id != 0 || tree.root.children[0].id != 1 || tree.root.children[0].children[0].id != 2 {
		t.Fatalf("command identities = %d/%d/%d",
			tree.root.id, tree.root.children[0].id, tree.root.children[0].children[0].id,
		)
	}

	if _, err := Compile(NewCommand("tool"), WithLimits(Limits{MaximumMetadataBytes: 4})); err != nil {
		t.Fatalf("Compile(exact metadata limit) error = %v", err)
	}
	for _, code := range []int{1, 255} {
		policy := ExitCodePolicy{Usage: code, Command: code, Canceled: code, Deadline: code, Internal: code}
		if _, err := Compile(NewCommand("tool"), WithExitCodePolicy(policy)); err != nil {
			t.Fatalf("Compile(exit code %d) error = %v", code, err)
		}
	}
}

func TestOptionGroupSatisfiabilityTraversesEveryComponent(t *testing.T) {
	t.Parallel()

	a, b, c := new(int), new(int), new(int)
	options := []optionSpec{
		{binding: a, required: true},
		{binding: b},
		{binding: c, required: true},
	}
	together := optionGroupSpec{kind: optionGroupTogether, bindings: []any{a, b}}
	optionalOptions := []optionSpec{{binding: a}, {binding: b}, {binding: c}}
	if err := validateGroupSatisfiability([]optionGroupSpec{
		together,
		{kind: optionGroupExclusive, bindings: []any{a, b}},
	}, optionalOptions); err != nil {
		t.Fatalf("exclusive duplicate within one component error = %v", err)
	}
	if err := validateGroupSatisfiability([]optionGroupSpec{
		together,
		{kind: optionGroupExclusive, bindings: []any{a, b, c}},
	}, options); !errors.Is(err, ErrInternal) {
		t.Fatalf("exclusive forced components error = %v", err)
	}
	if err := validateGroupSatisfiability([]optionGroupSpec{
		{kind: optionGroupExclusive, bindings: []any{a, c}},
		together,
	}, []optionSpec{{binding: a, required: true}, {binding: b}, {binding: c}}); err != nil {
		t.Fatalf("exclusive group processed as a union error = %v", err)
	}
	if err := validateGroupSatisfiability([]optionGroupSpec{
		{kind: optionGroupExclusive, bindings: []any{a, b}},
		together,
	}, []optionSpec{{binding: a, required: true}, {binding: b}}); !errors.Is(err, ErrInternal) {
		t.Fatalf("together group after exclusive group error = %v", err)
	}

	d := new(int)
	chainedOptions := append([]optionSpec(nil), options...)
	chainedOptions = append(chainedOptions, optionSpec{binding: d})
	if err := validateGroupSatisfiability([]optionGroupSpec{
		{kind: optionGroupTogether, bindings: []any{a, b}},
		{kind: optionGroupTogether, bindings: []any{b, d}},
		{kind: optionGroupExclusive, bindings: []any{a, d}},
	}, chainedOptions); !errors.Is(err, ErrInternal) {
		t.Fatalf("chained union error = %v", err)
	}
	if err := validateGroupSatisfiability([]optionGroupSpec{
		{kind: optionGroupTogether, bindings: []any{a, b}},
		{kind: optionGroupExclusive, bindings: []any{a, b, c, d}},
	}, []optionSpec{
		{binding: a}, {binding: b}, {binding: c, required: true}, {binding: d, required: true},
	}); !errors.Is(err, ErrInternal) {
		t.Fatalf("duplicate optional component traversal error = %v", err)
	}
}

func TestASCIIAlphaNumericIncludesOnlyExactRanges(t *testing.T) {
	t.Parallel()

	for _, character := range []rune{'a', 'z', 'A', 'Z', '0', '9'} {
		if !isASCIIAlphaNumeric(character) {
			t.Fatalf("isASCIIAlphaNumeric(%q) = false", character)
		}
	}
	for _, character := range []rune{'`', '{', '@', '[', '/', ':'} {
		if isASCIIAlphaNumeric(character) {
			t.Fatalf("isASCIIAlphaNumeric(%q) = true", character)
		}
	}
}

func TestCompletionPositionPreservesTokenGrammar(t *testing.T) {
	t.Parallel()

	value := optionSpec{name: "value", short: 'v'}
	boolean := optionSpec{name: "all", short: 'a', boolean: true}
	child := &compiledCommand{name: "child", aliases: []string{"alias"}}
	root := &compiledCommand{
		name: "tool", effective: []optionSpec{boolean, value}, children: []*compiledCommand{child},
	}

	for _, token := range []string{"", "value", "--value", "-"} {
		if option, attached, ok := completionAttachedShortOption(root, token); ok || option != nil || attached != "" {
			t.Fatalf("completionAttachedShortOption(%q) = %#v/%q/%v", token, option, attached, ok)
		}
	}
	if option, attached, ok := completionAttachedShortOption(root, "-avtail"); !ok || option == nil || option.name != "value" || attached != "tail" {
		t.Fatalf("completionAttachedShortOption(cluster) = %#v/%q/%v", option, attached, ok)
	}

	assertPosition := func(tokens []string, wantCommand *compiledCommand, wantPositional int, wantPending string) {
		t.Helper()
		command, positional, pending := completionPosition(root, tokens)
		pendingName := ""
		if pending != nil {
			pendingName = pending.name
		}
		if command != wantCommand || positional != wantPositional || pendingName != wantPending {
			t.Fatalf("completionPosition(%q) = %p/%d/%q, want %p/%d/%q",
				tokens, command, positional, pendingName, wantCommand, wantPositional, wantPending,
			)
		}
	}
	assertPosition([]string{"--", "one", "two"}, root, 2, "")
	assertPosition([]string{"positional", "--", "one", "two"}, root, 3, "")
	assertPosition([]string{"--value"}, root, 0, "value")
	assertPosition([]string{"--value=assigned"}, root, 0, "")
	assertPosition([]string{"--all"}, root, 0, "")
	assertPosition([]string{"-a"}, root, 0, "")
	assertPosition([]string{"-av"}, root, 0, "value")
	assertPosition([]string{"-vattached"}, root, 0, "")
	assertPosition([]string{"-vv"}, root, 0, "")
	assertPosition([]string{"-?"}, root, 1, "")
	assertPosition([]string{"-?a"}, root, 1, "")
	assertPosition([]string{"-a", "positional"}, root, 1, "")
	assertPosition([]string{"--value", "consumed", "positional"}, root, 1, "")
	assertPosition([]string{"positional", "alias"}, child, 0, "")
}

func TestCompletionArgumentAndCandidateBoundsAreExact(t *testing.T) {
	t.Parallel()

	for _, cardinality := range []ArgumentCardinality{ArgumentRepeated, ArgumentRemainder} {
		command := &compiledCommand{arguments: []argumentSpec{{name: "values", cardinality: cardinality}}}
		if argument := completionArgument(command, 3); argument == nil || argument.name != "values" {
			t.Fatalf("completionArgument(%d) = %#v", cardinality, argument)
		}
	}
	if argument := completionArgument(&compiledCommand{arguments: []argumentSpec{{
		name: "value", cardinality: ArgumentOptional,
	}}}, 1); argument != nil {
		t.Fatalf("completionArgument(optional overflow) = %#v", argument)
	}

	application := &Application{limits: Limits{MaximumCompletionResults: 10, MaximumCompletionBytes: 6}}
	bounded := application.boundCandidates([]CompletionCandidate{
		{Value: "oversized", Description: "value"},
		{Value: "exact", Description: "x"},
	})
	if len(bounded) != 1 || bounded[0].Value != "exact" {
		t.Fatalf("exact byte bounds = %#v", bounded)
	}
	bounded = application.boundCandidates([]CompletionCandidate{
		{Value: "a", Description: "12"},
		{Value: "b", Description: "3"},
		{Value: "c", Description: "4"},
	})
	if len(bounded) != 2 || bounded[0].Value != "a" || bounded[1].Value != "b" {
		t.Fatalf("cumulative byte bounds = %#v", bounded)
	}
	application.limits.MaximumCompletionResults = 1
	bounded = application.boundCandidates([]CompletionCandidate{{Value: "a"}, {Value: "b"}})
	if len(bounded) != 1 || bounded[0].Value != "a" {
		t.Fatalf("result count bounds = %#v", bounded)
	}
}

func TestCompletionSkipsHiddenChildrenAndPreservesDeadline(t *testing.T) {
	t.Parallel()

	application := &Application{
		root: &compiledCommand{children: []*compiledCommand{
			{name: "hidden", hidden: true},
			{name: "visible"},
		}},
		limits: defaultLimits(),
	}
	candidates, err := application.Complete(context.Background(), []string{""})
	if err != nil || len(candidates) != 1 || candidates[0].Value != "visible" {
		t.Fatalf("visible candidates = %#v, error = %v", candidates, err)
	}
	if _, err := application.dynamicCandidates(context.Background(), func(context.Context, CompletionRequest) ([]CompletionCandidate, error) {
		return nil, context.DeadlineExceeded
	}, CompletionRequest{}); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCompletion) {
		t.Fatalf("provider deadline error = %v", err)
	}
}

func TestHelpAndMarkdownRenderEveryIndependentSection(t *testing.T) {
	t.Parallel()

	for name, command := range map[string]*compiledCommand{
		"experimental": {name: "tool", experimental: true},
		"deprecated":   {name: "tool", deprecated: "old"},
		"replacement":  {name: "tool", replacement: "new"},
	} {
		help, err := (&Application{root: command}).Help(nil, HelpOptions{})
		if err != nil || !strings.Contains(help, "Status:\n") {
			t.Fatalf("Help(%s) = %q, %v", name, help, err)
		}
	}
	emptyHelp, err := (&Application{root: &compiledCommand{name: "tool"}}).Help(nil, HelpOptions{})
	if err != nil || strings.Contains(emptyHelp, "Status:") || strings.Contains(emptyHelp, "Arguments:") ||
		strings.Contains(emptyHelp, "Options:") || strings.Contains(emptyHelp, "Aliases:") ||
		strings.Contains(emptyHelp, "Examples:") || strings.Contains(emptyHelp, "[options]") {
		t.Fatalf("empty Help() = %q, %v", emptyHelp, err)
	}
	rich := &compiledCommand{
		name: "tool", aliases: []string{"t"}, examples: []string{"tool run"},
		children:  []*compiledCommand{{name: "hidden", hidden: true}, {name: "visible"}},
		arguments: []argumentSpec{{name: "value", cardinality: ArgumentRequired}},
		options:   []optionSpec{{name: "local", origin: "tool"}},
		effective: []optionSpec{{name: "local", origin: "tool"}},
	}
	richHelp, err := (&Application{root: rich}).Help(nil, HelpOptions{})
	if err != nil || !strings.Contains(richHelp, "visible") || strings.Contains(richHelp, "hidden") ||
		!strings.Contains(richHelp, "Arguments:") || !strings.Contains(richHelp, "Options:") ||
		!strings.Contains(richHelp, "Aliases:") || !strings.Contains(richHelp, "Examples:") {
		t.Fatalf("rich Help() = %q, %v", richHelp, err)
	}

	for name, command := range map[string]*compiledCommand{
		"experimental": {name: "tool", experimental: true},
		"hidden":       {name: "tool", hidden: true},
		"deprecated":   {name: "tool", deprecated: "old"},
		"replacement":  {name: "tool", replacement: "new"},
	} {
		var output strings.Builder
		writeMarkdownCommand(&output, command, "tool", 2)
		if !strings.Contains(output.String(), "### Status\n") {
			t.Fatalf("Markdown(%s) = %q", name, output.String())
		}
	}
	markdownCommand := &compiledCommand{
		name: "tool", summary: "summary", description: "description",
		aliases: []string{"t"}, examples: []string{"tool run\nnext"},
		arguments: []argumentSpec{{name: "value", valueType: "string", description: "argument"}},
		effective: []optionSpec{{name: "inherited", valueType: "bool", description: "option", origin: "parent"}},
	}
	var markdown strings.Builder
	writeMarkdownCommand(&markdown, markdownCommand, "tool", 2)
	for _, fragment := range []string{
		"summary", "description", "### Arguments", ": argument", "### Options",
		": option", "Inherited from `parent`.", "### Aliases", "### Examples",
	} {
		if !strings.Contains(markdown.String(), fragment) {
			t.Fatalf("Markdown missing %q: %q", fragment, markdown.String())
		}
	}
	var emptyMarkdown strings.Builder
	writeMarkdownCommand(&emptyMarkdown, &compiledCommand{name: "tool"}, "tool", 2)
	for _, section := range []string{"### Arguments", "### Options", "### Aliases", "### Examples"} {
		if strings.Contains(emptyMarkdown.String(), section) {
			t.Fatalf("empty Markdown contains %q: %q", section, emptyMarkdown.String())
		}
	}
	var localMarkdown strings.Builder
	writeMarkdownCommand(&localMarkdown, &compiledCommand{
		name:      "tool",
		options:   []optionSpec{{name: "local", valueType: "bool", origin: "tool"}},
		effective: []optionSpec{{name: "local", valueType: "bool", origin: "tool"}},
	}, "tool", 2)
	if !strings.Contains(localMarkdown.String(), "### Options") {
		t.Fatalf("local option Markdown = %q", localMarkdown.String())
	}
	for name, command := range map[string]*compiledCommand{
		"empty": {name: "tool", summary: "summary"},
		"equal": {name: "tool", summary: "summary", description: "summary"},
	} {
		var output strings.Builder
		writeMarkdownCommand(&output, command, "tool", 2)
		if strings.Count(output.String(), "summary") != 1 {
			t.Fatalf("Markdown(%s) duplicated description: %q", name, output.String())
		}
	}
}

func TestGenerationExactWrappingAndMetadataBoundaries(t *testing.T) {
	t.Parallel()

	if got := wrapHelp("abcd\n", 0); got != "abcd\n" {
		t.Fatalf("wrapHelp(width zero) = %q", got)
	}
	if got := wrapHelp("ab\n", 1); got != "a\nb\n" {
		t.Fatalf("wrapHelp(width one) = %q", got)
	}
	if got := wrapHelpLine("abcd", 4); len(got) != 1 || got[0] != "abcd" {
		t.Fatalf("wrapHelpLine(exact width) = %#v", got)
	}
	for _, test := range []struct {
		line  string
		width int
	}{
		{line: "-x  alpha beta", width: 8},
		{line: "  alpha beta", width: 6},
		{line: "-long  alpha", width: 6},
		{line: "word  alpha", width: 6},
		{line: "-abcdef", width: 3},
	} {
		lines := wrapHelpLine(test.line, test.width)
		if len(lines) < 2 {
			t.Fatalf("wrapHelpLine(%q, %d) = %#v", test.line, test.width, lines)
		}
		for _, line := range lines {
			if len([]rune(line)) > test.width {
				t.Fatalf("wrapped line %q exceeds width %d", line, test.width)
			}
		}
		if strings.HasPrefix(test.line, "-long") {
			for _, line := range lines[1:] {
				if !strings.HasPrefix(line, "     ") {
					t.Fatalf("option continuation indentation = %#v", lines)
				}
			}
		}
		if strings.HasPrefix(test.line, "word") {
			for _, line := range lines[1:] {
				if strings.HasPrefix(line, " ") {
					t.Fatalf("non-option continuation indentation = %#v", lines)
				}
			}
		}
	}

	if got := manifestOption(optionSpec{name: "plain"}).Short; got != "" {
		t.Fatalf("manifest zero short = %q", got)
	}
	if got := manifestOption(optionSpec{name: "short", short: 's'}).Short; got != "s" {
		t.Fatalf("manifest short = %q", got)
	}
	if got := commandPath(&compiledCommand{name: "child", effective: []optionSpec{{origin: "tool child"}}}); got != "tool child" {
		t.Fatalf("commandPath(suffix) = %q", got)
	}
	if got := commandPath(&compiledCommand{name: "child", effective: []optionSpec{{origin: "child"}}}); got != "child" {
		t.Fatalf("commandPath(exact) = %q", got)
	}
	application := &Application{root: &compiledCommand{name: "tool", children: []*compiledCommand{{
		name: "child", aliases: []string{"alias"},
	}}}}
	if command, canonical, err := application.findCommand([]string{"alias"}); err != nil || command.name != "child" || canonical != "tool child" {
		t.Fatalf("findCommand(alias) = %#v/%q/%v", command, canonical, err)
	}
}

type divergentOutputValue struct {
	json  string
	human string
}

func (value divergentOutputValue) MarshalJSON() ([]byte, error) { return json.Marshal(value.json) }
func (value divergentOutputValue) String() string               { return value.human }

func TestOutputAcceptsExactLimitsAndTracksCumulativeBytes(t *testing.T) {
	t.Parallel()

	records := &Output{infos: make([]string, maximumOutputRecords-1)}
	if err := records.Info(""); err != nil || len(records.infos) != maximumOutputRecords {
		t.Fatalf("Info(exact record limit) = %v, records = %d", err, len(records.infos))
	}
	if err := records.Info(""); !errors.Is(err, ErrOutput) {
		t.Fatalf("Info(over record limit) = %v", err)
	}

	bytesOutput := &Output{}
	if err := bytesOutput.Info("ab"); err != nil {
		t.Fatal(err)
	}
	if err := bytesOutput.Info(strings.Repeat("x", maximumOutputBytes-2)); err != nil {
		t.Fatalf("Info(exact byte limit) = %v", err)
	}
	if err := bytesOutput.Info("x"); !errors.Is(err, ErrOutput) {
		t.Fatalf("Info(over byte limit) = %v", err)
	}
	combined := &Output{bytes: 1, dataBytes: 1}
	if err := combined.Info(strings.Repeat("x", maximumOutputBytes-1)); !errors.Is(err, ErrOutput) {
		t.Fatalf("Info(combined byte overflow) = %v", err)
	}

	exactData := &Output{bytes: maximumOutputBytes - 3}
	if err := exactData.SetData("x"); err != nil {
		t.Fatalf("SetData(exact cumulative limit) = %v", err)
	}
	if err := (&Output{}).SetData(strings.Repeat("x", maximumOutputBytes-2)); err != nil {
		t.Fatalf("SetData(exact encoded limit) = %v", err)
	}
	for name, value := range map[string]divergentOutputValue{
		"encoded": {json: strings.Repeat("x", maximumOutputBytes), human: "small"},
		"human":   {json: "small", human: strings.Repeat("x", maximumOutputBytes+1)},
	} {
		if err := (&Output{}).SetData(value); !errors.Is(err, ErrOutput) {
			t.Fatalf("SetData(%s overflow) = %v", name, err)
		}
	}
}

func TestRunPreflightSeparatesApplicationModeAndContextFailures(t *testing.T) {
	t.Parallel()

	for name, application := range map[string]*Application{
		"nil":   nil,
		"empty": {},
	} {
		for _, run := range []func(context.Context, Request) Result{
			application.Run,
			application.RunCommand,
		} {
			if result := run(context.Background(), Request{}); !errors.Is(result.Err, ErrInternal) {
				t.Fatalf("%s application result = %#v", name, result)
			}
		}
	}

	called := 0
	application, err := Compile(NewCommand("tool", WithHandler(func(context.Context, Invocation) error {
		called++

		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []OutputMode{OutputHuman, OutputJSON, OutputQuiet} {
		if result := application.RunCommand(context.Background(), Request{Output: OutputPolicy{Mode: mode}}); result.Err != nil {
			t.Fatalf("RunCommand(mode %d) = %#v", mode, result)
		}
	}
	if called != 3 {
		t.Fatalf("valid mode handler calls = %d", called)
	}
	if result := application.RunCommand(context.Background(), Request{
		Output: OutputPolicy{Mode: OutputMode(255)},
	}); !errors.Is(result.Err, ErrInternal) {
		t.Fatalf("RunCommand(invalid mode) = %#v", result)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, run := range []func(context.Context, Request) Result{application.Run, application.RunCommand} {
		if result := run(canceled, Request{}); !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("canceled run = %#v", result)
		}
	}
	if called != 3 {
		t.Fatalf("canceled handler calls = %d", called)
	}
}

func TestCleanupUsesTheDocumentedDefaultDeadline(t *testing.T) {
	t.Parallel()

	var remaining time.Duration
	command := &compiledCommand{cleanup: []Handler{func(ctx context.Context, _ Invocation) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline")
		}
		remaining = time.Until(deadline)

		return nil
	}}}
	if err := executeCleanup(context.Background(), command, Invocation{}); err != nil {
		t.Fatalf("executeCleanup() error = %v", err)
	}
	if remaining < 29*time.Second || remaining > 30*time.Second {
		t.Fatalf("cleanup deadline remaining = %v", remaining)
	}
}

func TestCompletionProtocolDescriptionAndContextMatrix(t *testing.T) {
	t.Parallel()

	application := &Application{
		root: &compiledCommand{name: "tool", children: []*compiledCommand{
			{name: "described", summary: "description"},
			{name: "plain"},
		}},
		limits: defaultLimits(),
	}
	for _, test := range []struct {
		name                string
		withoutDescriptions bool
		want                string
	}{
		{name: "descriptions", want: "described\tdescription\nplain\n:4\n"},
		{name: "without descriptions", withoutDescriptions: true, want: "described\nplain\n:4\n"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			result := application.runCompletionBoundary(
				context.Background(),
				[]string{""},
				test.withoutDescriptions,
				IO{Stdout: &stdout},
			)
			if result.Err != nil || stdout.String() != test.want {
				t.Fatalf("runCompletionBoundary() = %#v, output = %q", result, stdout.String())
			}
		})
	}
	var protocol bytes.Buffer
	result := application.Run(context.Background(), Request{
		Args: []string{"__completeNoDesc", ""}, Stdout: &protocol,
	})
	if result.Err != nil || protocol.String() != "described\nplain\n:4\n" {
		t.Fatalf("Run(__completeNoDesc) = %#v, output = %q", result, protocol.String())
	}
	for name, makeContext := range map[string]func() context.Context{
		"canceled": func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			return ctx
		},
		"deadline": func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()

			return ctx
		},
	} {
		var stdout bytes.Buffer
		result := application.runCompletionBoundary(makeContext(), nil, false, IO{Stdout: &stdout})
		var classified *Error
		if !errors.As(result.Err, &classified) ||
			name == "canceled" && classified.Kind() != ErrorKindCanceled ||
			name == "deadline" && classified.Kind() != ErrorKindDeadline ||
			stdout.String() != ":5\n" {
			t.Fatalf("%s completion = %#v, output = %q", name, result, stdout.String())
		}
	}
}

func TestInvocationInteractiveStateMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		interaction    Interaction
		nonInteractive bool
		want           bool
		wantUsage      bool
	}{
		{name: "optional", interaction: InteractionOptional, want: true},
		{name: "optional non-interactive", interaction: InteractionOptional, nonInteractive: true},
		{name: "required", interaction: InteractionRequired, want: true},
		{name: "required non-interactive", interaction: InteractionRequired, nonInteractive: true, wantUsage: true},
		{name: "forbidden", interaction: InteractionForbidden},
		{name: "forbidden non-interactive", interaction: InteractionForbidden, nonInteractive: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			called := false
			application, err := Compile(NewCommand(
				"tool",
				WithInteraction(test.interaction),
				WithHandler(func(_ context.Context, invocation Invocation) error {
					called = true
					if invocation.Interactive() != test.want {
						t.Fatalf("Interactive() = %v, want %v", invocation.Interactive(), test.want)
					}

					return nil
				}),
			))
			if err != nil {
				t.Fatal(err)
			}
			result := application.RunCommand(context.Background(), Request{NonInteractive: test.nonInteractive})
			if test.wantUsage {
				if !errors.Is(result.Err, ErrUsage) || called {
					t.Fatalf("RunCommand() = %#v, called = %v", result, called)
				}
			} else if result.Err != nil || !called {
				t.Fatalf("RunCommand() = %#v, called = %v", result, called)
			}
		})
	}
}

func TestSuggestionBoundsAndTieBreaking(t *testing.T) {
	t.Parallel()

	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: "bat"}, {name: "car"}}}, "cat"); got != "bat" {
		t.Fatalf("tie suggestion = %q", got)
	}
	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: "bat"}}}, "cat"); got != "bat" {
		t.Fatalf("threshold suggestion = %q", got)
	}
	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: "bots"}}}, "cats"); got != "bots" {
		t.Fatalf("four-rune threshold suggestion = %q", got)
	}
	long64 := strings.Repeat("x", 64)
	long65 := strings.Repeat("x", 65)
	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: long64}}}, long64); got != long64 {
		t.Fatalf("64-rune suggestion = %q", got)
	}
	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: long65}}}, long65); got != "" {
		t.Fatalf("65-rune suggestion = %q", got)
	}
	if got := suggestCommand(&compiledCommand{children: []*compiledCommand{{name: long65}, {name: "target"}}}, "target"); got != "target" {
		t.Fatalf("oversized candidate stopped traversal, suggestion = %q", got)
	}

	children := make([]*compiledCommand, 0, 101)
	for index := range 100 {
		children = append(children, &compiledCommand{name: "candidate" + string(rune('Ā'+index))})
	}
	children = append(children, &compiledCommand{name: "target"})
	if got := suggestCommand(&compiledCommand{children: children}, "target"); got != "" {
		t.Fatalf("candidate beyond bound = %q", got)
	}
	for index := range 100 {
		children[index].hidden = true
	}
	if got := suggestCommand(&compiledCommand{children: children}, "target"); got != "target" {
		t.Fatalf("hidden candidates consumed bound, suggestion = %q", got)
	}
}

func TestBoundedEditDistanceMatchesReferenceAtEverySmallBoundary(t *testing.T) {
	t.Parallel()

	values := []string{"", "a", "b", "aa", "ab", "ba", "bb", "aaa", "aba", "bbb"}
	for _, left := range values {
		for _, right := range values {
			wantDistance := referenceEditDistance([]rune(left), []rune(right))
			for limit := range 4 {
				want := wantDistance
				if want > limit {
					want = limit + 1
				}
				if got := boundedEditDistance([]rune(left), []rune(right), limit); got != want {
					t.Fatalf("boundedEditDistance(%q, %q, %d) = %d, want %d", left, right, limit, got, want)
				}
			}
		}
	}
}

func referenceEditDistance(left, right []rune) int {
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range left {
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range right {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[rightIndex+1] = min(
				min(current[rightIndex]+1, previous[rightIndex+1]+1),
				previous[rightIndex]+cost,
			)
		}
		previous, current = current, previous
	}

	return previous[len(right)]
}

func TestRuntimeOptionGroupsAndArgvAcceptExactBounds(t *testing.T) {
	t.Parallel()

	a, b := new(int), new(int)
	for _, test := range []struct {
		name   string
		group  optionGroupSpec
		values map[any]resolvedValue
		want   bool
	}{
		{name: "exclusive empty", group: optionGroupSpec{kind: optionGroupExclusive, bindings: []any{a, b}}},
		{name: "exclusive one", group: optionGroupSpec{kind: optionGroupExclusive, bindings: []any{a, b}}, values: map[any]resolvedValue{a: {state: ValueExplicit}}},
		{name: "exclusive two", group: optionGroupSpec{kind: optionGroupExclusive, bindings: []any{a, b}}, values: map[any]resolvedValue{a: {state: ValueExplicit}, b: {state: ValueExplicit}}, want: true},
		{name: "together empty", group: optionGroupSpec{kind: optionGroupTogether, bindings: []any{a, b}}},
		{name: "together partial", group: optionGroupSpec{kind: optionGroupTogether, bindings: []any{a, b}}, values: map[any]resolvedValue{a: {state: ValueExplicit}}, want: true},
		{name: "together all", group: optionGroupSpec{kind: optionGroupTogether, bindings: []any{a, b}}, values: map[any]resolvedValue{a: {state: ValueExplicit}, b: {state: ValueExplicit}}},
	} {
		err := validateOptionGroups([]optionGroupSpec{test.group}, test.values)
		if errors.Is(err, ErrUsage) != test.want {
			t.Fatalf("%s error = %v, want usage = %v", test.name, err, test.want)
		}
	}

	limits := Limits{MaximumArguments: 2, MaximumArgvBytes: 5}
	if err := validateArgv([]string{"ab", "cde"}, limits); err != nil {
		t.Fatalf("validateArgv(exact limits) error = %v", err)
	}
	if err := validateArgv([]string{"a", "b", "c"}, limits); !errors.Is(err, ErrUsage) {
		t.Fatalf("validateArgv(argument overflow) error = %v", err)
	}
	if err := validateArgv([]string{"ab", "cdef"}, limits); !errors.Is(err, ErrUsage) {
		t.Fatalf("validateArgv(byte overflow) error = %v", err)
	}
}

func TestFailureResultAndContextCausePreserveExactState(t *testing.T) {
	t.Parallel()

	failure := errors.New("failure")
	if result := failureResult(nil, failure); result.Command.command != nil || !errors.Is(result.Err, failure) {
		t.Fatalf("failureResult(nil) = %#v", result)
	}
	command := &compiledCommand{name: "tool"}
	if result := failureResult(command, failure); result.Command.command != command || !errors.Is(result.Err, failure) {
		t.Fatalf("failureResult(command) = %#v", result)
	}
	cause := errors.New("cancel cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if err := contextError(ctx); !errors.Is(err, cause) || !errors.Is(err, ErrCanceled) {
		t.Fatalf("contextError(cause) = %v", err)
	}
}
