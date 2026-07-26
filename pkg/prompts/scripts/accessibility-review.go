//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	prompts "github.com/faustbrian/golib/pkg/prompts"
	"github.com/faustbrian/golib/pkg/prompts/terminal"
)

func main() {
	if err := review(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "accessibility review failed:", err)
		os.Exit(1)
	}
}

func review(ctx context.Context) error {
	if err := message(ctx, prompts.MessageInfo, "VoiceOver review",
		"Use only the keyboard. Confirm that every label, hint, error, focused option, selected option, and final status is announced in a useful order."); err != nil {
		return err
	}

	name, err := reviewNamePrompt()
	if err != nil {
		return err
	}
	if _, err := run(ctx, name); err != nil {
		return err
	}

	secret, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "review-secret", Label: "Demonstration secret",
		Description: "Enter disposable text, not a real password or token.",
		Hint:        "Input must not be displayed or repeated after submission.",
		Accessibility: prompts.Accessibility{
			TextualHint: "Type disposable text and press Enter. Confirm that VoiceOver does not announce the entered value.",
		},
		Class: prompts.SecretToken,
	})
	if err != nil {
		return err
	}
	if _, err := run(ctx, secret); err != nil {
		return err
	}
	if err := message(ctx, prompts.MessageSuccess, "Secret accepted", "The secret value was not rendered."); err != nil {
		return err
	}

	options, err := reviewOptions()
	if err != nil {
		return err
	}
	selection, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "review-select", Label: "Deployment region",
		Description: "Use arrow keys to inspect focus, including the disabled option.",
		Hint:        "Choose Helsinki or London and press Enter.",
		Accessibility: prompts.Accessibility{
			TextualHint: "Use Up and Down. Confirm that focused, disabled, and option descriptions are announced.",
		},
		Options: options,
	})
	if err != nil {
		return err
	}
	if _, err := run(ctx, selection); err != nil {
		return err
	}

	multiple, err := reviewMultiSelect(options)
	if err != nil {
		return err
	}
	if _, err := run(ctx, multiple); err != nil {
		return err
	}

	if err := message(ctx, prompts.MessageInfo, "Resize check",
		"Make this terminal narrow before continuing. The next prompt detects the new width without relying on color or cursor animation."); err != nil {
		return err
	}
	resizeReady, err := prompts.NewConfirm(prompts.ConfirmConfig{
		ID: "review-resize", Label: "Narrow terminal ready",
		Hint: "Resize the terminal, then type yes and press Enter.",
		Accessibility: prompts.Accessibility{
			TextualHint: "After making the terminal narrow, type yes and press Enter.",
		},
	})
	if err != nil {
		return err
	}
	if _, err := run(ctx, resizeReady); err != nil {
		return err
	}
	search, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[string]{
		Select: prompts.SelectConfig[string]{
			ID: "review-search", Label: "Search for a city",
			Hint: "Type hel, confirm the filtered result remains understandable, then press Enter.",
			Accessibility: prompts.Accessibility{
				TextualHint: "Type hel and verify that Helsinki is announced as the focused result.",
			},
			Options: options,
		},
		Search: prompts.SearchPolicy{MaxOptions: 10, MaxResults: 5, MaxQueryRunes: 20},
	})
	if err != nil {
		return err
	}
	if _, err := run(ctx, search); err != nil {
		return err
	}

	cancel, err := prompts.NewText(prompts.TextConfig{
		ID: "review-cancel", Label: "Cancellation check",
		Hint: "Press Escape. The review should continue with a cancellation confirmation.",
		Accessibility: prompts.Accessibility{
			TextualHint: "Press Escape without entering a value.",
		},
	})
	if err != nil {
		return err
	}
	if _, err := run(ctx, cancel); !errors.Is(err, prompts.ErrCanceled) {
		return fmt.Errorf("cancellation check: %w", err)
	}
	if err := message(ctx, prompts.MessageWarning, "Cancellation confirmed",
		"The canceled prompt returned control and restored the terminal."); err != nil {
		return err
	}

	progress, err := prompts.NewProgress(prompts.ProgressConfig{
		ID: "review-progress", Label: "Accessible progress", Total: 3,
	})
	if err != nil {
		return err
	}
	if err := progress.Update(1, "started"); err != nil {
		return err
	}
	if err := progress.Render(ctx, outputExecution()); err != nil {
		return err
	}
	if err := progress.Update(3, "complete"); err != nil {
		return err
	}
	progress.Complete("complete")
	if err := progress.Render(ctx, outputExecution()); err != nil {
		return err
	}

	return message(ctx, prompts.MessageSuccess, "Review flow complete",
		"Record announcement order, focus, validation recovery, secret non-disclosure, narrow-width behavior, cancellation, and terminal restoration.")
}

