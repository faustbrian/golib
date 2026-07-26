package money

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
)

func TestDefaultContextUsesAuthoritativeCurrencyMinorUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code  string
		scale uint8
	}{
		{code: "JPY", scale: 0},
		{code: "EUR", scale: 2},
		{code: "BHD", scale: 3},
	}

	for _, test := range tests {
		code, parseErr := currency.Parse(test.code)
		if parseErr != nil {
			t.Fatalf("currency.Parse(%s) error = %v", test.code, parseErr)
		}
		context, err := DefaultContext(code)
		if err != nil {
			t.Fatalf("DefaultContext(%s) error = %v", test.code, err)
		}
		if context.Scale() != test.scale {
			t.Errorf("DefaultContext(%s).Scale() = %d, want %d", test.code, context.Scale(), test.scale)
		}
	}

	if _, err := DefaultContext(currency.Code{}); !errors.Is(err, ErrUnknownCurrency) {
		t.Fatalf("DefaultContext(zero) error = %v, want ErrUnknownCurrency", err)
	}
	for _, code := range currency.All() {
		scale, applicable := code.MinorUnits()
		if applicable && scale > MaxScale {
			t.Fatalf("currency %s minor-unit scale = %d, exceeds %d", code, scale, MaxScale)
		}
	}
}

func TestExplicitContextsValidateTheirBounds(t *testing.T) {
	t.Parallel()

	custom, err := CustomContext(4)
	if err != nil || custom.Kind() != ContextCustom || custom.Scale() != 4 {
		t.Fatalf("CustomContext(4) = %#v, %v", custom, err)
	}

	cash, err := CashContext(2, 5)
	if err != nil || cash.Kind() != ContextCash || cash.CashStep() != 5 {
		t.Fatalf("CashContext(2, 5) = %#v, %v", cash, err)
	}

	automatic := AutomaticContext()
	if automatic.Kind() != ContextAutomatic || automatic.IsZero() {
		t.Fatalf("AutomaticContext() = %#v", automatic)
	}

	if _, err := CustomContext(MaxScale + 1); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("CustomContext(over limit) error = %v", err)
	}
	if _, err := CashContext(2, 0); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("CashContext(zero step) error = %v", err)
	}
}
