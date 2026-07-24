// Package ruleenginemeasurement adapts exact measurement quantities
// without adding dependencies to the core rule-engine module.
package ruleenginemeasurement

import (
	"context"
	"fmt"
	"strings"

	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

const quantityPrefix = "quantity:"

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
	return ruleengine.String(quantityPrefix + value.String())
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
	if err := ctx.Err(); err != nil {
		return false, err
	}
	leftQuantity, err := parseQuantity(left)
	if err != nil {
		return false, err
	}
	rightQuantity, err := parseQuantity(right)
	if err != nil {
		return false, err
	}
	comparison, err := leftQuantity.Compare(rightQuantity, measurement.ExactConversion())
	if err != nil {
		return false, fmt.Errorf("rule-engine measurement: incompatible quantities: %w", err)
	}
	return operator.match(comparison), nil
}

func parseQuantity(value ruleengine.Value) (measurement.Quantity, error) {
	text, ok := value.StringValue()
	if !ok || !strings.HasPrefix(text, quantityPrefix) {
		return measurement.Quantity{}, fmt.Errorf("rule-engine measurement: invalid tagged value")
	}
	parsed, err := measurement.Parse(strings.TrimPrefix(text, quantityPrefix), measurement.SymbolProfile())
	if err != nil {
		return measurement.Quantity{}, fmt.Errorf("rule-engine measurement: invalid value: %w", err)
	}
	return parsed, nil
}
