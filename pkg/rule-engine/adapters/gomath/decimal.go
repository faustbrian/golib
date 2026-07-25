// Package ruleenginemath adapts exact math decimals without adding a
// math dependency to the core rule-engine module.
package ruleenginemath

import (
	"context"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

const decimalPrefix = "decimal:"

// Decimal operator names identify the exact comparison applied to two tagged
// decimal values.
const (
	OpDecimalEqual          ruleengine.OperatorName = "decimal_equal"
	OpDecimalLessThan       ruleengine.OperatorName = "decimal_less_than"
	OpDecimalLessOrEqual    ruleengine.OperatorName = "decimal_less_or_equal"
	OpDecimalGreaterThan    ruleengine.OperatorName = "decimal_greater_than"
	OpDecimalGreaterOrEqual ruleengine.OperatorName = "decimal_greater_or_equal"
)

// Decimal encodes an exact decimal as a tagged canonical string value.
func Decimal(value decimal.Decimal) ruleengine.Value {
	return ruleengine.String(decimalPrefix + value.String())
}

// Operators returns a fresh complete decimal comparison operator set.
func Operators() []ruleengine.Operator {
	return []ruleengine.Operator{
		decimalOperator{name: OpDecimalEqual, match: func(result int) bool { return result == 0 }},
		decimalOperator{name: OpDecimalLessThan, match: func(result int) bool { return result < 0 }},
		decimalOperator{name: OpDecimalLessOrEqual, match: func(result int) bool { return result <= 0 }},
		decimalOperator{name: OpDecimalGreaterThan, match: func(result int) bool { return result > 0 }},
		decimalOperator{name: OpDecimalGreaterOrEqual, match: func(result int) bool { return result >= 0 }},
	}
}

type decimalOperator struct {
	name  ruleengine.OperatorName
	match func(int) bool
}

func (operator decimalOperator) Name() ruleengine.OperatorName { return operator.name }
func (decimalOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (operator decimalOperator) Evaluate(ctx context.Context, left, right ruleengine.Value) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	leftValue, err := parseDecimal(left)
	if err != nil {
		return false, err
	}
	rightValue, err := parseDecimal(right)
	if err != nil {
		return false, err
	}
	return operator.match(leftValue.Cmp(rightValue)), nil
}

func parseDecimal(value ruleengine.Value) (decimal.Decimal, error) {
	text, ok := value.StringValue()
	if !ok || !strings.HasPrefix(text, decimalPrefix) {
		return decimal.Decimal{}, fmt.Errorf("rule-engine decimal: invalid tagged value")
	}
	parsed, err := decimal.Parse(strings.TrimPrefix(text, decimalPrefix))
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("rule-engine decimal: invalid value: %w", err)
	}
	return parsed, nil
}
