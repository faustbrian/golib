package prompts_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func BenchmarkFirstSemanticRender(benchmark *testing.B) {
	frame := prompts.NewFrame(
		prompts.Line(prompts.Text(prompts.RoleLabel, "Environment")),
		prompts.Line(prompts.Text(prompts.RoleFocus, "Production")),
		prompts.Line(prompts.Text(prompts.RoleHint, "Remote deployment target")),
	)
	renderer := prompts.ANSIRenderer{}
	options := prompts.RenderOptions{Width: 80, Color: prompts.ColorANSI256}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		if _, err := renderer.Render(frame, options); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkInteractiveTextEditing(benchmark *testing.B) {
	prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		terminal := prompts.NewVirtualTerminal(80, 24)
		terminal.Push(prompts.PasteEvent("emoji 👩‍💻 and combining e\u0301"),
			prompts.KeyEvent(prompts.KeyWordLeft), prompts.RuneEvent('X'),
			prompts.KeyEvent(prompts.KeyEnter))
		execution := interactiveExecution(terminal)
		execution.Output = io.Discard
		if _, err := prompts.Run(context.Background(), prompt, execution); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkInteractiveTextEditingMaximumBound(benchmark *testing.B) {
	const maximum = 64 << 10
	prompt, err := prompts.NewText(prompts.TextConfig{ID: "payload", Label: "Payload"})
	if err != nil {
		benchmark.Fatal(err)
	}
	input := strings.Repeat("x", maximum)
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		terminal := prompts.NewVirtualTerminal(80, 24)
		terminal.Push(prompts.PasteEvent(input), prompts.KeyEvent(prompts.KeyEnter))
		execution := interactiveExecution(terminal)
		execution.Output = io.Discard
		execution.Limits = prompts.InputLimits{MaxPasteBytes: maximum, MaxInputBytes: maximum}
		if _, err := prompts.Run(context.Background(), prompt, execution); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkSearchLargeOptionSet(benchmark *testing.B) {
	options := benchmarkOptions(benchmark, 10_000)
	policy := prompts.SearchPolicy{MaxOptions: 10_000, MaxResults: 50, MaxQueryRunes: 64}
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for benchmark.Loop() {
		if _, err := prompts.Search(options, "option 099", policy); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkInteractiveSearchNavigationPagination(benchmark *testing.B) {
	options := benchmarkOptions(benchmark, 1_000)
	prompt, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[int]{
		Select: prompts.SelectConfig[int]{
			ID: "option", Label: "Option", Options: options, MaxOptions: len(options),
		},
		Search: prompts.SearchPolicy{
			MaxOptions: len(options), MaxResults: 100, MaxQueryRunes: 64,
		},
	})
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for benchmark.Loop() {
		terminal := prompts.NewVirtualTerminal(40, 6)
		terminal.Push(
			prompts.PasteEvent("option 09"), prompts.KeyEvent(prompts.KeyPageDown),
			prompts.ResizeEvent(32, 5), prompts.KeyEvent(prompts.KeyEnter),
		)
		execution := interactiveExecution(terminal)
		execution.Output = io.Discard
		if _, err := prompts.Run(context.Background(), prompt, execution); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkFormValidationAndTransitions(benchmark *testing.B) {
	name, err := prompts.NewText(prompts.TextConfig{
		ID: "name", Label: "Name",
		PostValidate: []prompts.Validator[string]{func(
			_ context.Context, value string, _ prompts.ValidationContext,
		) error {
			if value == "" {
				return prompts.NewValidationIssue("required", "Name is required", "name")
			}

			return nil
		}},
	})
	if err != nil {
		benchmark.Fatal(err)
	}
	count, err := prompts.NewInteger(prompts.IntegerConfig{ID: "count", Label: "Count"})
	if err != nil {
		benchmark.Fatal(err)
	}
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "setup", Fields: []prompts.FormField{prompts.AsField(name), prompts.AsField(count)},
	})
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		terminal := prompts.NewVirtualTerminal(80, 24)
		terminal.Push(
			prompts.PasteEvent("Ada"), prompts.KeyEvent(prompts.KeyTab),
			prompts.PasteEvent("42"), prompts.KeyEvent(prompts.KeyEnter),
		)
		execution := interactiveExecution(terminal)
		execution.Output = io.Discard
		if _, err := prompts.RunForm(context.Background(), form, execution); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkSemanticRenderProfiles(benchmark *testing.B) {
	frame := prompts.NewFrame(
		prompts.Line(prompts.Text(prompts.RoleLabel, "Environment")),
		prompts.Line(prompts.Text(prompts.RoleFocus, "Production 界")),
		prompts.Line(prompts.Text(prompts.RoleHint, "Remote deployment target")),
	)
	for _, test := range []struct {
		name     string
		renderer prompts.Renderer
		options  prompts.RenderOptions
	}{
		{"plain", prompts.PlainRenderer{}, prompts.RenderOptions{Width: 80}},
		{"no-color", prompts.ANSIRenderer{}, prompts.RenderOptions{Width: 80}},
		{"ansi-256", prompts.ANSIRenderer{}, prompts.RenderOptions{Width: 80, Color: prompts.ColorANSI256}},
		{"true-color", prompts.ANSIRenderer{}, prompts.RenderOptions{Width: 80, Color: prompts.ColorTrueColor}},
		{"redirected-ascii", prompts.PlainRenderer{}, prompts.RenderOptions{ASCIIOnly: true}},
	} {
		benchmark.Run(test.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				if _, err := test.renderer.Render(frame, test.options); err != nil {
					benchmark.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkInteractiveCancellationCleanup(benchmark *testing.B) {
	prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		ctx, cancel := context.WithCancel(context.Background())
		terminal := prompts.NewVirtualTerminal(80, 24)
		execution := interactiveExecution(terminal)
		execution.Output = io.Discard
		execution.Events = eventSourceFunc(func(context.Context) (prompts.InputEvent, error) {
			cancel()

			return prompts.InputEvent{}, context.Canceled
		})
		_, err := prompts.Run(ctx, prompt, execution)
		if !errors.Is(err, prompts.ErrCanceled) || !terminal.Released() {
			benchmark.Fatalf("Run() error = %v, released = %v", err, terminal.Released())
		}
	}
}

func BenchmarkProgressUpdateAndRender(benchmark *testing.B) {
	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "items", Label: "Items", AllowRegression: true,
	})
	if err != nil {
		benchmark.Fatal(err)
	}
	execution := prompts.Execution{Output: io.Discard}
	benchmark.ReportAllocs()
	for index := 0; benchmark.Loop(); index++ {
		if err := progress.Update(int64(index), "working"); err != nil {
			benchmark.Fatal(err)
		}
		if err := progress.Render(context.Background(), execution); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkProgressUpdateCoalescing(benchmark *testing.B) {
	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "items", Label: "Items", AllowRegression: true,
	})
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		for value := range int64(1_000) {
			if err := progress.Update(value, "working"); err != nil {
				benchmark.Fatal(err)
			}
		}
		_ = progress.Snapshot()
	}
}

func benchmarkOptions(testingContext testing.TB, count int) []prompts.Option[int] {
	testingContext.Helper()
	options := make([]prompts.Option[int], count)
	for index := range options {
		option, err := prompts.NewOption(prompts.OptionConfig[int]{
			ID: fmt.Sprintf("option-%05d", index), Label: fmt.Sprintf("Option %05d", index),
			Description: "deterministic benchmark option", Value: index,
		})
		if err != nil {
			testingContext.Fatal(err)
		}
		options[index] = option
	}

	return options
}
