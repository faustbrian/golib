package authorization

import (
	"errors"
	"testing"
)

func TestCombineDenyOverrides(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		decisions []Decision
		want      Outcome
	}{
		"empty is not applicable": {
			want: NotApplicable,
		},
		"not applicable remains not applicable": {
			decisions: []Decision{{Outcome: NotApplicable}},
			want:      NotApplicable,
		},
		"allow applies": {
			decisions: []Decision{{Outcome: NotApplicable}, {Outcome: Allow}},
			want:      Allow,
		},
		"deny overrides allow": {
			decisions: []Decision{{Outcome: Allow}, {Outcome: Deny}},
			want:      Deny,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Combine(DenyOverrides, tt.decisions)
			if err != nil {
				t.Fatalf("Combine() error = %v", err)
			}

			if got.Outcome != tt.want {
				t.Errorf("Combine() outcome = %v, want %v", got.Outcome, tt.want)
			}
		})
	}
}

func TestCombineAllowOverrides(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		decisions []Decision
		want      Outcome
	}{
		"empty is not applicable": {
			want: NotApplicable,
		},
		"not applicable remains not applicable": {
			decisions: []Decision{{Outcome: NotApplicable}},
			want:      NotApplicable,
		},
		"deny applies": {
			decisions: []Decision{{Outcome: NotApplicable}, {Outcome: Deny}},
			want:      Deny,
		},
		"allow overrides deny": {
			decisions: []Decision{{Outcome: Deny}, {Outcome: Allow}},
			want:      Allow,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Combine(AllowOverrides, tt.decisions)
			if err != nil {
				t.Fatalf("Combine() error = %v", err)
			}

			if got.Outcome != tt.want {
				t.Errorf("Combine() outcome = %v, want %v", got.Outcome, tt.want)
			}
		})
	}
}

func TestCombineOrderedAlgorithms(t *testing.T) {
	t.Parallel()

	for _, algorithm := range []CombiningAlgorithm{FirstApplicable, PriorityOrder} {
		algorithm := algorithm
		t.Run(algorithm.String(), func(t *testing.T) {
			t.Parallel()

			got, err := Combine(algorithm, []Decision{
				{Outcome: NotApplicable},
				{Outcome: Allow},
				{Outcome: Deny},
			})
			if err != nil {
				t.Fatalf("Combine() error = %v", err)
			}

			if got.Outcome != Allow {
				t.Errorf("Combine() outcome = %v, want %v", got.Outcome, Allow)
			}
		})
	}
}

func TestCombineRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		algorithm CombiningAlgorithm
		decisions []Decision
		want      error
	}{
		"algorithm": {
			algorithm: CombiningAlgorithm(255),
			want:      ErrInvalidCombiningAlgorithm,
		},
		"outcome": {
			algorithm: DenyOverrides,
			decisions: []Decision{{Outcome: Outcome(255)}},
			want:      ErrInvalidOutcome,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Combine(tt.algorithm, tt.decisions)
			if !errors.Is(err, tt.want) {
				t.Errorf("Combine() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCombiningAlgorithmString(t *testing.T) {
	t.Parallel()

	tests := map[CombiningAlgorithm]string{
		DenyOverrides:           "deny-overrides",
		AllowOverrides:          "allow-overrides",
		FirstApplicable:         "first-applicable",
		PriorityOrder:           "priority-order",
		CombiningAlgorithm(255): "unknown",
	}

	for algorithm, want := range tests {
		if got := algorithm.String(); got != want {
			t.Errorf("CombiningAlgorithm(%d).String() = %q, want %q", algorithm, got, want)
		}
	}
}

func TestOutcomeString(t *testing.T) {
	t.Parallel()

	tests := map[Outcome]string{
		NotApplicable: "not-applicable",
		Allow:         "allow",
		Deny:          "deny",
		Outcome(99):   "unknown",
	}
	for outcome, want := range tests {
		if got := outcome.String(); got != want {
			t.Errorf("Outcome(%d).String() = %q, want %q", outcome, got, want)
		}
	}
}

func TestCombiningAlgorithmsExhaustiveTruthTables(t *testing.T) {
	t.Parallel()

	algorithms := []CombiningAlgorithm{
		DenyOverrides,
		AllowOverrides,
		FirstApplicable,
		PriorityOrder,
	}
	for length := 0; length <= 4; length++ {
		for _, outcomes := range outcomeSequences(length) {
			decisions := make([]Decision, len(outcomes))
			for index, outcome := range outcomes {
				decisions[index] = Decision{Outcome: outcome}
			}

			for _, algorithm := range algorithms {
				got, err := Combine(algorithm, decisions)
				if err != nil {
					t.Fatalf("Combine(%s, %v) error = %v", algorithm, outcomes, err)
				}
				want := referenceOutcome(algorithm, outcomes)
				if got.Outcome != want {
					t.Errorf("Combine(%s, %v) = %v, want %v", algorithm, outcomes, got.Outcome, want)
				}
			}
		}
	}
}

func outcomeSequences(length int) [][]Outcome {
	if length == 0 {
		return [][]Outcome{{}}
	}
	shorter := outcomeSequences(length - 1)
	sequences := make([][]Outcome, 0, len(shorter)*3)
	for _, sequence := range shorter {
		for _, outcome := range []Outcome{NotApplicable, Allow, Deny} {
			next := append(append([]Outcome(nil), sequence...), outcome)
			sequences = append(sequences, next)
		}
	}
	return sequences
}

func referenceOutcome(algorithm CombiningAlgorithm, outcomes []Outcome) Outcome {
	switch algorithm {
	case DenyOverrides:
		result := NotApplicable
		for _, outcome := range outcomes {
			if outcome == Deny {
				return Deny
			}
			if outcome == Allow {
				result = Allow
			}
		}
		return result
	case AllowOverrides:
		result := NotApplicable
		for _, outcome := range outcomes {
			if outcome == Allow {
				return Allow
			}
			if outcome == Deny {
				result = Deny
			}
		}
		return result
	case FirstApplicable, PriorityOrder:
		for _, outcome := range outcomes {
			if outcome != NotApplicable {
				return outcome
			}
		}
		return NotApplicable
	default:
		panic("unsupported test algorithm")
	}
}
