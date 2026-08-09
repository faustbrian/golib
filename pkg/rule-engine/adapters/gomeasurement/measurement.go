// Package ruleenginemeasurement adapts exact measurement quantities
// without adding dependencies to the core rule-engine module.
package ruleenginemeasurement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

const quantityPrefix = "quantity:v1|"

// MaxTaggedValueBytes accommodates the v1 envelope, the longest unit symbol,
// and a signed decimal at the measurement package's exact output-digit limit.
const MaxTaggedValueBytes = 100_020

var (
	// ErrInvalidQuantity reports an invalid tagged value or a quantity whose
	// exact conversion cannot be represented within measurement limits.
	ErrInvalidQuantity = errors.New("rule-engine measurement: invalid quantity")
	// ErrIncompatibleQuantity reports quantities with different dimensions.
	ErrIncompatibleQuantity = errors.New("rule-engine measurement: incompatible quantity")
)

// Quantity operator names identify the exact comparison applied to two tagged
// quantities after compatible-unit conversion.
const (
	OpQuantityEqual          ruleengine.OperatorName = "quantity_equal"
	OpQuantityLessThan       ruleengine.OperatorName = "quantity_less_than"
	OpQuantityLessOrEqual    ruleengine.OperatorName = "quantity_less_or_equal"
	OpQuantityGreaterThan    ruleengine.OperatorName = "quantity_greater_than"
	OpQuantityGreaterOrEqual ruleengine.OperatorName = "quantity_greater_or_equal"
)

// Quantity encodes an exact amount and unit as a tagged canonical string.
func Quantity(value measurement.Quantity) ruleengine.Value {
	return ruleengine.String(quantityPrefix + value.Amount().String() + "|" + value.Unit().String())
}

// Operators returns a fresh complete quantity comparison operator set.
func Operators() []ruleengine.Operator {
	return []ruleengine.Operator{
		quantityOperator{name: OpQuantityEqual, match: func(result int) bool { return result == 0 }},
		quantityOperator{name: OpQuantityLessThan, match: func(result int) bool { return result < 0 }},
		quantityOperator{name: OpQuantityLessOrEqual, match: func(result int) bool { return result <= 0 }},
		quantityOperator{name: OpQuantityGreaterThan, match: func(result int) bool { return result > 0 }},
		quantityOperator{name: OpQuantityGreaterOrEqual, match: func(result int) bool { return result >= 0 }},
	}
}

type quantityOperator struct {
	name  ruleengine.OperatorName
	match func(int) bool
}

func (operator quantityOperator) Name() ruleengine.OperatorName { return operator.name }
func (quantityOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (operator quantityOperator) Evaluate(ctx context.Context, left, right ruleengine.Value) (bool, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return false, contextErr
	}
	leftQuantity, err := parseQuantity(left)
	if err != nil {
		return false, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return false, contextErr
	}
	rightQuantity, err := parseQuantity(right)
	if err != nil {
		return false, err
	}
	comparison, err := leftQuantity.Compare(rightQuantity, measurement.ExactConversion())
	if err != nil {
		if errors.Is(err, measurement.ErrDimensionMismatch) {
			return false, fmt.Errorf("%w: %w", ErrIncompatibleQuantity, err)
		}

		return false, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return false, contextErr
	}
	return operator.match(comparison), nil
}

func parseQuantity(value ruleengine.Value) (measurement.Quantity, error) {
	text, ok := value.StringValue()
	if !ok {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	if len(text) > MaxTaggedValueBytes {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	if !strings.HasPrefix(text, quantityPrefix) {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	payload := strings.TrimPrefix(text, quantityPrefix)
	amountText, unitText, found := strings.Cut(payload, "|")
	if !found {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	if amountText == "" {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	if unitText == "" {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	if strings.Contains(unitText, "|") {
		return measurement.Quantity{}, ErrInvalidQuantity
	}
	amount, err := decimal.Parse(amountText)
	if err != nil {
		return measurement.Quantity{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	unit := measurement.Unit(unitText)
	if _, err := unit.Dimension(); err != nil {
		return measurement.Quantity{}, errors.Join(ErrInvalidQuantity, measurement.ErrUnknownUnit)
	}
	return measurement.MustNew(amount, unit), nil
}
