package prompts_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestInteractiveSelectSkipsDisabledOptions(t *testing.T) {
	t.Parallel()

	options := selectionOptions(t)
	prompt, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: options, InitialID: "dev",
		Accessibility: prompts.Accessibility{
			Label: "Deployment environment", Description: "Choose one target",
			TextualHint: "Use arrow keys",
		},
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.KeyEvent(prompts.KeyDown), prompts.KeyEvent(prompts.KeyEnter))
	execution := interactiveExecution(terminal)
	execution.Capabilities.Color = prompts.ColorANSI16
	value, err := prompts.Run(context.Background(), prompt, execution)
	if err != nil || value != "production" {
		t.Fatalf("Run() = %q, %v", value, err)
	}
	output := terminal.Output()
	if !strings.Contains(output, "[disabled] Staging") || !strings.Contains(output, "[Remote] Production") ||
		!strings.Contains(output, "> ") || !strings.Contains(output, "\x1b[") ||
		!strings.Contains(output, "Deployment environment") ||
		!strings.Contains(output, "Choose one target") || !strings.Contains(output, "Use arrow keys") {
		t.Fatalf("select output = %q", output)
	}
}

func TestInteractiveSelectPaginationWrapAndResize(t *testing.T) {
	t.Parallel()

	options := make([]prompts.Option[int], 0, 8)
	for index := range 8 {
		option, err := prompts.NewOption(prompts.OptionConfig[int]{
			ID: string(rune('a' + index)), Label: "Option " + string(rune('A'+index)), Value: index,
		})
		if err != nil {
			t.Fatalf("NewOption() error = %v", err)
		}
		options = append(options, option)
	}
	prompt, err := prompts.NewSelect(prompts.SelectConfig[int]{
		ID: "option", Label: "Option", Options: options, InitialID: "a",
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 4)
	terminal.Push(prompts.KeyEvent(prompts.KeyPageDown), prompts.ResizeEvent(20, 3),
		prompts.KeyEvent(prompts.KeyUp), prompts.KeyEvent(prompts.KeyDown),
		prompts.KeyEvent(prompts.KeyEnter))
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != 3 {
		t.Fatalf("Run() = %d, %v", value, err)
	}
}

func TestInteractiveMultiSelectPreservesDeclarationOrder(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "environment", Label: "Environments", Options: selectionOptions(t),
		InitialIDs: []string{"dev"}, Min: 1, Max: 2,
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.KeyEvent(prompts.KeyDown), prompts.RuneEvent(' '),
		prompts.KeyEvent(prompts.KeyEnter))
	values, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || len(values) != 2 || values[0] != "development" || values[1] != "production" {
		t.Fatalf("Run() = %#v, %v", values, err)
	}
	if !strings.Contains(terminal.Output(), "[x] Development") || !strings.Contains(terminal.Output(), "[x] [Remote] Production") {
		t.Fatalf("multi-select output = %q", terminal.Output())
	}
}

func TestInteractiveMultiSelectEnforcesBoundsWithoutLosingFocus(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "environment", Label: "Environments", Options: selectionOptions(t), Min: 1, Max: 1,
		Retry: prompts.RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.RuneEvent(' '), prompts.KeyEvent(prompts.KeyDown),
		prompts.RuneEvent(' '), prompts.KeyEvent(prompts.KeyEnter))
	values, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || len(values) != 1 || values[0] != "development" {
		t.Fatalf("Run() = %#v, %v", values, err)
	}
	if !strings.Contains(terminal.Output(), "Maximum selections reached") {
		t.Fatalf("bounded multi-select output = %q", terminal.Output())
	}

	empty := prompts.NewVirtualTerminal(40, 8)
	empty.Push(prompts.KeyEvent(prompts.KeyEnter))
	_, err = prompts.Run(context.Background(), prompt, interactiveExecution(empty))
	if !errors.Is(err, prompts.ErrValidationExhausted) {
		t.Fatalf("empty multi-select error = %v", err)
	}
}

func TestInteractiveMultiSelectRunsCallerValidationForBounds(t *testing.T) {
	t.Parallel()

	validationCalls := 0
	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "environment", Label: "Environments", Options: selectionOptions(t),
		Min: 2, Max: 2, Retry: prompts.RetryPolicy{MaxAttempts: 2},
		PostValidate: []prompts.Validator[[]string]{func(_ context.Context, values []string, _ prompts.ValidationContext) error {
			validationCalls++
			if len(values) != 2 {
				return prompts.NewValidationIssue(
					"choose_two", "Select exactly two environments with Space", "environment",
				)
			}

			return nil
		}},
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(80, 8)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnter), prompts.RuneEvent(' '),
		prompts.KeyEvent(prompts.KeyDown), prompts.RuneEvent(' '), prompts.KeyEvent(prompts.KeyEnter))
	values, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || len(values) != 2 {
		t.Fatalf("Run() = %#v, %v", values, err)
	}
	if validationCalls != 2 {
		t.Fatalf("validation calls = %d, want 2", validationCalls)
	}
	if !strings.Contains(terminal.Output(), "Select exactly two environments with Space") {
		t.Fatalf("multi-select output = %q", terminal.Output())
	}
}

