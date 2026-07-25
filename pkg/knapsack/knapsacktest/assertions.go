// Package knapsacktest provides solver-independent assertions for consumers,
// fixtures, examples, differential adapters, and benchmarks.
package knapsacktest

import (
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

// RequireVerified fails the test when independent verification rejects plan.
func RequireVerified(t testing.TB, request knapsack.NormalizedRequest, plan knapsack.Plan, options verify.Options) {
	t.Helper()
	result := verify.Plan(request, plan, options)
	if !result.Valid() {
		t.Fatalf("plan verification failed: %+v", result.Violations())
	}
}

// RequireCanonicalEqual fails the test unless both plans have identical
// deterministic canonical encodings.
func RequireCanonicalEqual(t testing.TB, left, right knapsack.Plan) {
	t.Helper()
	if left.CanonicalString() != right.CanonicalString() {
		t.Fatalf("plans differ\nleft:  %s\nright: %s", left.CanonicalString(), right.CanonicalString())
	}
}
