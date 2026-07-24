package abac

import (
	"net/netip"
	"sort"
	"strings"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type existsCondition struct{ reference Reference }

func Exists(reference Reference) Condition { return existsCondition{reference: reference} }

func (condition existsCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}
	value, exists := state.attribute(condition.reference)
	matched := exists && value.Kind() != authorization.ValueMissing
	if matched {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition existsCondition) validate() error {
	return validateReference(condition.reference)
}

type nullCondition struct{ reference Reference }

func IsNull(reference Reference) Condition { return nullCondition{reference: reference} }

func (condition nullCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}
	value, exists := state.attribute(condition.reference)
	if exists && value.Kind() == authorization.ValueNull {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition nullCondition) validate() error {
	return validateReference(condition.reference)
}

type anyCondition struct{ conditions []Condition }

func Any(conditions ...Condition) Condition {
	return anyCondition{conditions: append([]Condition(nil), conditions...)}
}

func (condition anyCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}
	status := StatusNoMatch
	for _, child := range condition.conditions {
		result, err := child.evaluate(state)
		if err != nil || result.Matched {
			return result, err
		}
		if status == StatusNoMatch && result.Status != StatusNoMatch {
			status = result.Status
		}
	}
	return Result{Status: status}, nil
}

func (condition anyCondition) validate() error {
	return validateChildren(condition.conditions)
}

type notCondition struct{ condition Condition }

func Not(condition Condition) Condition { return notCondition{condition: condition} }

func (condition notCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}
	result, err := condition.condition.evaluate(state)
	if err != nil {
		return result, err
	}
	if result.Status != StatusMatch && result.Status != StatusNoMatch {
		return result, nil
	}
	result.Matched = !result.Matched
	if result.Matched {
		result.Status = StatusMatch
	} else {
		result.Status = StatusNoMatch
	}
	return result, nil
}

func (condition notCondition) validate() error {
	if condition.condition == nil {
		return ErrInvalidCondition
	}
	return nil
}

type comparison uint8

const (
	compareGreater comparison = iota
	compareLess
)

type comparisonCondition struct {
	reference Reference
	want      authorization.Value
	operator  comparison
}

func GreaterThan(reference Reference, want authorization.Value) Condition {
	return comparisonCondition{reference: reference, want: want, operator: compareGreater}
}

func LessThan(reference Reference, want authorization.Value) Condition {
	return comparisonCondition{reference: reference, want: want, operator: compareLess}
}