func TestInteractiveSearchSelectFiltersAndRanks(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[string]{
		Select: prompts.SelectConfig[string]{
			ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
		},
		Search: prompts.SearchPolicy{MaxOptions: 10, MaxResults: 3, MaxQueryRunes: 8},
	})
	if err != nil {
		t.Fatalf("NewSearchSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.RuneEvent('p'), prompts.RuneEvent('r'), prompts.KeyEvent(prompts.KeyEnter))
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "production" {
		t.Fatalf("Run() = %q, %v", value, err)
	}
	if !strings.Contains(terminal.Output(), "Production") || !strings.Contains(terminal.Output(), "search: pr") {
		t.Fatalf("search output = %q", terminal.Output())
	}
}

func TestInteractiveSelectionTerminalEventsAndFailures(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	tests := []struct {
		name  string
		event prompts.InputEvent
		want  error
	}{
		{"cancel", prompts.KeyEvent(prompts.KeyEscape), prompts.ErrCanceled},
		{"control c", prompts.KeyEvent(prompts.KeyCtrlC), prompts.ErrCanceled},
		{"control d", prompts.KeyEvent(prompts.KeyCtrlD), prompts.ErrEndOfInput},
		{"eof", prompts.InputEvent{Kind: prompts.EventEOF}, prompts.ErrEndOfInput},
		{"detached", prompts.InputEvent{Kind: prompts.EventDetached}, prompts.ErrTerminalDetached},
		{"invalid resize", prompts.ResizeEvent(-1, 2), prompts.ErrReader},
		{"paste without search", prompts.PasteEvent("dev"), prompts.ErrReader},
		{"unknown event", prompts.InputEvent{Kind: prompts.EventKind(200)}, prompts.ErrReader},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			terminal := prompts.NewVirtualTerminal(40, 8)
			terminal.Push(test.event)
			_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
			if !errors.Is(runErr, test.want) || !terminal.Released() {
				t.Fatalf("Run() error = %v, released %v", runErr, terminal.Released())
			}
		})
	}

	for _, sourceErr := range []error{io.EOF, io.ErrUnexpectedEOF} {
		terminal := prompts.NewVirtualTerminal(40, 8)
		execution := interactiveExecution(terminal)
		execution.Events = eventSourceFunc(func(context.Context) (prompts.InputEvent, error) {
			return prompts.InputEvent{}, sourceErr
		})
		_, runErr := prompts.Run(context.Background(), prompt, execution)
		want := prompts.ErrReader
		if errors.Is(sourceErr, io.EOF) {
			want = prompts.ErrEndOfInput
		}
		if !errors.Is(runErr, want) {
			t.Fatalf("source error %v produced %v", sourceErr, runErr)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	terminal := prompts.NewVirtualTerminal(40, 8)
	execution := interactiveExecution(terminal)
	execution.Events = eventSourceFunc(func(context.Context) (prompts.InputEvent, error) {
		cancel()
		return prompts.InputEvent{}, context.Canceled
	})
	_, err = prompts.Run(ctx, prompt, execution)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled source error = %v", err)
	}
}

func TestInteractiveSelectionRenderingAndCallbackFailures(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
		PostValidate: []prompts.Validator[string]{func(context.Context, string, prompts.ValidationContext) error {
			panic("callback")
		}},
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnter))
	_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrAdapter) || !terminal.Released() {
		t.Fatalf("callback failure = %v, released %v", err, terminal.Released())
	}

	plain, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	for _, afterEvent := range []bool{false, true} {
		terminal = prompts.NewVirtualTerminal(40, 8)
		execution := interactiveExecution(terminal)
		calls := 0
		execution.Renderer = rendererFunc(func(prompts.Frame, prompts.RenderOptions) (string, error) {
			calls++
			if !afterEvent || calls == 2 {
				return "", io.ErrClosedPipe
			}
			return "frame\n", nil
		})
		if afterEvent {
			terminal.Push(prompts.KeyEvent(prompts.KeyDown))
		}
		_, runErr := prompts.Run(context.Background(), plain, execution)
		if !errors.Is(runErr, prompts.ErrRenderer) || !terminal.Released() {
			t.Fatalf("renderer failure afterEvent=%v: %v", afterEvent, runErr)
		}
	}

	terminal = prompts.NewVirtualTerminal(40, 8)
	execution := interactiveExecution(terminal)
	execution.Output = &failingWriter{err: io.ErrClosedPipe}
	_, err = prompts.Run(context.Background(), plain, execution)
	if !errors.Is(err, prompts.ErrWriter) || !terminal.Released() {
		t.Fatalf("writer failure = %v, released %v", err, terminal.Released())
	}
}

