package money

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func FuzzParseMoney(f *testing.F) {
	for _, seed := range []string{"0", "-0.00", "1.23", "9007199254740993.10", "1e3", "NaN"} {
		f.Add(seed)
	}
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	f.Fuzz(func(t *testing.T, input string) {
		value, err := Parse(input, euro, monetaryContext)
		if err != nil {
			return
		}
		roundTrip, err := Parse(value.Amount().String(), euro, monetaryContext)
		if err != nil {
			t.Fatalf("round-trip parse error = %v", err)
		}
		equal, err := value.Equal(roundTrip)
		if err != nil || !equal {
			t.Fatalf("round trip = %s, want %s", roundTrip, value)
		}
	})
}

func FuzzAllocationConservation(f *testing.F) {
	f.Add(int64(1000), uint8(3))
	f.Add(int64(-1000), uint8(7))
	f.Add(int64(0), uint8(1))
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	f.Fuzz(func(t *testing.T, units int64, rawCount uint8) {
		count := int(rawCount%64) + 1
		total, err := FromMinorUnits(integer.New(units), euro, monetaryContext)
		if err != nil {
			t.Skip()
		}
		allocation, err := total.EqualSplit(context.Background(), count)
		if err != nil {
			t.Fatalf("EqualSplit() error = %v", err)
		}
		assertAllocationEquals(t, allocation, total)
	})
}

func FuzzWeightedAllocationConservation(f *testing.F) {
	f.Add(int64(1000), uint16(1), uint16(2), uint16(5), uint16(8))
	f.Add(int64(-1000), uint16(1), uint16(1), uint16(1), uint16(1))
	f.Add(int64(0), ^uint16(0), uint16(1), ^uint16(0), uint16(1))
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	f.Fuzz(func(t *testing.T, units int64, first, second, third, fourth uint16) {
		ratios := []integer.Integer{
			integer.New(int64(first) + 1),
			integer.New(int64(second) + 1),
			integer.New(int64(third) + 1),
			integer.New(int64(fourth) + 1),
		}
		total, err := FromMinorUnits(integer.New(units), euro, monetaryContext)
		if err != nil {
			t.Skip()
		}
		allocation, err := total.Allocate(context.Background(), ratios)
		if err != nil {
			t.Fatalf("Allocate() error = %v", err)
		}
		assertAllocationEquals(t, allocation, total)
	})
}

func FuzzRate(f *testing.F) {
	for _, seed := range []string{"0", "1", "1/3", "1.25", "-1", "1/0", "1000001"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		rate, err := ParseRate(input)
		if err != nil {
			return
		}
		if !rate.Valid() || rate.Rational().Sign() < 0 {
			t.Fatalf("accepted invalid rate %q", input)
		}
	})
}
