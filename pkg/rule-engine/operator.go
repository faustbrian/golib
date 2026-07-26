package ruleengine

import (
	"context"
	"regexp"
	"strings"
)

// OperatorName is a stable operator identifier.
type OperatorName string

// Signature declares one exact pair of operand kinds accepted by an Operator.
type Signature struct {
	Left  Kind
	Right Kind
}

// Operator is an explicitly registered, typed, concurrency-safe extension.
// Implementations must be deterministic and must honor context cancellation.
type Operator interface {
	Name() OperatorName
	Signatures() []Signature
	Evaluate(context.Context, Value, Value) (bool, error)
}

type registeredOperator struct {
	implementation Operator
	signatures     []Signature
}

const (
	// OpEqual begins the stable built-in operator name set.
	OpEqual OperatorName = "equal"
	// OpNotEqual tests exact inequality.
	OpNotEqual OperatorName = "not_equal"
	// OpLessThan tests strict lower ordering.
	OpLessThan OperatorName = "less_than"
	// OpLessOrEqual tests inclusive lower ordering.
	OpLessOrEqual OperatorName = "less_or_equal"
	// OpGreaterThan tests strict higher ordering.
	OpGreaterThan OperatorName = "greater_than"
	// OpGreaterOrEqual tests inclusive higher ordering.
	OpGreaterOrEqual OperatorName = "greater_or_equal"
	// OpIn tests list membership.
	OpIn OperatorName = "in"
	// OpNotIn tests list non-membership.
	OpNotIn OperatorName = "not_in"
	// OpContains tests substring or list membership.
	OpContains OperatorName = "contains"
	// OpStartsWith tests a string prefix.
	OpStartsWith OperatorName = "starts_with"
	// OpEndsWith tests a string suffix.
	OpEndsWith OperatorName = "ends_with"
	// OpMatches tests a bounded regular expression.
	OpMatches OperatorName = "matches"
)

func knownOperator(name OperatorName) bool {
	switch name {
	case OpEqual, OpNotEqual, OpLessThan, OpLessOrEqual, OpGreaterThan,
		OpGreaterOrEqual, OpIn, OpNotIn, OpContains, OpStartsWith, OpEndsWith,
		OpMatches:
		return true
	default:
		return false
	}
}

func validateStaticOperator(name OperatorName, left, right Operand, operators map[OperatorName]registeredOperator, limits Limits) error {
	custom, customExists := operators[name]
	if !knownOperator(name) && !customExists {
		return newError(CodeUnknownOperator, "operator is not registered")
	}
	if literal, ok := left.(literalOperand); ok {
		if err := validateValue(literal.value, limits, 0); err != nil {
			return err
		}
	}
	if literal, ok := right.(literalOperand); ok {
		if err := validateValue(literal.value, limits, 0); err != nil {
			return err
		}
	}
	if name == OpMatches {
		pattern, ok := right.(literalOperand)
		if !ok || pattern.value.kind != KindString {
			return newError(CodeInvalidRule, "regex pattern must be a string literal")
		}
		text, _ := pattern.value.StringValue()
		if len(text) > limits.MaxRegexBytes {
			return newError(CodeLimitExceeded, "regex pattern is too large")
		}
		if _, err := regexp.Compile(text); err != nil {
			return newError(CodeInvalidRule, "regex pattern is invalid")
		}
	}
	leftKind, leftKnown := left.staticKind()
	rightKind, rightKnown := right.staticKind()
	if !leftKnown || !rightKnown {
		return nil
	}
	if customExists {
		if !supportsSignature(custom.signatures, leftKind, rightKind) {
			return newError(CodeTypeMismatch, "custom operator operands are incompatible")
		}
		return nil
	}
	return validateOperatorKinds(name, leftKind, rightKind)
}

func supportsSignature(signatures []Signature, left, right Kind) bool {
	for _, signature := range signatures {
		if signature.Left == left && signature.Right == right {
			return true
		}
	}
	return false
}

