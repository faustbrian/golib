package gomoney_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/money"
)

func FuzzCostTypeIdentifiers(f *testing.F) {
	f.Add("box")
	f.Add(" ")
	f.Add(strings.Repeat("x", 33))

	euro, err := currency.Parse("EUR")
	if err != nil {
		f.Fatalf("parse currency: %v", err)
	}
	moneyContext, err := money.DefaultContext(euro)
	if err != nil {
		f.Fatalf("create money context: %v", err)
	}
	cost, err := money.Parse("1.00", euro, moneyContext)
	if err != nil {
		f.Fatalf("parse cost: %v", err)
	}

	f.Fuzz(func(t *testing.T, typeID string) {
		if len(typeID) > 4_096 {
			t.Skip()
		}

		costs, newErr := gomoney.NewWithLimits(
			map[string]money.Money{typeID: cost},
			gomoney.Limits{MaxTypes: 1, MaxIDBytes: 32},
		)
		valid := strings.TrimSpace(typeID) != "" && len(typeID) <= 32
		if !valid {
			if !errors.Is(newErr, gomoney.ErrInvalidCosts) {
				t.Fatalf("NewWithLimits(%q) error = %v", typeID, newErr)
			}

			return
		}
		if newErr != nil {
			t.Fatalf("NewWithLimits(%q) error = %v", typeID, newErr)
		}
		if !costs.Valid() {
			t.Fatalf("NewWithLimits(%q) returned invalid costs", typeID)
		}
	})
}

func FuzzCostMappings(f *testing.F) {
	f.Add("box", "crate", int64(125), uint8(2), uint16(16), false)
	f.Add("duplicate", "duplicate", int64(1), uint8(0), uint16(16), false)
	f.Add("credit", "box", int64(-25), uint8(2), uint16(16), true)

	euro, err := currency.Parse("EUR")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, leftID, rightID string, amount int64, scale uint8, maxIDBytes uint16, allowNegative bool) {
		if len(leftID) > 4_096 || len(rightID) > 4_096 {
			t.Skip()
		}
		scale %= money.MaxScale + 1
		monetaryContext, contextErr := money.CustomContext(scale)
		if contextErr != nil {
			t.Fatal(contextErr)
		}
		value, parseErr := money.Parse(strconv.FormatInt(amount, 10), euro, monetaryContext)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		policy := gomoney.Policy{
			Limits:             gomoney.Limits{MaxTypes: 2, MaxIDBytes: uint32(maxIDBytes%65 + 1)},
			AllowNegativeCosts: allowNegative,
		}
		entries := []gomoney.Entry{{TypeID: leftID, Cost: value}, {TypeID: rightID, Cost: value}}
		first, firstErr := gomoney.NewFromEntries(entries, policy)
		second, secondErr := gomoney.NewFromEntries(entries, policy)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("construction changed outcome: %v then %v", firstErr, secondErr)
		}
		if firstErr != nil {
			if !errors.Is(firstErr, gomoney.ErrInvalidCosts) || !errors.Is(secondErr, gomoney.ErrInvalidCosts) {
				t.Fatalf("construction errors = %v and %v", firstErr, secondErr)
			}
			return
		}
		plan := mustPlan(t, leftID, rightID)
		firstTotal, firstTotalErr := first.Total(plan)
		secondTotal, secondTotalErr := second.Total(plan)
		if firstTotalErr != nil || secondTotalErr != nil {
			t.Fatalf("totals failed: %v and %v", firstTotalErr, secondTotalErr)
		}
		equal, equalErr := firstTotal.Equal(secondTotal)
		if equalErr != nil || !equal {
			t.Fatalf("totals differ: %s and %s (%v)", firstTotal, secondTotal, equalErr)
		}
	})
}

func FuzzSolverCostObjective(f *testing.F) {
	f.Add(int64(60), int64(150), uint8(2), false, false)
	f.Add(int64(-60), int64(-150), uint8(3), true, false)
	f.Add(int64(0), int64(0), uint8(1), false, false)
	f.Add(int64(1<<62), int64(-(1 << 62)), uint8(2), true, true)

	euro, err := currency.Parse("EUR")
	if err != nil {
		f.Fatal(err)
	}
	moneyContext, err := money.CustomContext(2)
	if err != nil {
		f.Fatal(err)
	}
	request := exactMoneyRequest(f)

	f.Fuzz(func(t *testing.T, smallAmount, largeAmount int64, rawMaxTypes uint8, reverse, cancelled bool) {
		small, parseErr := money.Parse(strconv.FormatInt(smallAmount, 10), euro, moneyContext)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		large, parseErr := money.Parse(strconv.FormatInt(largeAmount, 10), euro, moneyContext)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		mapping := make(map[string]money.Money, 2)
		if reverse {
			mapping["large"] = large
			mapping["small"] = small
		} else {
			mapping["small"] = small
			mapping["large"] = large
		}
		maximumTypes := uint32(rawMaxTypes % 4)
		costs, newErr := gomoney.NewWithPolicy(mapping, gomoney.Policy{
			Limits:             gomoney.Limits{MaxTypes: maximumTypes, MaxIDBytes: 5},
			AllowNegativeCosts: true,
		})
		if maximumTypes < 2 {
			if !errors.Is(newErr, gomoney.ErrInvalidCosts) {
				t.Fatalf("constructor limit error = %v", newErr)
			}
			return
		}
		if newErr != nil {
			t.Fatal(newErr)
		}
		mapping["small"] = large
		delete(mapping, "large")

		ctx, cancel := canceledContext(cancelled)
		defer cancel()
		first, solveErr := (solver.Exact{}).PackAll(
			ctx, request.Normalized(), solver.Options{PlanObjective: costs},
		)
		if cancelled {
			if !errors.Is(solveErr, context.Canceled) {
				t.Fatalf("cancelled solver error = %v", solveErr)
			}
			return
		}
		if solveErr != nil {
			t.Fatal(solveErr)
		}
		second, solveErr := (solver.Exact{}).PackAll(
			context.Background(), request.Normalized(),
			solver.Options{PlanObjective: costs},
		)
		if solveErr != nil {
			t.Fatal(solveErr)
		}
		if first.CanonicalString() != second.CanonicalString() {
			t.Fatal("identical solver callbacks produced different plans")
		}

		zero, zeroErr := small.Sub(small)
		if zeroErr != nil {
			t.Fatal(zeroErr)
		}
		want := zero
		for _, container := range first.Containers() {
			cost := small
			if container.TypeID == "large" {
				cost = large
			}
			want, zeroErr = want.Add(cost)
			if zeroErr != nil {
				t.Fatal(zeroErr)
			}
		}
		got, totalErr := costs.Total(first)
		if totalErr != nil {
			t.Fatal(totalErr)
		}
		equal, equalErr := got.Equal(want)
		if equalErr != nil || !equal {
			t.Fatalf("solver total = %s, direct total = %s (%v)", got, want, equalErr)
		}
	})
}
