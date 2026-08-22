package prompts

import (
	"errors"
	"testing"
)

func TestInteractionAllowedRequiresBothTerminals(t *testing.T) {
	t.Parallel()

	policy := InteractionPolicy{Mode: InteractiveRequired, PermitInteraction: true}
	for name, capabilities := range
		map[string]Capabilities{
			"input only": {InputTerminal: true},
			"output only": {OutputTerminal: true},
		} {
		t.Run(
			name,
			func(t *testing.T) {
				t.Parallel()

				allowed, err := interactionAllowed(policy, capabilities)
				if allowed || !errors.Is(err, ErrTerminalUnavailable) {
					t.Fatalf("interactionAllowed() = %t, %v", allowed, err)
				}
			},
		)
	}
}
