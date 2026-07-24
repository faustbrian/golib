package ruleengine

import (
	"context"
	"time"
)

// Decision is the rule-set evaluation state.
type Decision uint8

const (
	// Unmatched means no selected rule matched and no error occurred.
	Unmatched Decision = iota
	// Matched means at least one selected rule matched without an error.
	Matched
	// Indeterminate means an error or bound prevented a reliable decision.
	Indeterminate
)

// Explanation records one bounded rule evaluation without fact values.
type Explanation struct {
	RuleID  RuleID
	Matched bool
}

// Result is the complete inspectable evaluation result.
type Result struct {
	Decision     Decision
	MatchedRules []string
	Explanation  []Explanation
	Errors       []error
	Duration     time.Duration
	DerivedFacts Context
}

// Plan is an immutable, concurrency-safe execution plan.
type Plan struct {
	setID         string
	namespace     string
	strategy      ConflictStrategy
	rules         []Rule
	limits        Limits
	operators     map[OperatorName]registeredOperator
	requiredPaths []Path
	hash          string
}

// Hash returns the canonical definition hash when the plan was compiled
// through CompileCached.
func (plan Plan) Hash() string { return plan.hash }

// FactResolver supplies explicitly requested missing facts. Implementations
// must be deterministic; EvaluateResolved invokes paths in lexical order.
type FactResolver interface {
	Resolve(context.Context, Path) (Value, Owner, bool, error)
}

// EvaluateResolved resolves only required paths absent from the base context,
// validates every returned value, then evaluates the completed snapshot.
func (plan Plan) EvaluateResolved(ctx context.Context, base Context, resolver FactResolver) Result {
	if resolver == nil {
		return Result{Decision: Indeterminate, Errors: []error{newError(CodeEvaluation, "fact resolver is nil")}}
	}
	working := base
	for _, path := range plan.requiredPaths {
		if working.Lookup(path).kind != KindMissing {
			continue
		}
		value, owner, found, err := resolver.Resolve(ctx, path)
		if err != nil {
			return Result{Decision: Indeterminate, Errors: []error{newError(CodeEvaluation, "fact resolution failed")}}
		}
		if !found {
			continue
		}
		fact := Fact{Path: path, Value: value, Owner: owner}
		if _, err := NewContextWithLimits(plan.limits, fact); err != nil {
			return Result{Decision: Indeterminate, Errors: []error{newError(CodeInvalidFact, "resolved fact is invalid")}}
		}
		working = working.withFact(fact)
	}
	return plan.Evaluate(ctx, working)
}

// Evaluate applies the compiled plan to an immutable fact snapshot.
func (plan Plan) Evaluate(ctx context.Context, facts Context) (result Result) {
	started := time.Now()
	derived, _ := NewContext()
	result = Result{Decision: Unmatched, DerivedFacts: derived}
	defer func() { result.Duration = time.Since(started) }()

	evaluationContext, cancel := context.WithTimeout(ctx, plan.limits.EvaluationTimeout)
	defer cancel()
	working := facts
	matchedRules := make(map[RuleID]bool)
	hadError := false
	for iteration := 0; iteration < plan.limits.MaxIterations; iteration++ {
		changed := false
		stop := false
		for _, rule := range plan.rules {
			if err := evaluationContext.Err(); err != nil {
				result.Decision = Indeterminate
				result.Errors = append(result.Errors, newError(CodeEvaluation, "evaluation canceled or timed out"))
				return result
			}
			matched, err := plan.evaluatePredicate(evaluationContext, rule.When, working)
			if len(result.Explanation) < plan.limits.MaxExplanation {
				result.Explanation = append(result.Explanation, Explanation{RuleID: rule.ID, Matched: matched})
			}
			if err != nil {
				hadError = true
				result.Decision = Indeterminate
				if len(result.Errors) < plan.limits.MaxDiagnostics {
					result.Errors = append(result.Errors, newError(CodeEvaluation, "rule evaluation failed"))
				}
				continue
			}
			if !matched {
				continue
			}
			if !matchedRules[rule.ID] {
				matchedRules[rule.ID] = true
				result.MatchedRules = append(result.MatchedRules, string(rule.ID))
			}
			if !hadError {
				result.Decision = Matched
			}
			for _, fact := range rule.Derive {
				existing := working.Lookup(fact.Path)
				if existing.kind == KindMissing {
					working = working.withFact(fact)
					result.DerivedFacts = result.DerivedFacts.withFact(fact)
					changed = true
					continue
				}
				if !valuesEqual(existing, fact.Value) {
					result.Decision = Indeterminate
					result.Errors = append(result.Errors, newError(CodeConflict, "derived fact conflicts with existing fact"))
					return result
				}
			}
			if plan.strategy == FirstMatch {
				stop = true
				break
			}
			if plan.strategy == ErrorOnMultiple && len(result.MatchedRules) > 1 {
				result.Decision = Indeterminate
				result.Errors = append(result.Errors, newError(CodeConflict, "multiple rules matched"))
				return result
			}
		}
		if !changed || stop {
			return result
		}
	}
	result.Decision = Indeterminate
	result.Errors = append(result.Errors, newError(CodeLimitExceeded, "forward chaining iteration limit exceeded"))
	return result
}

func (plan Plan) evaluatePredicate(ctx context.Context, predicate Predicate, facts Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch typed := predicate.(type) {
	case comparisonPredicate:
		left := typed.left.resolve(facts)
		right := typed.right.resolve(facts)
		if custom, exists := plan.operators[typed.operator]; exists {
			if left.kind == KindMissing || right.kind == KindMissing {
				return false, nil
			}
			if !supportsSignature(custom.signatures, left.kind, right.kind) {
				return false, newError(CodeTypeMismatch, "custom operator operands are incompatible")
			}
			return custom.implementation.Evaluate(ctx, left, right)
		}
		return evaluateBuiltin(typed.operator, left, right)
	case allPredicate:
		for _, child := range typed.children {
			matched, err := plan.evaluatePredicate(ctx, child, facts)
			if err != nil || !matched {
				return matched, err
			}
		}
		return true, nil
	case anyPredicate:
		for _, child := range typed.children {
			matched, err := plan.evaluatePredicate(ctx, child, facts)
			if err != nil || matched {
				return matched, err
			}
		}
		return false, nil
	case notPredicate:
		matched, err := plan.evaluatePredicate(ctx, typed.child, facts)
		return !matched, err
	default:
		return predicate.Evaluate(ctx, facts)
	}
}
