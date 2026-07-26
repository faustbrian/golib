package breaker

import (
	"testing"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func TestOpeningDecisionTruthTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rules       OpeningRules
		minimum     int
		consecutive uint64
		snapshot    window.Snapshot
		want        bool
	}{
		{
			name:        "below minimum throughput",
			rules:       OpeningRules{ConsecutiveFailures: 2},
			minimum:     3,
			consecutive: 2,
			snapshot:    window.Snapshot{Classified: 2, Failures: 2},
			want:        false,
		},
		{
			name:        "consecutive threshold exact",
			rules:       OpeningRules{ConsecutiveFailures: 3},
			minimum:     3,
			consecutive: 3,
			snapshot:    window.Snapshot{Classified: 3, Failures: 3},
			want:        true,
		},
		{
			name:    "failure count exact",
			rules:   OpeningRules{FailureCount: 2},
			minimum: 4,
			snapshot: window.Snapshot{
				Classified: 4,
				Failures:   2,
				Successes:  2,
			},
			want: true,
		},
		{
			name:    "failure ratio below threshold",
			rules:   OpeningRules{FailureRatio: 0.5},
			minimum: 3,
			snapshot: window.Snapshot{
				Classified: 3,
				Failures:   1,
				Successes:  2,
			},
			want: false,
		},
		{
			name:    "failure ratio exact threshold",
			rules:   OpeningRules{FailureRatio: 0.5},
			minimum: 4,
			snapshot: window.Snapshot{
				Classified: 4,
				Failures:   2,
				Successes:  2,
			},
			want: true,
		},
		{
			name:    "slow count combines success and failure",
			rules:   OpeningRules{SlowCount: 2},
			minimum: 3,
			snapshot: window.Snapshot{
				Classified:  3,
				SlowSuccess: 1,
				SlowFailure: 1,
			},
			want: true,
		},
		{
			name:    "slow ratio exact threshold",
			rules:   OpeningRules{SlowRatio: 0.5},
			minimum: 4,
			snapshot: window.Snapshot{
				Classified:  4,
				SlowSuccess: 1,
				SlowFailure: 1,
			},
			want: true,
		},
		{
			name: "any rule",
			rules: OpeningRules{
				FailureCount: 4,
				SlowCount:    1,
				Combination:  OpenWhenAny,
			},
			minimum:  2,
			snapshot: window.Snapshot{Classified: 2, SlowSuccess: 1},
			want:     true,
		},
		{
			name: "all rules require every enabled threshold",
			rules: OpeningRules{
				FailureCount: 1,
				SlowCount:    2,
				Combination:  OpenWhenAll,
			},
			minimum: 2,
			snapshot: window.Snapshot{
				Classified:  2,
				Failures:    1,
				SlowFailure: 1,
			},
			want: false,
		},
		{
			name: "all rules open when every enabled threshold matches",
			rules: OpeningRules{
				FailureCount: 1,
				SlowCount:    1,
				Combination:  OpenWhenAll,
			},
			minimum:  2,
			snapshot: window.Snapshot{Classified: 2, Failures: 1, SlowFailure: 1},
			want:     true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := openingDecision(test.rules, test.minimum, test.consecutive, test.snapshot)
			if got != test.want {
				t.Fatalf("openingDecision() = %t, want %t", got, test.want)
			}
		})
	}
}
