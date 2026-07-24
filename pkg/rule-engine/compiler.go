package ruleengine

import (
	"context"
	"sort"
	"unicode/utf8"
)

// Severity classifies a compile diagnostic.
type Severity uint8

const (
	// SeverityWarning identifies a non-blocking diagnostic.
	SeverityWarning Severity = iota
	// SeverityError identifies a blocking diagnostic.
	SeverityError
)

// Diagnostic describes a safe compile finding without operand values.
type Diagnostic struct {
	RuleID   RuleID
	Code     Code
	Severity Severity
	Message  string
}

// Compiler validates rule sets and produces immutable plans.
type Compiler struct {
	limits    Limits
	operators map[OperatorName]registeredOperator
}

// NewCompiler creates a compiler containing only built-in operators.
func NewCompiler(limits Limits) Compiler {
	return Compiler{limits: limits, operators: map[OperatorName]registeredOperator{}}
}

// NewCompilerWithOperators creates an isolated operator registry. Built-in
// names and duplicate custom names cannot be replaced.
func NewCompilerWithOperators(limits Limits, operators ...Operator) (Compiler, error) {
	if err := limits.validate(); err != nil {
		return Compiler{}, err
	}
	registered := make(map[OperatorName]registeredOperator, len(operators))
	for _, operator := range operators {
		if operator == nil || operator.Name() == "" || knownOperator(operator.Name()) {
			return Compiler{}, newError(CodeInvalidRule, "custom operator name is invalid")
		}
		if _, exists := registered[operator.Name()]; exists {
			return Compiler{}, newError(CodeInvalidRule, "custom operator name is duplicated")
		}
		signatures := append([]Signature(nil), operator.Signatures()...)
		if len(signatures) == 0 {
			return Compiler{}, newError(CodeInvalidRule, "custom operator has no signatures")
		}
		for _, signature := range signatures {
			if signature.Left > KindList || signature.Right > KindList ||
				signature.Left == KindMissing || signature.Right == KindMissing {
				return Compiler{}, newError(CodeInvalidRule, "custom operator signature is invalid")
			}
		}
		registered[operator.Name()] = registeredOperator{implementation: operator, signatures: signatures}
	}
	return Compiler{limits: limits, operators: registered}, nil
}

// Compile validates, copies, and deterministically orders a rule set.
func (compiler Compiler) Compile(ctx context.Context, set RuleSet) (Plan, []Diagnostic, error) {
	if err := compiler.limits.validate(); err != nil {
		return Plan{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, nil, err
	}
	if !validIdentifier(set.ID, compiler.limits.MaxIdentifierBytes) ||
		!validOptionalIdentifier(set.Namespace, compiler.limits.MaxIdentifierBytes) ||
		set.Strategy > ErrorOnMultiple {
		return Plan{}, nil, newError(CodeInvalidRule, "invalid rule set")
	}
	if len(set.Rules) > compiler.limits.MaxRules {
		return Plan{}, nil, newError(CodeLimitExceeded, "too many rules")
	}
	rules := make([]Rule, len(set.Rules))
	seen := make(map[RuleID]struct{}, len(set.Rules))
	derivedPaths := make(map[string]RuleID)
	derivedCount := 0
	for index, rule := range set.Rules {
		if err := ctx.Err(); err != nil {
			return Plan{}, nil, err
		}
		if !validIdentifier(string(rule.ID), compiler.limits.MaxIdentifierBytes) ||
			!validOptionalIdentifier(rule.Namespace, compiler.limits.MaxIdentifierBytes) || rule.When == nil {
			return compileFailure(rule.ID, CodeInvalidRule, "rule is incomplete")
		}
		if code := validateTags(rule.Tags, compiler.limits); code != "" {
			return compileFailure(rule.ID, code, "rule tags are invalid")
		}
		if _, exists := seen[rule.ID]; exists {
			return compileFailure(rule.ID, CodeDuplicateRule, "rule identifier is duplicated")
		}
		seen[rule.ID] = struct{}{}
		count := 0
		if err := validatePredicate(rule.When, compiler.limits, compiler.operators, 1, &count); err != nil {
			code := errorCode(err, CodeInvalidRule)
			return compileFailure(rule.ID, code, "rule proposition is invalid")
		}
		if len(rule.Derive) > compiler.limits.MaxDerivedFacts {
			return compileFailure(rule.ID, CodeLimitExceeded, "rule derives too many facts")
		}
		derivedCount += len(rule.Derive)
		if derivedCount > compiler.limits.MaxDerivedFacts {
			return compileFailure(rule.ID, CodeLimitExceeded, "rule set derives too many facts")
		}
		if _, err := NewContextWithLimits(compiler.limits, rule.Derive...); err != nil {
			return compileFailure(rule.ID, CodeInvalidFact, "derived fact is invalid")
		}
		for _, fact := range rule.Derive {
			if _, exists := derivedPaths[fact.Path.key]; exists {
				return compileFailure(rule.ID, CodeDuplicateFact, "multiple rules derive the same path")
			}
			derivedPaths[fact.Path.key] = rule.ID
		}
		rules[index] = cloneRule(rule)
	}
	if cycleRule := findDerivationCycle(rules); cycleRule != "" {
		return compileFailure(cycleRule, CodeCycle, "derived fact dependency cycle")
	}
	sort.Slice(rules, func(left, right int) bool {
		if rules[left].Priority != rules[right].Priority {
			return rules[left].Priority > rules[right].Priority
		}
		return rules[left].ID < rules[right].ID
	})
	return Plan{
		setID: set.ID, namespace: set.Namespace, strategy: set.Strategy,
		rules: rules, limits: compiler.limits,
		operators: cloneOperators(compiler.operators), requiredPaths: requiredPaths(rules),
	}, nil, nil
}

func validIdentifier(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !containsControl(value)
}

func validOptionalIdentifier(value string, maximum int) bool {
	return value == "" || validIdentifier(value, maximum)
}

func validateTags(tags []string, limits Limits) Code {
	if len(tags) > limits.MaxTags {
		return CodeLimitExceeded
	}
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if !validIdentifier(tag, limits.MaxTagBytes) {
			return CodeInvalidRule
		}
		if _, exists := seen[tag]; exists {
			return CodeInvalidRule
		}
		seen[tag] = struct{}{}
	}
	return ""
}

