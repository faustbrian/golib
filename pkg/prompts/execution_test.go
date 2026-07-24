package prompts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestRunHeadlessResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  prompts.TextConfig
		policy  prompts.InteractionPolicy
		want    string
		wantErr error
	}{
		{
			name: "explicit default permitted",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessUseDefault,
				Default: prompts.Some("default-name"),
			},
			policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly, PermitDefaults: true},
			want:   "default-name",
		},
		{
			name: "default permission absent",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessUseDefault,
				Default: prompts.Some("default-name"),
			},
			policy:  prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
			wantErr: prompts.ErrInteractionNotPermitted,
		},
		{
			name: "default absent",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessUseDefault,
			},
			policy:  prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly, PermitDefaults: true},
			wantErr: prompts.ErrInteractionNotPermitted,
		},
		{
			name: "explicit fallback",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
				Fallback: prompts.Some("batch-name"),
			},
			policy: prompts.InteractionPolicy{Mode: prompts.InteractivePreferred},
			want:   "batch-name",
		},
		{
			name: "fallback absent",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
			},
			policy:  prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
			wantErr: prompts.ErrInteractionNotPermitted,
		},
		{
			name: "invalid headless behavior",
			config: prompts.TextConfig{
				ID: "name", Label: "Name", Headless: prompts.HeadlessBehavior(200),
			},
			policy:  prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
			wantErr: prompts.ErrUnsupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			prompt := newTextPrompt(t, test.config)
			got, err := prompts.Run(context.Background(), prompt, prompts.Execution{Policy: test.policy})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Run() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunInteractionPolicyMatrix(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("batch-name"),
	})
	terminal := prompts.Capabilities{InputTerminal: true, OutputTerminal: true}

	tests := []struct {
		name         string
		policy       prompts.InteractionPolicy
		capabilities prompts.Capabilities
		want         string
		wantErr      error
	}{
		{"required without permission", prompts.InteractionPolicy{Mode: prompts.InteractiveRequired}, terminal, "", prompts.ErrInteractionNotPermitted},
		{"required without input terminal", prompts.InteractionPolicy{Mode: prompts.InteractiveRequired, PermitInteraction: true}, prompts.Capabilities{OutputTerminal: true}, "", prompts.ErrTerminalUnavailable},
		{"required interactive", prompts.InteractionPolicy{Mode: prompts.InteractiveRequired, PermitInteraction: true}, terminal, "", prompts.ErrTerminalUnavailable},
		{"preferred interactive", prompts.InteractionPolicy{Mode: prompts.InteractivePreferred, PermitInteraction: true}, terminal, "", prompts.ErrTerminalUnavailable},
		{"preferred fallback", prompts.InteractionPolicy{Mode: prompts.InteractivePreferred, PermitInteraction: true}, prompts.Capabilities{}, "batch-name", nil},
		{"auto lacks permission", prompts.InteractionPolicy{Mode: prompts.AutoDetect}, terminal, "batch-name", nil},
		{"auto requires input", prompts.InteractionPolicy{Mode: prompts.AutoDetect, PermitInteraction: true, Auto: prompts.AutoRules{RequireInputTerminal: true}}, prompts.Capabilities{OutputTerminal: true}, "batch-name", nil},
		{"auto requires output", prompts.InteractionPolicy{Mode: prompts.AutoDetect, PermitInteraction: true, Auto: prompts.AutoRules{RequireOutputTerminal: true}}, prompts.Capabilities{InputTerminal: true}, "batch-name", nil},
		{"auto caller permits detected terminal", prompts.InteractionPolicy{Mode: prompts.AutoDetect, PermitInteraction: true, Auto: prompts.AutoRules{RequireInputTerminal: true, RequireOutputTerminal: true}}, terminal, "", prompts.ErrTerminalUnavailable},
		{"invalid mode", prompts.InteractionPolicy{Mode: prompts.InteractionMode(200)}, terminal, "", prompts.ErrUnsupported},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := prompts.Run(context.Background(), prompt, prompts.Execution{
				Policy: test.policy, Capabilities: test.capabilities,
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Run() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunRejectsNilAndExpiredContexts(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	var nilContext context.Context

	_, err := prompts.Run(nilContext, prompt, prompts.Execution{})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context error = %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err = prompts.Run(ctx, prompt, prompts.Execution{})
	if !errors.Is(err, prompts.ErrDeadlineExceeded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}
