package money

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func TestEqualSplitConservesPositiveAndNegativeTotalsDeterministically(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)

	for _, input := range []string{"10.00", "-10.00"} {
		total, _ := Parse(input, euro, monetaryContext)
		allocation, err := total.EqualSplit(context.Background(), 3)
		if err != nil {
			t.Fatalf("EqualSplit(%s) error = %v", input, err)
		}
		parts := allocation.Parts()
		want := []string{"3.34 EUR", "3.33 EUR", "3.33 EUR"}
		if input[0] == '-' {
			want = []string{"-3.34 EUR", "-3.33 EUR", "-3.33 EUR"}
		}
		for index := range want {
			if parts[index].String() != want[index] {
				t.Errorf("EqualSplit(%s)[%d] = %s, want %s", input, index, parts[index], want[index])
			}
		}
		sum, err := allocation.Sum()
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		equal, err := sum.Equal(total)
		if err != nil || !equal {
			t.Fatalf("allocation sum = %s, want %s", sum, total)
		}
	}

	total, _ := Parse("1.00", euro, monetaryContext)
	if _, err := total.EqualSplit(context.Background(), 0); !errors.Is(err, ErrInvalidAllocation) {
		t.Fatalf("EqualSplit(0) error = %v", err)
	}
}

func TestWeightedAllocationUsesStableLargestRemainders(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	total, _ := Parse("10.00", euro, monetaryContext)

	allocation, err := total.Allocate(context.Background(), []integer.Integer{
		integer.New(1), integer.New(2), integer.New(3),
	})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	want := []string{"1.67 EUR", "3.33 EUR", "5.00 EUR"}
	for index, part := range allocation.Parts() {
		if part.String() != want[index] {
			t.Errorf("Allocate()[%d] = %s, want %s", index, part, want[index])
		}
	}

	if _, err := total.Allocate(context.Background(), []integer.Integer{integer.New(1), integer.Zero()}); !errors.Is(err, ErrInvalidAllocation) {
		t.Fatalf("Allocate(zero ratio) error = %v", err)
	}
}
