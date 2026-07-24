package money

import (
	"sort"

	"github.com/faustbrian/golib/pkg/international/currency"
)

// MaxMoneyBagEntries bounds heterogeneous output and lookup work.
const MaxMoneyBagEntries = 1_000

// MoneyBag is an immutable deterministic collection keyed by exact currency
// and context identity. Its zero value is an empty bag.
type MoneyBag struct{ values []Money }

// NewMoneyBag validates and combines values with identical identities.
func NewMoneyBag(values ...Money) (MoneyBag, error) {
	bag := MoneyBag{}
	var err error
	for _, value := range values {
		bag, err = bag.Add(value)
		if err != nil {
			return MoneyBag{}, err
		}
	}

	return bag, nil
}

// Add returns a new bag, combining only an identical currency/context entry.
func (bag MoneyBag) Add(value Money) (MoneyBag, error) {
	if !value.Valid() {
		return MoneyBag{}, ErrInvalidMoney
	}
	values := append([]Money(nil), bag.values...)
	for index, existing := range values {
		if existing.currency == value.currency && existing.context == value.context {
			combined, err := existing.Add(value)
			if err != nil {
				return MoneyBag{}, err
			}
			values[index] = combined
			return MoneyBag{values: values}, nil
		}
	}
	if len(values) >= MaxMoneyBagEntries {
		return MoneyBag{}, ErrMoneyBagLimit
	}
	values = append(values, value)
	sort.Slice(values, func(left, right int) bool {
		if values[left].currency != values[right].currency {
			return values[left].currency.String() < values[right].currency.String()
		}
		return contextLess(values[left].context, values[right].context)
	})

	return MoneyBag{values: values}, nil
}

// Values returns an independent deterministic slice.
func (bag MoneyBag) Values() []Money { return append([]Money(nil), bag.values...) }

// Get returns the value with exact currency and context identity.
func (bag MoneyBag) Get(code currency.Code, context Context) (Money, bool) {
	for _, value := range bag.values {
		if value.currency == code && value.context == context {
			return value, true
		}
	}

	return Money{}, false
}

func contextLess(left, right Context) bool {
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	if left.scale != right.scale {
		return left.scale < right.scale
	}
	if left.cashStep != right.cashStep {
		return left.cashStep < right.cashStep
	}

	return left.currency.String() < right.currency.String()
}
