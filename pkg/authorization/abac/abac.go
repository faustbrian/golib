// Package abac provides a bounded, closed expression model for typed
// attribute-based authorization.
package abac

import (
	"context"
	"errors"
	"fmt"
	"sort"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var (
	ErrInvalidRule             = errors.New("invalid ABAC rule")
	ErrInvalidCondition        = errors.New("invalid ABAC condition")
	ErrCostExceeded            = errors.New("ABAC evaluation cost exceeded")
	ErrDepthExceeded           = errors.New("ABAC condition depth exceeded")
	ErrRuleLimitExceeded       = errors.New("ABAC rule limit exceeded")
	ErrSetLimitExceeded        = errors.New("ABAC set limit exceeded")
	ErrMatchLimitExceeded      = errors.New("ABAC match limit exceeded")
	ErrBatchLimitExceeded      = errors.New("ABAC batch limit exceeded")
	ErrUnknownNamedCondition   = errors.New("unknown named ABAC condition")
	ErrDuplicateNamedCondition = errors.New("duplicate named ABAC condition")
)

const (
	ReasonAllow         authorization.ReasonCode = "abac-allow"
	ReasonExplicitDeny  authorization.ReasonCode = "abac-explicit-deny"
	ReasonLimitExceeded authorization.ReasonCode = "abac-limit-exceeded"
)

type Source uint8

const (
	Subject Source = iota
	Resource
	Request
	Environment
)

type Reference struct {
	Source Source
	Name   authorization.AttributeName
}

type Status uint8

const (
	StatusNoMatch Status = iota
	StatusMatch
	StatusMissing
	StatusNull
	StatusTypeMismatch
)

type Result struct {
	Matched bool
	Status  Status
	Cost    int
}

type Condition interface {
	evaluate(*evaluationState) (Result, error)
	validate() error
}

type Rule struct {
	ID               authorization.PolicyID
	Priority         int
	Tenant           authorization.TenantID
	Action           authorization.Action
	ResourceType     authorization.ResourceType
	ResourceID       authorization.ResourceID
	Effect           authorization.Outcome
	Condition        Condition
	ConditionName    string
	ConditionVersion uint64
}

type NamedCondition struct {
	Name      string
	Version   uint64
	Condition Condition
}

type Limits struct {
	MaxRules           int `json:"max_rules,omitempty"`
	MaxDepth           int `json:"max_depth,omitempty"`
	MaxCost            int `json:"max_cost,omitempty"`
	MaxMatches         int `json:"max_matches,omitempty"`
	MaxSetSize         int `json:"max_set_size,omitempty"`
	MaxBatchSize       int `json:"max_batch_size,omitempty"`
	MaxNamedConditions int `json:"max_named_conditions,omitempty"`
}

const (
	defaultMaxRules           = 1000
	defaultMaxDepth           = 32
	defaultMaxCost            = 1000
	defaultMaxMatches         = 100
	defaultMaxSetSize         = 1000
	defaultMaxBatchSize       = 1000
	defaultMaxNamedConditions = 1000
)

type Option func(*Evaluator)

func WithLimits(limits Limits) Option {
	return func(evaluator *Evaluator) {
		if limits.MaxRules > 0 {
			evaluator.limits.MaxRules = limits.MaxRules
		}
		if limits.MaxDepth > 0 {
			evaluator.limits.MaxDepth = limits.MaxDepth
		}
		if limits.MaxCost > 0 {
			evaluator.limits.MaxCost = limits.MaxCost
		}
		if limits.MaxMatches > 0 {
			evaluator.limits.MaxMatches = limits.MaxMatches
		}
		if limits.MaxSetSize > 0 {
			evaluator.limits.MaxSetSize = limits.MaxSetSize
		}
		if limits.MaxBatchSize > 0 {
			evaluator.limits.MaxBatchSize = limits.MaxBatchSize
		}
		if limits.MaxNamedConditions > 0 {
			evaluator.limits.MaxNamedConditions = limits.MaxNamedConditions
		}
	}
}

type Evaluator struct {
	rules  []Rule
	limits Limits
}

func New(
	rules []Rule,
	namedConditions []NamedCondition,
	options ...Option,
) (*Evaluator, error) {
	evaluator := &Evaluator{
		limits: normalizedLimits(Limits{}),
	}
	for _, option := range options {
		option(evaluator)
	}
	if len(rules) > evaluator.limits.MaxRules {
		return nil, ErrRuleLimitExceeded
	}
	if len(namedConditions) > evaluator.limits.MaxNamedConditions {
		return nil, ErrRuleLimitExceeded
	}

	type namedKey struct {
		name    string
		version uint64
	}
	named := make(map[namedKey]Condition, len(namedConditions))
	for index, definition := range namedConditions {
		if definition.Name == "" || definition.Version == 0 || definition.Condition == nil {
			return nil, fmt.Errorf("named condition %d: %w", index, ErrInvalidCondition)
		}
		key := namedKey{name: definition.Name, version: definition.Version}
		if _, exists := named[key]; exists {
			return nil, fmt.Errorf("named condition %q version %d: %w", definition.Name, definition.Version, ErrDuplicateNamedCondition)
		}
		if err := validateConditionLimits(definition.Condition, evaluator.limits); err != nil {
			return nil, fmt.Errorf("named condition %q version %d: %w", definition.Name, definition.Version, err)
		}
		named[key] = definition.Condition
	}

	ruleIDs := make(map[authorization.PolicyID]struct{}, len(rules))
	evaluator.rules = append([]Rule(nil), rules...)
	for index, rule := range evaluator.rules {
		if rule.ConditionName != "" {
			if rule.Condition != nil || rule.ConditionVersion == 0 {
				return nil, fmt.Errorf("rule %d: %w", index, ErrInvalidRule)
			}
			condition, exists := named[namedKey{name: rule.ConditionName, version: rule.ConditionVersion}]
			if !exists {
				return nil, fmt.Errorf("rule %q: %w", rule.ID, ErrUnknownNamedCondition)
			}
			rule.Condition = condition
			evaluator.rules[index] = rule
		} else if rule.ConditionVersion != 0 {
			return nil, fmt.Errorf("rule %d: %w", index, ErrInvalidRule)
		}
		if rule.ID == "" || rule.Action == "" || rule.ResourceType == "" ||
			(rule.Effect != authorization.Allow && rule.Effect != authorization.Deny) ||
			rule.Condition == nil {
			return nil, fmt.Errorf("rule %d: %w", index, ErrInvalidRule)
		}
		if _, exists := ruleIDs[rule.ID]; exists {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, ErrInvalidRule)
		}
		if err := validateConditionLimits(rule.Condition, evaluator.limits); err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, err)
		}
		ruleIDs[rule.ID] = struct{}{}
	}
	sort.Slice(evaluator.rules, func(left, right int) bool {
		if evaluator.rules[left].Priority == evaluator.rules[right].Priority {
			return evaluator.rules[left].ID < evaluator.rules[right].ID
		}
		return evaluator.rules[left].Priority > evaluator.rules[right].Priority
	})

	return evaluator, nil
}

