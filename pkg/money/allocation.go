package money

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/faustbrian/golib/pkg/math/integer"
)

// MaxAllocationParts bounds output and remainder-distribution work.
const MaxAllocationParts = 10_000

// MaxRatioDigits bounds each positive integer allocation weight.
const MaxRatioDigits = 64

// AllocationResult is an immutable ordered allocation. Earlier parts receive
// deterministic one-unit remainders.
type AllocationResult struct{ parts []Money }

// Parts returns an independent copy of the ordered allocation.
func (result AllocationResult) Parts() []Money {
	return append([]Money(nil), result.parts...)
}

// Sum returns the conserved total represented by all parts.
func (result AllocationResult) Sum() (Money, error) {
	if len(result.parts) == 0 {
		return Money{}, ErrInvalidAllocation
	}
	total := result.parts[0]
	for _, part := range result.parts[1:] {
		var err error
		total, err = total.Add(part)
		if err != nil {
			return Money{}, err
		}
	}

	return total, nil
}

// EqualSplit divides money into count fixed-context parts and distributes any
// positive or negative minor-unit remainder from the first part onward.
func (money Money) EqualSplit(ctx context.Context, count int) (AllocationResult, error) {
	if !money.Valid() {
		return AllocationResult{}, ErrInvalidMoney
	}
	if ctx == nil {
		return AllocationResult{}, ErrInvalidAllocation
	}
	if err := ctx.Err(); err != nil {
		return AllocationResult{}, err
	}
	if count <= 0 || count > MaxAllocationParts {
		return AllocationResult{}, ErrInvalidAllocation
	}

	total, err := money.MinorUnits()
	if err != nil {
		return AllocationResult{}, err
	}
	quotient, remainder, err := total.QuoRem(ctx, integer.New(int64(count)), arithmeticLimits())
	if err != nil {
		return AllocationResult{}, fmt.Errorf("money: split units: %w", err)
	}
	remainderCount := mustAllocationRemainder(remainder.Abs(), int64(count))
	delta := integer.New(int64(remainder.Sign()))
	parts := make([]Money, count)
	for index := range parts {
		units := quotient
		if int64(index) < remainderCount {
			units, err = units.Add(ctx, delta, arithmeticLimits())
			if err != nil {
				return AllocationResult{}, fmt.Errorf("money: distribute remainder: %w", err)
			}
		}
		parts[index], err = FromMinorUnits(units, money.currency, money.context)
		if err != nil {
			return AllocationResult{}, err
		}
	}

	return AllocationResult{parts: parts}, nil
}

// Allocate apportions money by positive integer ratios using stable largest
// remainders. Equal remainders are resolved by original ratio order.
func (money Money) Allocate(ctx context.Context, ratios []integer.Integer) (AllocationResult, error) {
	if !money.Valid() {
		return AllocationResult{}, ErrInvalidMoney
	}
	if ctx == nil || len(ratios) == 0 || len(ratios) > MaxAllocationParts {
		return AllocationResult{}, ErrInvalidAllocation
	}
	if err := ctx.Err(); err != nil {
		return AllocationResult{}, err
	}

	sum := integer.Zero()
	for _, ratio := range ratios {
		if ratio.Sign() <= 0 || len(strings.TrimPrefix(ratio.String(), "-")) > MaxRatioDigits {
			return AllocationResult{}, ErrInvalidAllocation
		}
		var err error
		sum, err = sum.Add(ctx, ratio, arithmeticLimits())
		if err != nil {
			return AllocationResult{}, fmt.Errorf("money: sum ratios: %w", err)
		}
	}

	total, err := money.MinorUnits()
	if err != nil {
		return AllocationResult{}, err
	}
	absTotal := total.Abs()
	units := make([]integer.Integer, len(ratios))
	remainders := make([]integer.Integer, len(ratios))
	assigned := integer.Zero()
	for index, ratio := range ratios {
		product, multiplyErr := absTotal.Mul(ctx, ratio, arithmeticLimits())
		if multiplyErr != nil {
			return AllocationResult{}, fmt.Errorf("money: weight amount: %w", multiplyErr)
		}
		units[index], remainders[index], err = product.QuoRem(ctx, sum, arithmeticLimits())
		if err != nil {
			return AllocationResult{}, fmt.Errorf("money: apportion amount: %w", err)
		}
		assigned, err = assigned.Add(ctx, units[index], arithmeticLimits())
		if err != nil {
			return AllocationResult{}, fmt.Errorf("money: sum apportioned amount: %w", err)
		}
	}

	remaining, err := absTotal.Sub(ctx, assigned, arithmeticLimits())
	if err != nil {
		return AllocationResult{}, fmt.Errorf("money: allocation remainder: %w", err)
	}
	remainderCount := mustAllocationRemainder(remaining, int64(len(ratios)))
	order := make([]int, len(ratios))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(left, right int) bool {
		return remainders[order[left]].Cmp(remainders[order[right]]) > 0
	})
	for position := int64(0); position < remainderCount; position++ {
		index := order[position]
		units[index], err = units[index].Add(ctx, integer.New(1), arithmeticLimits())
		if err != nil {
			return AllocationResult{}, fmt.Errorf("money: distribute weighted remainder: %w", err)
		}
	}

	parts := make([]Money, len(ratios))
	for index, partUnits := range units {
		if total.Sign() < 0 {
			partUnits = partUnits.Neg()
		}
		parts[index], err = FromMinorUnits(partUnits, money.currency, money.context)
		if err != nil {
			return AllocationResult{}, err
		}
	}

	return AllocationResult{parts: parts}, nil
}

func mustAllocationRemainder(remainder integer.Integer, maximum int64) int64 {
	value, err := remainder.Int64()
	if err != nil || value < 0 || value > maximum {
		panic(invariantViolation)
	}

	return value
}