func reviewMultiSelect(options []prompts.Option[string]) (prompts.Prompt[[]string], error) {
	return prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "review-multi", Label: "Preferred regions",
		Description: "Toggle two choices and verify selected state is spoken.",
		Hint:        "Space toggles, arrows move, and Enter submits.",
		Accessibility: prompts.Accessibility{
			TextualHint: "Select exactly two choices with Space, then press Enter.",
		},
		Options: options, Min: 2, Max: 2,
		Retry: prompts.RetryPolicy{Unlimited: true},
		PostValidate: []prompts.Validator[[]string]{func(_ context.Context, values []string, _ prompts.ValidationContext) error {
			if len(values) != 2 {
				return prompts.NewValidationIssue(
					"choose_two", "Select exactly two choices with Space before pressing Enter", "review-multi",
				)
			}

			return nil
		}},
	})
}

func reviewNamePrompt() (prompts.Prompt[string], error) {
	return prompts.NewText(prompts.TextConfig{
		ID: "review-name", Label: "Review name",
		Description: "Submit an empty value once, then enter any two letters.",
		Hint:        "The first submission should announce a validation error.",
		Accessibility: prompts.Accessibility{
			Label: "Reviewer name", Description: "A validation retry demonstration.",
			TextualHint: "Press Enter once while empty, then type two or more letters and press Enter.",
		},
		PostValidate: []prompts.Validator[string]{func(_ context.Context, value string, _ prompts.ValidationContext) error {
			if len(strings.TrimSpace(value)) < 2 {
				return prompts.NewValidationIssue("too_short", "Type at least two letters before pressing Enter", "review-name")
			}
			return nil
		}},
		Retry: prompts.RetryPolicy{Unlimited: true},
	})
}

func run[T any](ctx context.Context, prompt prompts.Prompt[T]) (T, error) {
	adapter, err := terminal.New(os.Stdin, os.Stdout, terminal.Config{})
	if err != nil {
		var zero T
		return zero, err
	}
	capabilities := adapter.Capabilities()
	capabilities.Color = prompts.ColorNone
	capabilities.Animation = false

	return prompts.Run(ctx, prompt, prompts.Execution{
		Input: os.Stdin, Output: os.Stdout, Error: os.Stderr,
		Events: adapter, Terminal: adapter, Capabilities: capabilities,
		Policy: prompts.InteractionPolicy{
			Mode: prompts.InteractiveRequired, PermitInteraction: true,
			PermitUnlimitedRetries: true,
		},
	})
}

func reviewOptions() ([]prompts.Option[string], error) {
	configs := []prompts.OptionConfig[string]{
		{ID: "helsinki", Label: "Helsinki", Description: "Supported primary location", Value: "helsinki"},
		{ID: "london", Label: "London", Description: "Supported secondary location", Value: "london"},
		{ID: "retired", Label: "Retired region", Description: "Unavailable choice", Value: "retired", Disabled: true},
	}
	options := make([]prompts.Option[string], 0, len(configs))
	for _, config := range configs {
		option, err := prompts.NewOption(config)
		if err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, nil
}

func message(ctx context.Context, kind prompts.MessageKind, title, body string) error {
	return prompts.WriteMessage(ctx, prompts.Message{Kind: kind, Title: title, Body: body}, outputExecution())
}

func outputExecution() prompts.Execution {
	return prompts.Execution{
		Output:       os.Stdout,
		Capabilities: prompts.Capabilities{Unicode: true, Color: prompts.ColorNone},
	}
}