func (evaluator *Evaluator) Evaluate(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	state := &evaluationState{
		ctx: ctx, request: request,
		maxCost: evaluator.limits.MaxCost, maxSetSize: evaluator.limits.MaxSetSize,
	}
	decision := authorization.Decision{Outcome: authorization.NotApplicable}

	for _, rule := range evaluator.rules {
		if rule.Tenant != request.Tenant || rule.Action != request.Action ||
			rule.ResourceType != request.Resource.Type ||
			(rule.ResourceID != "" && rule.ResourceID != request.Resource.ID) {
			continue
		}

		result, err := rule.Condition.evaluate(state)
		if err != nil {
			reason := ReasonLimitExceeded
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason = authorization.ReasonContextCanceled
			}
			return authorization.Decision{Outcome: authorization.Deny, Reason: reason}, err
		}
		if !result.Matched {
			continue
		}
		if len(decision.MatchedPolicyIDs) >= evaluator.limits.MaxMatches {
			return authorization.Decision{
				Outcome: authorization.Deny,
				Reason:  ReasonLimitExceeded,
			}, ErrMatchLimitExceeded
		}

		decision.MatchedPolicyIDs = append(decision.MatchedPolicyIDs, rule.ID)
		if rule.Effect == authorization.Deny {
			decision.Outcome = authorization.Deny
			decision.Reason = ReasonExplicitDeny
		} else if decision.Outcome == authorization.NotApplicable {
			decision.Outcome = authorization.Allow
			decision.Reason = ReasonAllow
		}
	}

	return decision, nil
}

func (evaluator *Evaluator) EvaluateBatch(
	ctx context.Context,
	requests []authorization.Request,
) ([]authorization.Decision, error) {
	if len(requests) > evaluator.limits.MaxBatchSize {
		return nil, ErrBatchLimitExceeded
	}

	decisions := make([]authorization.Decision, len(requests))
	evaluationErrors := make([]error, 0)
	for index, request := range requests {
		decision, err := evaluator.Evaluate(ctx, request)
		decisions[index] = decision
		if err != nil {
			evaluationErrors = append(evaluationErrors, err)
		}
	}

	return decisions, errors.Join(evaluationErrors...)
}

func validateConditionLimits(condition Condition, limits Limits) error {
	type frame struct {
		condition Condition
		depth     int
	}
	stack := []frame{{condition: condition, depth: 1}}
	cardinality := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.depth > limits.MaxDepth {
			return ErrDepthExceeded
		}
		if err := current.condition.validate(); err != nil {
			return err
		}

		children := conditionChildren(current.condition)
		for index := len(children) - 1; index >= 0; index-- {
			stack = append(stack, frame{condition: children[index], depth: current.depth + 1})
		}

		localCardinality := conditionLocalCardinality(current.condition)
		if localCardinality > limits.MaxSetSize-cardinality {
			return ErrSetLimitExceeded
		}
		cardinality += localCardinality
	}
	return nil
}

