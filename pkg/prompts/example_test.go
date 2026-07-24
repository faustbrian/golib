package prompts_test

import (
	"context"
	"fmt"
	"os"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func ExampleParse() {
	prompt, err := prompts.NewInteger(prompts.IntegerConfig{ID: "workers", Label: "Workers"})
	if err != nil {
		panic(err)
	}
	workers, err := prompts.Parse(context.Background(), prompt, "4", nil)
	fmt.Println(workers, err)
	// Output: 4 <nil>
}

func ExampleRun_headlessFallback() {
	prompt, err := prompts.NewText(prompts.TextConfig{
		ID: "region", Label: "Region", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("eu-west-1"),
	})
	if err != nil {
		panic(err)
	}
	region, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	fmt.Println(region, err)
	// Output: eu-west-1 <nil>
}

func ExampleRun_virtualTerminal() {
	prompt, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "continue", Label: "Continue"})
	if err != nil {
		panic(err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	if err := terminal.Push(prompts.PasteEvent("yes"), prompts.KeyEvent(prompts.KeyEnter)); err != nil {
		panic(err)
	}
	answer, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Events: terminal, Terminal: terminal, Output: terminal,
		Capabilities: prompts.Capabilities{InputTerminal: true, OutputTerminal: true, Width: 80, Height: 24},
		Policy:       prompts.InteractionPolicy{Mode: prompts.InteractiveRequired, PermitInteraction: true},
	})
	fmt.Println(answer, err, terminal.Released())
	// Output: true <nil> true
}

func ExampleRunForm() {
	name, err := prompts.NewText(prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("Brian"),
	})
	if err != nil {
		panic(err)
	}
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "profile", Fields: []prompts.FormField{prompts.AsField(name)},
	})
	if err != nil {
		panic(err)
	}
	result, err := prompts.RunForm(context.Background(), form, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	value, ok := prompts.FormValue[string](result, "name")
	fmt.Println(value, ok, err)
	// Output: Brian true <nil>
}

func ExampleProgress() {
	progress, err := prompts.NewProgress(prompts.ProgressConfig{ID: "upload", Label: "Upload", Total: 2})
	if err != nil {
		panic(err)
	}
	if err := progress.Update(2, "sent"); err != nil {
		panic(err)
	}
	progress.Complete("done")
	if err := progress.Render(context.Background(), prompts.Execution{Output: os.Stdout}); err != nil {
		panic(err)
	}
	// Output: success: Upload: 2/2 (100%) - done
}

func ExampleWriteTable() {
	err := prompts.WriteTable(context.Background(), prompts.Table{
		Headers: []string{"Name", "State"}, Rows: [][]string{{"api", "ready"}},
	}, prompts.Execution{Output: os.Stdout})
	if err != nil {
		panic(err)
	}
	// Output:
	// | Name | State |
	// | api  | ready |
}
