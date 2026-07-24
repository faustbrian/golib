package prompts_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestAllocationBudgets(t *testing.T) {
	t.Run("first semantic render", func(t *testing.T) {
		frame := prompts.NewFrame(
			prompts.Line(prompts.Text(prompts.RoleLabel, "Environment")),
			prompts.Line(prompts.Text(prompts.RoleFocus, "Production")),
			prompts.Line(prompts.Text(prompts.RoleHint, "Remote deployment target")),
		)
		renderer := prompts.ANSIRenderer{}
		assertAllocationBudget(t, 55, func() {
			if _, err := renderer.Render(frame, prompts.RenderOptions{
				Width: 80, Color: prompts.ColorANSI256,
			}); err != nil {
				panic(err)
			}
		})
	})

	t.Run("interactive text editing", func(t *testing.T) {
		prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
		if err != nil {
			t.Fatal(err)
		}
		assertAllocationBudget(t, 150, func() {
			terminal := prompts.NewVirtualTerminal(80, 24)
			terminal.Push(
				prompts.PasteEvent("emoji 👩‍💻 and combining e\u0301"),
				prompts.KeyEvent(prompts.KeyWordLeft), prompts.RuneEvent('X'),
				prompts.KeyEvent(prompts.KeyEnter),
			)
			execution := interactiveExecution(terminal)
			execution.Output = io.Discard
			if _, err := prompts.Run(context.Background(), prompt, execution); err != nil {
				panic(err)
			}
		})
	})

	t.Run("large option search", func(t *testing.T) {
		options := make([]prompts.Option[int], 10_000)
		for index := range options {
			option, err := prompts.NewOption(prompts.OptionConfig[int]{
				ID:    fmt.Sprintf("option-%05d", index),
				Label: fmt.Sprintf("Option %05d", index), Value: index,
			})
			if err != nil {
				t.Fatal(err)
			}
			options[index] = option
		}
		assertAllocationBudget(t, 55_000, func() {
			if _, err := prompts.Search(options, "option 099", prompts.SearchPolicy{
				MaxOptions: 10_000, MaxResults: 50, MaxQueryRunes: 64,
			}); err != nil {
				panic(err)
			}
		})
	})

	t.Run("progress update and render", func(t *testing.T) {
		progress, err := prompts.NewProgress(prompts.ProgressConfig{
			ID: "items", Label: "Items", AllowRegression: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		value := int64(0)
		assertAllocationBudget(t, 40, func() {
			value++
			if err := progress.Update(value, "working"); err != nil {
				panic(err)
			}
			if err := progress.Render(context.Background(), prompts.Execution{
				Output: io.Discard,
			}); err != nil {
				panic(err)
			}
		})
	})

	t.Run("interactive search pagination", func(t *testing.T) {
		options := benchmarkOptions(t, 1_000)
		prompt, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[int]{
			Select: prompts.SelectConfig[int]{
				ID: "option", Label: "Option", Options: options, MaxOptions: len(options),
			},
			Search: prompts.SearchPolicy{
				MaxOptions: len(options), MaxResults: 100, MaxQueryRunes: 64,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertAllocationBudget(t, 10_500, func() {
			terminal := prompts.NewVirtualTerminal(40, 6)
			terminal.Push(
				prompts.PasteEvent("option 09"), prompts.KeyEvent(prompts.KeyPageDown),
				prompts.ResizeEvent(32, 5), prompts.KeyEvent(prompts.KeyEnter),
			)
			execution := interactiveExecution(terminal)
			execution.Output = io.Discard
			if _, err := prompts.Run(context.Background(), prompt, execution); err != nil {
				panic(err)
			}
		})
	})

	t.Run("form validation transitions", func(t *testing.T) {
		name, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
		if err != nil {
			t.Fatal(err)
		}
		count, err := prompts.NewInteger(prompts.IntegerConfig{ID: "count", Label: "Count"})
		if err != nil {
			t.Fatal(err)
		}
		form, err := prompts.NewForm(prompts.FormConfig{
			ID: "setup", Fields: []prompts.FormField{prompts.AsField(name), prompts.AsField(count)},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertAllocationBudget(t, 150, func() {
			terminal := prompts.NewVirtualTerminal(80, 24)
			terminal.Push(
				prompts.PasteEvent("Ada"), prompts.KeyEvent(prompts.KeyTab),
				prompts.PasteEvent("42"), prompts.KeyEvent(prompts.KeyEnter),
			)
			execution := interactiveExecution(terminal)
			execution.Output = io.Discard
			if _, err := prompts.RunForm(context.Background(), form, execution); err != nil {
				panic(err)
			}
		})
	})

	t.Run("cancellation cleanup", func(t *testing.T) {
		prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
		if err != nil {
			t.Fatal(err)
		}
		assertAllocationBudget(t, 40, func() {
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
				panic("interactive cancellation did not clean up")
			}
		})
	})
}

func assertAllocationBudget(t *testing.T, maximum float64, operation func()) {
	t.Helper()
	allocations := testing.AllocsPerRun(25, operation)
	if allocations > maximum {
		t.Fatalf("allocations = %.0f, budget = %.0f", allocations, maximum)
	}
}
