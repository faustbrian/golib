package authstate

import (
	"context"
	"slices"
	"testing"
)

type sortBudgetContext struct {
	context.Context
	successfulChecks int
}

func (ctx *sortBudgetContext) Err() error {
	if ctx.successfulChecks == 0 {
		return context.Canceled
	}
	ctx.successfulChecks--

	return nil
}

func TestTreeProofIndexSortMatchesStableReference(t *testing.T) {
	t.Parallel()

	tail := []int{3, 4, 1, 2}
	if err := sortTreeProofValues(
		&sortBudgetContext{
			Context:          context.Background(),
			successfulChecks: 4_096,
		},
		tail,
		func(left, right int) int {
			return left - right
		},
	); err != nil {
		t.Fatalf("right-exhausted merge tail: %v", err)
	}
	if !slices.Equal(tail, []int{1, 2, 3, 4}) {
		t.Fatalf("right-exhausted merge tail = %v, want [1 2 3 4]", tail)
	}

	type ordered struct {
		group int
		order int
	}
	for size := 2; size <= 32; size++ {
		got := make([]ordered, size)
		for index := range got {
			got[index] = ordered{group: (index*7 + size) % 5, order: index}
		}
		want := slices.Clone(got)
		slices.SortStableFunc(want, func(left, right ordered) int {
			return left.group - right.group
		})
		ctx := &sortBudgetContext{
			Context:          context.Background(),
			successfulChecks: 4_096,
		}
		if err := sortTreeProofValues(
			ctx,
			got,
			func(left, right ordered) int {
				return left.group - right.group
			},
		); err != nil {
			t.Fatalf("stable sort size %d: %v", size, err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("stable sort size %d = %#v, want %#v", size, got, want)
		}
	}
}