func TestInteractiveSelectionAppliesCapabilityChanges(t *testing.T) {
	t.Parallel()

	option, err := prompts.NewOption(prompts.OptionConfig[string]{
		ID: "tokyo", Label: "東京", Value: "tokyo",
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "city", Label: "City", Options: []prompts.Option[string]{option},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.CapabilityEvent(prompts.Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: 20, Height: 3,
		}),
		prompts.KeyEvent(prompts.KeyEnter),
	)
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "tokyo" ||
		!strings.Contains(terminal.Output(), "\\u{6771}\\u{4EAC}") {
		t.Fatalf("Run() = %q, %v; output %q", value, err, terminal.Output())
	}

	for name, capabilities := range map[string]prompts.Capabilities{
		"detached": {},
		"invalid": {
			InputTerminal: true, OutputTerminal: true, Height: -1,
		},
	} {
		terminal := prompts.NewVirtualTerminal(80, 24)
		terminal.Push(prompts.CapabilityEvent(capabilities))
		_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		want := prompts.ErrReader
		if name == "detached" {
			want = prompts.ErrTerminalDetached
		}
		if !errors.Is(runErr, want) {
			t.Fatalf("%s capability error = %v", name, runErr)
		}
	}
}

func TestInteractiveSelectionNavigationAndSearchEditing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnd), prompts.KeyEvent(prompts.KeyPageUp),
		prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyTab),
		prompts.KeyEvent(prompts.KeyShiftTab), prompts.KeyEvent(prompts.KeyDown),
		prompts.KeyEvent(prompts.KeyLeft),
		prompts.KeyEvent(prompts.KeyEnter))
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "production" {
		t.Fatalf("navigation Run() = %q, %v", value, err)
	}

	search, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[string]{
		Select: prompts.SelectConfig[string]{
			ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
		},
		Search: prompts.SearchPolicy{MaxOptions: 10, MaxResults: 2, MaxQueryRunes: 3},
	})
	if err != nil {
		t.Fatalf("NewSearchSelect() error = %v", err)
	}
	terminal = prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.PasteEvent("pr"), prompts.KeyEvent(prompts.KeyLeft),
		prompts.KeyEvent(prompts.KeyRight), prompts.KeyEvent(prompts.KeyWordLeft),
		prompts.KeyEvent(prompts.KeyWordRight), prompts.KeyEvent(prompts.KeyBackspace),
		prompts.RuneEvent('r'), prompts.KeyEvent(prompts.KeyEnter))
	value, err = prompts.Run(context.Background(), search, interactiveExecution(terminal))
	if err != nil || value != "production" {
		t.Fatalf("search editing Run() = %q, %v", value, err)
	}

	for _, event := range []prompts.InputEvent{
		prompts.PasteEvent("four"), prompts.PasteEvent(string([]byte{0xff})),
		prompts.PasteEvent("a\n"), prompts.RuneEvent('\n'),
	} {
		terminal = prompts.NewVirtualTerminal(40, 8)
		terminal.Push(event)
		_, runErr := prompts.Run(context.Background(), search, interactiveExecution(terminal))
		if !errors.Is(runErr, prompts.ErrReader) {
			t.Fatalf("invalid search event error = %v", runErr)
		}
	}
	terminal = prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.PasteEvent("abc"), prompts.RuneEvent('d'))
	_, err = prompts.Run(context.Background(), search, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("search query bound error = %v", err)
	}

	terminal = prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.PasteEvent("zzz"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.KeyEvent(prompts.KeyEscape))
	_, err = prompts.Run(context.Background(), search, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrCanceled) || !strings.Contains(terminal.Output(), "No selectable options") {
		t.Fatalf("empty search error = %v, output %q", err, terminal.Output())
	}
}

func TestInteractiveMultiSelectCanDeselect(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "environment", Label: "Environments", Options: selectionOptions(t),
		InitialIDs: []string{"dev", "prod"}, Min: 1, Max: 2,
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.RuneEvent(' '), prompts.KeyEvent(prompts.KeyEnter))
	values, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || len(values) != 1 || values[0] != "production" {
		t.Fatalf("Run() = %#v, %v", values, err)
	}
}

func selectionOptions(t *testing.T) []prompts.Option[string] {
	t.Helper()
	configs := []prompts.OptionConfig[string]{
		{ID: "dev", Label: "Development", Description: "Local work", Value: "development"},
		{ID: "stage", Label: "Staging", Value: "staging", Disabled: true},
		{ID: "prod", Label: "Production", Group: "Remote", Value: "production"},
	}
	options := make([]prompts.Option[string], 0, len(configs))
	for _, config := range configs {
		option, err := prompts.NewOption(config)
		if err != nil {
			t.Fatalf("NewOption() error = %v", err)
		}
		options = append(options, option)
	}
	return options
}
