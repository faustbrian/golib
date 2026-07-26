package money

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestAmountIsAnExactBoundedDecimalDomainValue(t *testing.T) {
	t.Parallel()

	amount, err := ParseAmount("9007199254740993.125")
	if err != nil {
		t.Fatalf("ParseAmount() error = %v", err)
	}
	if amount.String() != "9007199254740993.125" || amount.Scale() != 3 {
		t.Fatalf("ParseAmount() = %s at scale %d", amount, amount.Scale())
	}
	fromDecimal, err := AmountFromDecimal(decimal.MustParse("9007199254740993.125"))
	if err != nil || !amount.Equal(fromDecimal) {
		t.Fatal("equal exact amounts compared unequal")
	}

	if _, err := ParseAmount("1.0000000000000000000"); !errors.Is(err, ErrAmountLimit) {
		t.Fatalf("ParseAmount(over scale) error = %v, want ErrAmountLimit", err)
	}
}
