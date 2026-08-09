// Package ruleenginemath adapts exact math decimals without adding a
// math dependency to the core rule-engine module.
package ruleenginemath

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

// EncodingV1Prefix identifies the first canonical persisted decimal encoding.
// A tagged value is this prefix followed by decimal.Decimal.MarshalText output.
const EncodingV1Prefix = "golib.rule-engine.decimal/v1:"

var (
	// ErrInvalidTaggedValue reports a non-string value or an unknown or malformed
	// decimal encoding tag. It never includes the rejected value.
	ErrInvalidTaggedValue = errors.New("rule-engine gomath: invalid tagged value")
	// ErrNonCanonicalDecimal reports a valid decimal payload that is not the
	// canonical decimal.Decimal.String representation.
	ErrNonCanonicalDecimal = errors.New("rule-engine gomath: noncanonical decimal")
)

// Decimal operator names identify the exact comparison applied to two tagged
// decimal values.
const (
	OpDecimalEqual          ruleengine.OperatorName = "golib_decimal_v1_equal"
	OpDecimalLessThan       ruleengine.OperatorName = "golib_decimal_v1_less_than"
	OpDecimalLessOrEqual    ruleengine.OperatorName = "golib_decimal_v1_less_or_equal"
	OpDecimalGreaterThan    ruleengine.OperatorName = "golib_decimal_v1_greater_than"
	OpDecimalGreaterOrEqual ruleengine.OperatorName = "golib_decimal_v1_greater_or_equal"
)

// Decimal encodes an exact decimal as a versioned, tagged canonical string
// value. It preserves the exact base-10 value and explicit fractional scale.
func Decimal(value decimal.Decimal) ruleengine.Value {
	text, _ := value.MarshalText()

	return ruleengine.String(EncodingV1Prefix + string(text))
}

// Operators returns a fresh complete decimal comparison operator set using
// math.DefaultLimits. Callers own registration of the returned operators.
func Operators() []ruleengine.Operator {
	return operators(gomath.DefaultLimits())
}

// OperatorsWithLimits validates limits and returns a fresh complete decimal
// comparison operator set. Limits are copied into each immutable operator.
func OperatorsWithLimits(limits gomath.Limits) ([]ruleengine.Operator, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}

	return operators(limits), nil
}

func operators(limits gomath.Limits) []ruleengine.Operator {
	return []ruleengine.Operator{
		decimalOperator{name: OpDecimalEqual, limits: limits, match: func(result int) bool { return result == 0 }},
		decimalOperator{name: OpDecimalLessThan, limits: limits, match: func(result int) bool { return result < 0 }},
		decimalOperator{name: OpDecimalLessOrEqual, limits: limits, match: func(result int) bool { return result <= 0 }},
		decimalOperator{name: OpDecimalGreaterThan, limits: limits, match: func(result int) bool { return result > 0 }},
		decimalOperator{name: OpDecimalGreaterOrEqual, limits: limits, match: func(result int) bool { return result >= 0 }},
	}
}

type decimalOperator struct {
	name   ruleengine.OperatorName
	limits gomath.Limits
	match  func(int) bool
}

func (operator decimalOperator) Name() ruleengine.OperatorName { return operator.name }
func (decimalOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (operator decimalOperator) Evaluate(ctx context.Context, left, right ruleengine.Value) (bool, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	leftValue, err := parseDecimal(left, operator.limits)
	if err != nil {
		return false, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	rightValue, err := parseDecimal(right, operator.limits)
	if err != nil {
		return false, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	return operator.match(leftValue.Cmp(rightValue)), nil
}

func parseDecimal(value ruleengine.Value, limits gomath.Limits) (decimal.Decimal, error) {
	text, ok := value.StringValue()
	if !ok || !strings.HasPrefix(text, EncodingV1Prefix) {
		return decimal.Decimal{}, ErrInvalidTaggedValue
	}
	payload := strings.TrimPrefix(text, EncodingV1Prefix)
	parsed, err := decimal.ParseWithOptions(payload, decimal.ParseOptions{Limits: limits})
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("rule-engine decimal: invalid value: %w", err)
	}
	if parsed.String() != payload {
		return decimal.Decimal{}, ErrNonCanonicalDecimal
	}
	return parsed, nil
}