func (condition comparisonCondition) evaluate(state *evaluationState) (Result, error) {
	got, status, err := loadValue(state, condition.reference)
	if err != nil || status != StatusMatch {
		return Result{Status: status}, err
	}
	if got.Kind() != condition.want.Kind() {
		return Result{Status: StatusTypeMismatch}, nil
	}

	comparisonResult, _ := got.Compare(condition.want)
	matched := (condition.operator == compareGreater && comparisonResult > 0) ||
		(condition.operator == compareLess && comparisonResult < 0)
	if matched {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition comparisonCondition) validate() error {
	if err := validateReference(condition.reference); err != nil {
		return err
	}
	switch condition.want.Kind() {
	case authorization.ValueString, authorization.ValueInt,
		authorization.ValueFloat, authorization.ValueTime:
		return nil
	default:
		return ErrInvalidCondition
	}
}

type inCondition struct {
	reference Reference
	values    []authorization.Value
}

func In(reference Reference, values ...authorization.Value) Condition {
	return inCondition{reference: reference, values: append([]authorization.Value(nil), values...)}
}

func (condition inCondition) evaluate(state *evaluationState) (Result, error) {
	got, status, err := loadValue(state, condition.reference)
	if err != nil || status != StatusMatch {
		return Result{Status: status}, err
	}
	if got.Kind() != condition.values[0].Kind() {
		return Result{Status: StatusTypeMismatch}, nil
	}
	for _, candidate := range condition.values {
		if got.Equal(candidate) {
			return Result{Matched: true, Status: StatusMatch}, nil
		}
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition inCondition) validate() error {
	if err := validateReference(condition.reference); err != nil || len(condition.values) == 0 {
		return ErrInvalidCondition
	}
	kind := condition.values[0].Kind()
	if kind == authorization.ValueMissing || kind == authorization.ValueNull {
		return ErrInvalidCondition
	}
	for _, value := range condition.values {
		if value.Kind() != kind {
			return ErrInvalidCondition
		}
	}
	return nil
}

type setContainsCondition struct {
	reference Reference
	value     string
}

func SetContains(reference Reference, value string) Condition {
	return setContainsCondition{reference: reference, value: value}
}

func (condition setContainsCondition) evaluate(state *evaluationState) (Result, error) {
	got, status, err := loadValue(state, condition.reference)
	if err != nil || status != StatusMatch {
		return Result{Status: status}, err
	}
	set, ok := got.StringSet()
	if !ok {
		return Result{Status: StatusTypeMismatch}, nil
	}
	index := sort.SearchStrings(set, condition.value)
	if index < len(set) && set[index] == condition.value {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition setContainsCondition) validate() error {
	return validateReference(condition.reference)
}

type stringOperator uint8

const (
	stringPrefix stringOperator = iota
	stringSuffix
	stringContains
)

type stringCondition struct {
	reference Reference
	value     string
	operator  stringOperator
}

func HasPrefix(reference Reference, value string) Condition {
	return stringCondition{reference: reference, value: value, operator: stringPrefix}
}
func HasSuffix(reference Reference, value string) Condition {
	return stringCondition{reference: reference, value: value, operator: stringSuffix}
}
func StringContains(reference Reference, value string) Condition {
	return stringCondition{reference: reference, value: value, operator: stringContains}
}

func (condition stringCondition) evaluate(state *evaluationState) (Result, error) {
	got, status, err := loadValue(state, condition.reference)
	if err != nil || status != StatusMatch {
		return Result{Status: status}, err
	}
	value, ok := got.String()
	if !ok {
		return Result{Status: StatusTypeMismatch}, nil
	}
	matched := false
	switch condition.operator {
	case stringPrefix:
		matched = strings.HasPrefix(value, condition.value)
	case stringSuffix:
		matched = strings.HasSuffix(value, condition.value)
	case stringContains:
		matched = strings.Contains(value, condition.value)
	}
	if matched {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition stringCondition) validate() error {
	return validateReference(condition.reference)
}

type ipCondition struct {
	reference Reference
	prefix    netip.Prefix
}

func IPIn(reference Reference, prefix netip.Prefix) Condition {
	return ipCondition{reference: reference, prefix: prefix.Masked()}
}

func (condition ipCondition) evaluate(state *evaluationState) (Result, error) {
	got, status, err := loadValue(state, condition.reference)
	if err != nil || status != StatusMatch {
		return Result{Status: status}, err
	}
	ip, ok := got.IP()
	if !ok {
		return Result{Status: StatusTypeMismatch}, nil
	}
	if condition.prefix.Contains(ip) {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (condition ipCondition) validate() error {
	if !condition.prefix.IsValid() {
		return ErrInvalidCondition
	}
	return validateReference(condition.reference)
}

func loadValue(
	state *evaluationState,
	reference Reference,
) (authorization.Value, Status, error) {
	if err := state.consume(); err != nil {
		return authorization.Value{}, StatusNoMatch, err
	}
	value, exists := state.attribute(reference)
	if !exists || value.Kind() == authorization.ValueMissing {
		return authorization.Value{}, StatusMissing, nil
	}
	if err := state.validateValue(value); err != nil {
		return authorization.Value{}, StatusNoMatch, err
	}
	if value.Kind() == authorization.ValueNull {
		return authorization.Value{}, StatusNull, nil
	}
	return value, StatusMatch, nil
}

func validateReference(reference Reference) error {
	if reference.Source > Environment || reference.Name == "" {
		return ErrInvalidCondition
	}
	return nil
}

func validateChildren(children []Condition) error {
	if len(children) == 0 {
		return ErrInvalidCondition
	}
	for _, child := range children {
		if child == nil {
			return ErrInvalidCondition
		}
	}
	return nil
}
