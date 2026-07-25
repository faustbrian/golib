package ruleengine

import (
	"math"
	"unicode/utf8"
)

// Owner describes which integration supplied a fact. It has no authorization
// semantics.
type Owner uint8

const (
	// OwnerUnspecified records facts without supplied provenance.
	OwnerUnspecified Owner = iota
	// OwnerSubject records facts supplied by the evaluated subject.
	OwnerSubject
	// OwnerResource records facts supplied by the evaluated resource.
	OwnerResource
	// OwnerEnvironment records facts supplied by the environment.
	OwnerEnvironment
)

// Fact associates a typed value with an explicit path and owner.
type Fact struct {
	Path  Path
	Value Value
	Owner Owner
}

// Context is an immutable snapshot of supplied facts.
type Context struct {
	facts map[string]Fact
}

// NewContext builds a context with DefaultLimits.
func NewContext(facts ...Fact) (Context, error) {
	return NewContextWithLimits(DefaultLimits(), facts...)
}

// NewContextWithLimits validates and copies all facts.
func NewContextWithLimits(limits Limits, facts ...Fact) (Context, error) {
	if err := limits.validate(); err != nil {
		return Context{}, err
	}
	if len(facts) > limits.MaxFacts {
		return Context{}, newError(CodeLimitExceeded, "too many facts")
	}
	result := Context{facts: make(map[string]Fact, len(facts))}
	for _, fact := range facts {
		if !fact.Path.valid() || fact.Value.kind == KindMissing || fact.Owner > OwnerEnvironment {
			return Context{}, newError(CodeInvalidFact, "invalid fact")
		}
		if _, exists := result.facts[fact.Path.key]; exists {
			return Context{}, newError(CodeDuplicateFact, "duplicate fact path")
		}
		if err := validateValue(fact.Value, limits, 0); err != nil {
			return Context{}, err
		}
		fact.Path.segments = append([]string(nil), fact.Path.segments...)
		fact.Value = fact.Value.clone()
		result.facts[fact.Path.key] = fact
	}
	return result, nil
}

// Lookup returns Missing when the path was not supplied.
func (c Context) Lookup(path Path) Value {
	fact, ok := c.facts[path.key]
	if !ok {
		return Missing()
	}
	return fact.Value.clone()
}

// Owner returns the supplied owner and whether the fact exists.
func (c Context) Owner(path Path) (Owner, bool) {
	fact, ok := c.facts[path.key]
	return fact.Owner, ok
}

func (c Context) withFact(fact Fact) Context {
	cloned := Context{facts: make(map[string]Fact, len(c.facts)+1)}
	for key, existing := range c.facts {
		cloned.facts[key] = existing
	}
	fact.Path.segments = append([]string(nil), fact.Path.segments...)
	fact.Value = fact.Value.clone()
	cloned.facts[fact.Path.key] = fact
	return cloned
}

func validateValue(value Value, limits Limits, depth int) error {
	if depth > limits.MaxASTDepth {
		return newError(CodeInvalidFact, "value nesting is too deep")
	}
	if value.kind > KindList {
		return newError(CodeInvalidFact, "unknown value type")
	}
	if text, ok := value.StringValue(); ok {
		if !utf8.ValidString(text) {
			return newError(CodeInvalidFact, "string is not valid UTF-8")
		}
		if len(text) > limits.MaxStringBytes {
			return newError(CodeInvalidFact, "string is too large")
		}
	}
	if number, ok := value.FloatValue(); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
		return newError(CodeInvalidFact, "float must be finite")
	}
	if values, ok := value.ListValue(); ok {
		if len(values) > limits.MaxCollection {
			return newError(CodeInvalidFact, "collection is too large")
		}
		for _, item := range values {
			if err := validateValue(item, limits, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
