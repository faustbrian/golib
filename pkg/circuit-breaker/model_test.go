package breaker_test

import (
	"context"
	rand "math/rand/v2"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

type referenceModel struct {
	state       breaker.State
	generation  uint64
	now         time.Time
	openUntil   time.Time
	window      []breaker.Outcome
	ignored     uint64
	halfSuccess int
}

func newReferenceModel(now time.Time) *referenceModel {
	return &referenceModel{state: breaker.StateClosed, generation: 1, now: now}
}

func (m *referenceModel) execute(outcome breaker.Outcome) bool {
	if m.state == breaker.StateOpen {
		if m.now.Before(m.openUntil) {
			return false
		}
		m.state = breaker.StateHalfOpen
		m.generation++
		m.halfSuccess = 0
	}
	switch m.state {
	case breaker.StateClosed:
		if outcome == breaker.OutcomeIgnored {
			m.ignored++
			return true
		}
		m.window = append(m.window, outcome)
		if len(m.window) > 5 {
			m.window = m.window[1:]
		}
		failures := 0
		for _, retained := range m.window {
			if retained == breaker.OutcomeFailure {
				failures++
			}
		}
		if len(m.window) >= 3 && float64(failures)/float64(len(m.window)) >= 0.5 {
			m.state = breaker.StateOpen
			m.generation++
			m.openUntil = m.now.Add(10 * time.Second)
		}
	case breaker.StateHalfOpen:
		if outcome == breaker.OutcomeFailure {
			m.state = breaker.StateOpen
			m.generation++
			m.openUntil = m.now.Add(10 * time.Second)
			m.halfSuccess = 0
			return true
		}
		if outcome == breaker.OutcomeSuccess {
			m.halfSuccess++
			if m.halfSuccess == 2 {
				m.state = breaker.StateClosed
				m.generation++
				m.window = nil
				m.ignored = 0
				m.halfSuccess = 0
			}
		}
	}
	return true
}

func (m *referenceModel) reset() {
	m.state = breaker.StateClosed
	m.generation++
	m.window = nil
	m.ignored = 0
	m.halfSuccess = 0
	m.openUntil = time.Time{}
}

func (m *referenceModel) advance(duration time.Duration) { m.now = m.now.Add(duration) }

func (m *referenceModel) counts() (successes, failures uint64) {
	for _, outcome := range m.window {
		if outcome == breaker.OutcomeSuccess {
			successes++
		} else {
			failures++
		}
	}
	return successes, failures
}

func TestRandomizedStateMachineMatchesReferenceModel(t *testing.T) {
	for seed := uint64(1); seed <= 64; seed++ {
		seed := seed
		t.Run("seed", func(t *testing.T) {
			start := time.Unix(100, 0)
			clock := breakertest.NewClock(start)
			actual := mustBreaker(t, breaker.Config{
				Name:              "inventory",
				Clock:             clock,
				Window:            breaker.CountWindow{Size: 5},
				MinimumThroughput: 3,
				Opening:           &breaker.OpeningRules{FailureRatio: 0.5},
				OpenDuration:      breaker.FixedOpenDuration(10 * time.Second),
				HalfOpen: &breaker.HalfOpenPolicy{
					MaxProbes:         2,
					RequiredSuccesses: 2,
				},
			})
			model := newReferenceModel(start)
			random := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))

			for step := 0; step < 500; step++ {
				switch command := random.IntN(10); {
				case command < 6:
					outcome := breaker.Outcome(random.IntN(3))
					modelAdmitted := model.execute(outcome)
					permit, err := actual.Acquire(context.Background())
					actualAdmitted := err == nil
					if actualAdmitted != modelAdmitted {
						t.Fatalf("seed %d step %d admission = %t, model %t, error %v", seed, step, actualAdmitted, modelAdmitted, err)
					}
					if actualAdmitted {
						if err := permit.Complete(outcome, false); err != nil {
							t.Fatalf("seed %d step %d Complete() error = %v", seed, step, err)
						}
					}
				case command < 9:
					duration := time.Duration(random.IntN(15)+1) * time.Second
					clock.Advance(duration)
					model.advance(duration)
				default:
					if err := actual.Reset(); err != nil {
						t.Fatalf("Reset() error = %v", err)
					}
					model.reset()
				}

				snapshot := actual.Snapshot()
				successes, failures := model.counts()
				if snapshot.State != model.state || snapshot.Generation != model.generation ||
					snapshot.Successes != successes || snapshot.Failures != failures ||
					snapshot.Ignored != model.ignored {
					t.Fatalf("seed %d step %d snapshot = %+v, model = %+v", seed, step, snapshot, model)
				}
			}
		})
	}
}
