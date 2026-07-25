// Package gomath defines contracts shared by the numeric subpackages.
package gomath

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidArgument reports an invalid option or operation argument.
	ErrInvalidArgument = errors.New("math: invalid argument")
	// ErrInvalidSyntax reports input outside a type's strict grammar.
	ErrInvalidSyntax = errors.New("math: invalid syntax")
	// ErrLimitExceeded reports work rejected by an explicit resource bound.
	ErrLimitExceeded = errors.New("math: resource limit exceeded")
	// ErrDivisionByZero reports division by numeric zero.
	ErrDivisionByZero = errors.New("math: division by zero")
	// ErrDomain reports an operation outside its mathematical domain.
	ErrDomain = errors.New("math: domain error")
	// ErrConversion reports a conversion that cannot be exact.
	ErrConversion = errors.New("math: inexact conversion")
	// ErrOverflow reports a result outside an explicitly bounded range.
	ErrOverflow = errors.New("math: overflow")
	// ErrUnderflow reports a nonzero result below an explicitly bounded range.
	ErrUnderflow = errors.New("math: underflow")
	// ErrTrappedCondition reports a decimal or floating-point condition selected
	// by an operation context's trap mask.
	ErrTrappedCondition = errors.New("math: trapped condition")
)

// Condition is a bit set of arithmetic conditions.
type Condition uint16

const (
	ConditionRounded Condition = 1 << iota
	ConditionInexact
	ConditionOverflow
	ConditionUnderflow
	ConditionDivisionByZero
	ConditionInvalidOperation
	ConditionClamped
	ConditionSubnormal
)

// Has reports whether every bit in wanted is present.
func (c Condition) Has(wanted Condition) bool { return c&wanted == wanted }

// String returns stable comma-separated condition names.
func (c Condition) String() string {
	if c == 0 {
		return "none"
	}

	names := []struct {
		condition Condition
		name      string
	}{
		{ConditionRounded, "rounded"},
		{ConditionInexact, "inexact"},
		{ConditionOverflow, "overflow"},
		{ConditionUnderflow, "underflow"},
		{ConditionDivisionByZero, "division_by_zero"},
		{ConditionInvalidOperation, "invalid_operation"},
		{ConditionClamped, "clamped"},
		{ConditionSubnormal, "subnormal"},
	}
	result := make([]string, 0, len(names)+1)
	known := Condition(0)
	for _, candidate := range names {
		known |= candidate.condition
		if c.Has(candidate.condition) {
			result = append(result, candidate.name)
		}
	}
	if unknown := c &^ known; unknown != 0 {
		result = append(result, fmt.Sprintf("unknown(0x%x)", uint16(unknown)))
	}

	return strings.Join(result, ",")
}

// RoundingMode selects how a discarded nonzero remainder affects a result.
type RoundingMode uint8

const (
	RoundHalfEven RoundingMode = iota
	RoundHalfUp
	RoundHalfDown
	RoundDown
	RoundUp
	RoundCeiling
	RoundFloor
)

// Valid reports whether r is a supported rounding mode.
func (r RoundingMode) Valid() bool { return r <= RoundFloor }

// String returns the stable configuration name of r.
func (r RoundingMode) String() string {
	names := [...]string{
		"half_even", "half_up", "half_down", "down", "up", "ceiling", "floor",
	}
	if !r.Valid() {
		return fmt.Sprintf("RoundingMode(%d)", r)
	}

	return names[r]
}
