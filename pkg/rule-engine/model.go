package ruleengine

import "context"

// RuleID is a stable identifier within a rule set.
type RuleID string

// ConflictStrategy controls how ordered matches are selected.
type ConflictStrategy uint8

const (
	// FirstMatch selects only the first deterministically ordered match.
	FirstMatch ConflictStrategy = iota
	// CollectAll selects every unique matching rule.
	CollectAll
	// ErrorOnMultiple rejects more than one unique match.
	ErrorOnMultiple
)

// Rule is a proposition with stable metadata and optional derived facts.
type Rule struct {
	ID        RuleID
	Namespace string
	Priority  int
	Tags      []string
	When      Predicate
	Derive    []Fact
}

// RuleSet is an independently compiled collection of rules.
type RuleSet struct {
	ID        string
	Namespace string
	Strategy  ConflictStrategy
	Rules     []Rule
}

// Predicate evaluates supplied facts without side effects.
type Predicate interface {
	Evaluate(context.Context, Context) (bool, error)
}

// PredicateFunc adapts a function as an explicit extension predicate.
type PredicateFunc func(context.Context, Context) (bool, error)

// Evaluate invokes the adapted predicate function.
func (predicate PredicateFunc) Evaluate(ctx context.Context, facts Context) (bool, error) {
	return predicate(ctx, facts)
}

type constantPredicate bool

func (predicate constantPredicate) Evaluate(ctx context.Context, _ Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return bool(predicate), nil
}

// True returns an always-true predicate.
func True() Predicate { return constantPredicate(true) }

// False returns an always-false predicate.
func False() Predicate { return constantPredicate(false) }

type allPredicate struct{ children []Predicate }

func (predicate allPredicate) Evaluate(ctx context.Context, facts Context) (bool, error) {
	for _, child := range predicate.children {
		matched, err := child.Evaluate(ctx, facts)
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

// All evaluates children left-to-right and stops at the first false result.
func All(children ...Predicate) Predicate {
	return allPredicate{children: append([]Predicate(nil), children...)}
}

type anyPredicate struct{ children []Predicate }

func (predicate anyPredicate) Evaluate(ctx context.Context, facts Context) (bool, error) {
	for _, child := range predicate.children {
		matched, err := child.Evaluate(ctx, facts)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
}

// Any evaluates children left-to-right and stops at the first true result.
func Any(children ...Predicate) Predicate {
	return anyPredicate{children: append([]Predicate(nil), children...)}
}

type notPredicate struct{ child Predicate }

func (predicate notPredicate) Evaluate(ctx context.Context, facts Context) (bool, error) {
	matched, err := predicate.child.Evaluate(ctx, facts)
	return !matched, err
}

// Not negates a predicate.
func Not(child Predicate) Predicate { return notPredicate{child: child} }

type existsPredicate struct{ path Path }

func (predicate existsPredicate) Evaluate(ctx context.Context, facts Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return facts.Lookup(predicate.path).kind != KindMissing, nil
}

// Exists reports whether a path was supplied, including an explicit null.
func Exists(path Path) Predicate { return existsPredicate{path: path} }

// Operand resolves a typed value from a literal or fact variable.
type Operand interface {
	resolve(Context) Value
	staticKind() (Kind, bool)
}

type literalOperand struct{ value Value }

func (operand literalOperand) resolve(Context) Value    { return operand.value.clone() }
func (operand literalOperand) staticKind() (Kind, bool) { return operand.value.kind, true }

// Literal returns an immutable literal operand.
func Literal(value Value) Operand { return literalOperand{value: value.clone()} }

type variableOperand struct{ path Path }

func (operand variableOperand) resolve(facts Context) Value { return facts.Lookup(operand.path) }
func (operand variableOperand) staticKind() (Kind, bool)    { return KindMissing, false }

// Variable resolves an explicit fact path during evaluation.
func Variable(path Path) Operand { return variableOperand{path: path} }

type comparisonPredicate struct {
	operator OperatorName
	left     Operand
	right    Operand
}

// Compare creates a typed binary comparison.
func Compare(operator OperatorName, left, right Operand) Predicate {
	return comparisonPredicate{operator: operator, left: left, right: right}
}

func (predicate comparisonPredicate) Evaluate(ctx context.Context, facts Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return evaluateBuiltin(predicate.operator, predicate.left.resolve(facts), predicate.right.resolve(facts))
}