func validateOperatorKinds(name OperatorName, left, right Kind) error {
	if left == KindMissing || right == KindMissing {
		return nil
	}
	switch name {
	case OpEqual, OpNotEqual:
		if left != right {
			return newError(CodeTypeMismatch, "equality operands have different types")
		}
	case OpLessThan, OpLessOrEqual, OpGreaterThan, OpGreaterOrEqual:
		if left != right || !orderedKind(left) {
			return newError(CodeTypeMismatch, "ordering operands are incompatible")
		}
	case OpIn, OpNotIn:
		if right != KindList {
			return newError(CodeTypeMismatch, "membership requires a list on the right")
		}
	case OpContains:
		if left != KindString && left != KindList {
			return newError(CodeTypeMismatch, "contains requires a string or list")
		}
		if left == KindString && right != KindString {
			return newError(CodeTypeMismatch, "string contains requires a string")
		}
	case OpStartsWith, OpEndsWith, OpMatches:
		if left != KindString || right != KindString {
			return newError(CodeTypeMismatch, "string operator requires strings")
		}
	}
	return nil
}

func orderedKind(kind Kind) bool {
	return kind == KindInt || kind == KindFloat || kind == KindString ||
		kind == KindTime || kind == KindDuration
}

func evaluateBuiltin(name OperatorName, left, right Value) (bool, error) {
	if left.kind == KindMissing || right.kind == KindMissing {
		return false, nil
	}
	if err := validateOperatorKinds(name, left.kind, right.kind); err != nil {
		return false, err
	}
	switch name {
	case OpEqual, OpNotEqual:
		equal := valuesEqual(left, right)
		return equal == (name == OpEqual), nil
	case OpLessThan, OpLessOrEqual, OpGreaterThan, OpGreaterOrEqual:
		ordering := compareOrdered(left, right)
		if name == OpLessThan {
			return ordering < 0, nil
		}
		if name == OpLessOrEqual {
			return ordering <= 0, nil
		}
		if name == OpGreaterThan {
			return ordering > 0, nil
		}
		return ordering >= 0, nil
	case OpIn, OpNotIn:
		values, _ := right.ListValue()
		contains := listContains(values, left)
		return contains == (name == OpIn), nil
	case OpContains:
		if left.kind == KindString {
			leftText, _ := left.StringValue()
			rightText, _ := right.StringValue()
			return strings.Contains(leftText, rightText), nil
		}
		values, _ := left.ListValue()
		return listContains(values, right), nil
	case OpStartsWith, OpEndsWith:
		leftText, _ := left.StringValue()
		rightText, _ := right.StringValue()
		if name == OpStartsWith {
			return strings.HasPrefix(leftText, rightText), nil
		}
		return strings.HasSuffix(leftText, rightText), nil
	case OpMatches:
		leftText, _ := left.StringValue()
		rightText, _ := right.StringValue()
		matched, err := regexp.MatchString(rightText, leftText)
		if err != nil {
			return false, newError(CodeEvaluation, "regex evaluation failed")
		}
		return matched, nil
	default:
		return false, newError(CodeUnknownOperator, "operator is not registered")
	}
}

func valuesEqual(left, right Value) bool {
	if left.kind != right.kind {
		return false
	}
	if left.kind == KindList {
		leftValues, _ := left.ListValue()
		rightValues, _ := right.ListValue()
		if len(leftValues) != len(rightValues) {
			return false
		}
		for index := range leftValues {
			if !valuesEqual(leftValues[index], rightValues[index]) {
				return false
			}
		}
		return true
	}
	if left.kind == KindTime {
		leftTime, _ := left.TimeValue()
		rightTime, _ := right.TimeValue()
		return leftTime.Equal(rightTime)
	}
	return left.data == right.data
}

func listContains(values []Value, target Value) bool {
	for _, value := range values {
		if valuesEqual(value, target) {
			return true
		}
	}
	return false
}

func compareOrdered(left, right Value) int {
	switch left.kind {
	case KindMissing, KindNull, KindBool, KindList:
		return 0
	case KindInt:
		leftValue, _ := left.IntValue()
		rightValue, _ := right.IntValue()
		return compare(leftValue, rightValue)
	case KindFloat:
		leftValue, _ := left.FloatValue()
		rightValue, _ := right.FloatValue()
		return compare(leftValue, rightValue)
	case KindString:
		leftValue, _ := left.StringValue()
		rightValue, _ := right.StringValue()
		return strings.Compare(leftValue, rightValue)
	case KindTime:
		leftValue, _ := left.TimeValue()
		rightValue, _ := right.TimeValue()
		return compare(leftValue.UnixNano(), rightValue.UnixNano())
	case KindDuration:
		leftValue, _ := left.DurationValue()
		rightValue, _ := right.DurationValue()
		return compare(leftValue, rightValue)
	default:
		return 0
	}
}

func compare[T ~int64 | ~float64](left, right T) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
