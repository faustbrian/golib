package breaker

import "github.com/faustbrian/golib/pkg/circuit-breaker/window"

func openingDecision(
	rules OpeningRules,
	minimumThroughput int,
	consecutiveFailures uint64,
	snapshot window.Snapshot,
) bool {
	if snapshot.Classified < uint64(minimumThroughput) {
		return false
	}

	decisions := make([]bool, 0, 5)
	if rules.ConsecutiveFailures > 0 {
		decisions = append(decisions, consecutiveFailures >= rules.ConsecutiveFailures)
	}
	if rules.FailureCount > 0 {
		decisions = append(decisions, snapshot.Failures >= rules.FailureCount)
	}
	if rules.FailureRatio > 0 {
		decisions = append(decisions, ratio(snapshot.Failures, snapshot.Classified) >= rules.FailureRatio)
	}
	slow := snapshot.SlowSuccess + snapshot.SlowFailure
	if rules.SlowCount > 0 {
		decisions = append(decisions, slow >= rules.SlowCount)
	}
	if rules.SlowRatio > 0 {
		decisions = append(decisions, ratio(slow, snapshot.Classified) >= rules.SlowRatio)
	}

	if rules.Combination == OpenWhenAll {
		for _, decision := range decisions {
			if !decision {
				return false
			}
		}
		return len(decisions) > 0
	}
	for _, decision := range decisions {
		if decision {
			return true
		}
	}
	return false
}

func ratio(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
