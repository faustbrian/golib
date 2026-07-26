// Package jsonast exposes the bounded versioned JSON rule-definition DSL.
// The grammar is the canonical representation implemented by ruleengine;
// unknown or variant-incompatible fields are rejected.
package jsonast

import ruleengine "github.com/faustbrian/golib/pkg/rule-engine"

// Parse decodes and validates a JSON AST definition.
func Parse(data []byte, limits ruleengine.Limits) (ruleengine.RuleSet, []ruleengine.Diagnostic, error) {
	return ruleengine.ParseJSON(data, limits)
}

// Marshal returns the canonical JSON AST representation.
func Marshal(set ruleengine.RuleSet) ([]byte, error) {
	return ruleengine.MarshalCanonical(set)
}