func compileFailure(ruleID RuleID, code Code, message string) (Plan, []Diagnostic, error) {
	diagnostic := Diagnostic{RuleID: ruleID, Code: code, Severity: SeverityError, Message: message}
	return Plan{}, []Diagnostic{diagnostic}, newError(code, message)
}

func validatePredicate(predicate Predicate, limits Limits, operators map[OperatorName]registeredOperator, depth int, count *int) error {
	if depth > limits.MaxASTDepth {
		return newError(CodeLimitExceeded, "proposition is too deep")
	}
	*count++
	if *count > limits.MaxOperands {
		return newError(CodeLimitExceeded, "too many proposition operands")
	}
	switch typed := predicate.(type) {
	case constantPredicate, PredicateFunc:
		return nil
	case existsPredicate:
		if !typed.path.valid() {
			return newError(CodeInvalidPath, "exists path is invalid")
		}
		return nil
	case comparisonPredicate:
		return validateStaticOperator(typed.operator, typed.left, typed.right, operators, limits)
	case allPredicate:
		if len(typed.children) == 0 {
			return newError(CodeInvalidRule, "all requires children")
		}
		for _, child := range typed.children {
			if child == nil {
				return newError(CodeInvalidRule, "nil child predicate")
			}
			if err := validatePredicate(child, limits, operators, depth+1, count); err != nil {
				return err
			}
		}
	case anyPredicate:
		if len(typed.children) == 0 {
			return newError(CodeInvalidRule, "any requires children")
		}
		for _, child := range typed.children {
			if child == nil {
				return newError(CodeInvalidRule, "nil child predicate")
			}
			if err := validatePredicate(child, limits, operators, depth+1, count); err != nil {
				return err
			}
		}
	case notPredicate:
		if typed.child == nil {
			return newError(CodeInvalidRule, "not requires a child")
		}
		return validatePredicate(typed.child, limits, operators, depth+1, count)
	default:
		return nil
	}
	return nil
}

func cloneRule(rule Rule) Rule {
	rule.Tags = append([]string(nil), rule.Tags...)
	rule.Derive = append([]Fact(nil), rule.Derive...)
	return rule
}

func findDerivationCycle(rules []Rule) RuleID {
	graph := make(map[string][]string)
	owners := make(map[string]RuleID)
	for _, rule := range rules {
		dependencies := predicatePaths(rule.When)
		for _, fact := range rule.Derive {
			owners[fact.Path.key] = rule.ID
			for _, dependency := range dependencies {
				graph[dependency] = append(graph[dependency], fact.Path.key)
			}
		}
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) string
	visit = func(path string) string {
		if visiting[path] {
			return path
		}
		if visited[path] {
			return ""
		}
		visiting[path] = true
		for _, next := range graph[path] {
			if cycle := visit(next); cycle != "" {
				return cycle
			}
		}
		visiting[path] = false
		visited[path] = true
		return ""
	}
	for path := range graph {
		if cycle := visit(path); cycle != "" {
			return owners[cycle]
		}
	}
	return ""
}

func predicatePaths(predicate Predicate) []string {
	switch typed := predicate.(type) {
	case existsPredicate:
		return []string{typed.path.key}
	case comparisonPredicate:
		paths := make([]string, 0, 2)
		if variable, ok := typed.left.(variableOperand); ok {
			paths = append(paths, variable.path.key)
		}
		if variable, ok := typed.right.(variableOperand); ok {
			paths = append(paths, variable.path.key)
		}
		return paths
	case allPredicate:
		return childPaths(typed.children)
	case anyPredicate:
		return childPaths(typed.children)
	case notPredicate:
		return predicatePaths(typed.child)
	default:
		return nil
	}
}

func childPaths(children []Predicate) []string {
	paths := make([]string, 0, len(children)*2)
	for _, child := range children {
		paths = append(paths, predicatePaths(child)...)
	}
	return paths
}

func cloneOperators(operators map[OperatorName]registeredOperator) map[OperatorName]registeredOperator {
	cloned := make(map[OperatorName]registeredOperator, len(operators))
	for name, operator := range operators {
		operator.signatures = append([]Signature(nil), operator.signatures...)
		cloned[name] = operator
	}
	return cloned
}

func requiredPaths(rules []Rule) []Path {
	unique := make(map[string]Path)
	for _, rule := range rules {
		collectPredicatePaths(rule.When, unique)
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	paths := make([]Path, len(keys))
	for index, key := range keys {
		paths[index] = unique[key]
	}
	return paths
}

func collectPredicatePaths(predicate Predicate, paths map[string]Path) {
	switch typed := predicate.(type) {
	case existsPredicate:
		paths[typed.path.key] = typed.path
	case comparisonPredicate:
		if variable, ok := typed.left.(variableOperand); ok {
			paths[variable.path.key] = variable.path
		}
		if variable, ok := typed.right.(variableOperand); ok {
			paths[variable.path.key] = variable.path
		}
	case allPredicate:
		for _, child := range typed.children {
			collectPredicatePaths(child, paths)
		}
	case anyPredicate:
		for _, child := range typed.children {
			collectPredicatePaths(child, paths)
		}
	case notPredicate:
		collectPredicatePaths(typed.child, paths)
	}
}
