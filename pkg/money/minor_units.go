package money

import (
	"strings"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
)

// FromMinorUnits constructs Money from an arbitrary-precision integer count of
// units at the supplied fixed context scale.
func FromMinorUnits(units integer.Integer, code currency.Code, context Context) (Money, error) {
	if context.IsZero() || context.kind == ContextAutomatic {
		return Money{}, ErrInvalidContext
	}

	return Parse(decimalTextFromMinor(units.String(), context.scale), code, context)
}

// MinorUnits returns the exact arbitrary-precision integer coefficient at the
// Money context's resolved scale.
func (money Money) MinorUnits() (integer.Integer, error) {
	if !money.Valid() {
		return integer.Integer{}, ErrInvalidMoney
	}

	text := strings.ReplaceAll(money.amount.String(), ".", "")
	return integer.Parse(text, integer.ParseOptions{
		Base:              10,
		AllowLeadingZeros: true,
		Limits:            arithmeticLimits(),
	})
}

func decimalTextFromMinor(text string, scale uint8) string {
	if scale == 0 {
		return text
	}

	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = "-"
		text = strings.TrimPrefix(text, "-")
	}
	places := int(scale)
	if len(text) <= places {
		text = strings.Repeat("0", places-len(text)+1) + text
	}
	point := len(text) - places

	return sign + text[:point] + "." + text[point:]
}