func conditionChildren(condition Condition) []Condition {
	switch typed := condition.(type) {
	case allCondition:
		return typed.conditions
	case anyCondition:
		return typed.conditions
	case notCondition:
		if typed.condition != nil {
			return []Condition{typed.condition}
		}
	}
	return nil
}

func conditionLocalCardinality(condition Condition) int {
	switch typed := condition.(type) {
	case inCondition:
		return len(typed.values)
	case equalCondition:
		values, ok := typed.want.StringSet()
		if ok {
			return len(values)
		}
	}
	return 0
}

func normalizedLimits(limits Limits) Limits {
	normalized := Limits{
		MaxRules:           defaultMaxRules,
		MaxDepth:           defaultMaxDepth,
		MaxCost:            defaultMaxCost,
		MaxMatches:         defaultMaxMatches,
		MaxSetSize:         defaultMaxSetSize,
		MaxBatchSize:       defaultMaxBatchSize,
		MaxNamedConditions: defaultMaxNamedConditions,
	}
	evaluator := &Evaluator{limits: normalized}
	WithLimits(limits)(evaluator)
	return evaluator.limits
}

func EvaluateCondition(
	ctx context.Context,
	condition Condition,
	request authorization.Request,
	limits Limits,
) (Result, error) {
	if condition == nil {
		return Result{}, ErrInvalidCondition
	}
	limits = normalizedLimits(limits)
	if err := validateConditionLimits(condition, limits); err != nil {
		return Result{}, err
	}
	state := &evaluationState{
		ctx: ctx, request: request,
		maxCost: limits.MaxCost, maxSetSize: limits.MaxSetSize,
	}
	result, err := condition.evaluate(state)
	result.Cost = state.cost
	return result, err
}

type evaluationState struct {
	ctx        context.Context
	request    authorization.Request
	cost       int
	maxCost    int
	maxSetSize int
}

func (state *evaluationState) consume() error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	if state.cost >= state.maxCost {
		return ErrCostExceeded
	}
	state.cost++
	return nil
}

func (state *evaluationState) attribute(reference Reference) (authorization.Value, bool) {
	var attributes authorization.Attributes
	switch reference.Source {
	case Subject:
		attributes = state.request.Subject.Attributes
	case Resource:
		attributes = state.request.Resource.Attributes
	case Request:
		attributes = state.request.Attributes
	case Environment:
		attributes = state.request.Environment.Attributes
	}

	value, exists := attributes[reference.Name]
	return value, exists
}

type equalCondition struct {
	reference Reference
	want      authorization.Value
}

func Equal(reference Reference, want authorization.Value) Condition {
	return equalCondition{reference: reference, want: want}
}

func (condition equalCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}

	got, exists := state.attribute(condition.reference)
	if !exists || got.Kind() == authorization.ValueMissing {
		return Result{Status: StatusMissing}, nil
	}
	if err := state.validateValue(got); err != nil {
		return Result{}, err
	}
	if got.Kind() == authorization.ValueNull {
		if condition.want.Kind() == authorization.ValueNull {
			return Result{Matched: true, Status: StatusMatch}, nil
		}
		return Result{Status: StatusNull}, nil
	}
	if got.Kind() != condition.want.Kind() {
		return Result{Status: StatusTypeMismatch}, nil
	}
	if got.Equal(condition.want) {
		return Result{Matched: true, Status: StatusMatch}, nil
	}
	return Result{Status: StatusNoMatch}, nil
}

func (state *evaluationState) validateValue(value authorization.Value) error {
	if length, collection := value.CollectionLength(); collection && length > state.maxSetSize {
		return ErrSetLimitExceeded
	}
	return nil
}

func (condition equalCondition) validate() error {
	if condition.reference.Source > Environment || condition.reference.Name == "" ||
		condition.want.Kind() == authorization.ValueMissing {
		return ErrInvalidCondition
	}
	return nil
}

type allCondition struct {
	conditions []Condition
}

func All(conditions ...Condition) Condition {
	return allCondition{conditions: append([]Condition(nil), conditions...)}
}

func (condition allCondition) evaluate(state *evaluationState) (Result, error) {
	if err := state.consume(); err != nil {
		return Result{}, err
	}
	for _, child := range condition.conditions {
		result, err := child.evaluate(state)
		if err != nil || !result.Matched {
			return result, err
		}
	}
	return Result{Matched: true, Status: StatusMatch}, nil
}

func (condition allCondition) validate() error {
	return validateChildren(condition.conditions)
}
