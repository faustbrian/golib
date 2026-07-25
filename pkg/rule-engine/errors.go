package ruleengine

import "errors"

// Code classifies errors without exposing fact values.
type Code string

const (
	// CodeInvalidLimit begins the stable machine-readable error code set.
	CodeInvalidLimit Code = "invalid_limit"
	// CodeInvalidPath reports a malformed fact path.
	CodeInvalidPath Code = "invalid_path"
	// CodeDuplicateFact reports a repeated fact path.
	CodeDuplicateFact Code = "duplicate_fact"
	// CodeInvalidFact reports a malformed fact value.
	CodeInvalidFact Code = "invalid_fact"
	// CodeInvalidRule reports a malformed rule definition.
	CodeInvalidRule Code = "invalid_rule"
	// CodeDuplicateRule reports a repeated rule identifier.
	CodeDuplicateRule Code = "duplicate_rule"
	// CodeUnknownOperator reports an unregistered operator.
	CodeUnknownOperator Code = "unknown_operator"
	// CodeTypeMismatch reports incompatible value kinds.
	CodeTypeMismatch Code = "type_mismatch"
	// CodeLimitExceeded reports an exhausted resource budget.
	CodeLimitExceeded Code = "limit_exceeded"
	// CodeEvaluation reports a predicate evaluation failure.
	CodeEvaluation Code = "evaluation_error"
	// CodeConflict reports incompatible matches or facts.
	CodeConflict Code = "conflict"
	// CodeCycle reports a derivation dependency cycle.
	CodeCycle Code = "cycle"
	// CodeInvalidJSON reports a malformed JSON AST.
	CodeInvalidJSON Code = "invalid_json"
	// CodeNotSerializable reports an unsupported canonical value.
	CodeNotSerializable Code = "not_serializable"
	// CodeCache reports a plan cache failure.
	CodeCache Code = "cache_error"
)

// Error is a safe diagnostic error. Its message never contains fact values.
type Error struct {
	code    Code
	message string
}

func newError(code Code, message string) *Error {
	return &Error{code: code, message: message}
}

// Error implements error.
func (e *Error) Error() string { return string(e.code) + ": " + e.message }

// Code returns the stable machine-readable classification.
func (e *Error) Code() Code { return e.code }

// IsCode reports whether err or a wrapped error has code.
func IsCode(err error, code Code) bool {
	var target *Error
	return errors.As(err, &target) && target.code == code
}

func errorCode(err error, fallback Code) Code {
	var target *Error
	if errors.As(err, &target) {
		return target.code
	}
	return fallback
}
