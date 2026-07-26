package objective_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/objective"
)

func FuzzObjectiveComparison(f *testing.F) {
	f.Add(int64(1), int64(10), int64(2), int64(0), "a", "b")
	f.Fuzz(func(t *testing.T, leftPrimary, leftSecondary, rightPrimary, rightSecondary int64, leftTie, rightTie string) {
		if len(leftTie) > 1_024 || len(rightTie) > 1_024 {
			return
		}
		goal, err := objective.New(
			objective.Minimize(objective.ContainerCount),
			objective.Maximize(objective.PackedPriority),
		)
		if err != nil {
			t.Fatal(err)
		}
		left := objective.Score{Values: []int64{leftPrimary, leftSecondary}, TieBreak: leftTie}
		right := objective.Score{Values: []int64{rightPrimary, rightSecondary}, TieBreak: rightTie}
		forward, err := goal.Compare(left, right)
		if err != nil {
			t.Fatal(err)
		}
		reverse, err := goal.Compare(right, left)
		if err != nil || forward != -reverse {
			t.Fatalf("comparison is not antisymmetric: %d %d %v", forward, reverse, err)
		}
		if leftPrimary < rightPrimary && forward >= 0 || leftPrimary > rightPrimary && forward <= 0 {
			t.Fatal("secondary score overrode the primary criterion")
		}
	})
}
