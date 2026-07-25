package ruleengine

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBuiltinOperatorTruthTable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		operator OperatorName
		left     Value
		right    Value
		want     bool
		wantCode Code
	}{
		{name: "equal bool", operator: OpEqual, left: Bool(false), right: Bool(false), want: true},
		{name: "unequal null", operator: OpNotEqual, left: Null(), right: Null(), want: false},
		{name: "missing is never equal", operator: OpEqual, left: Missing(), right: Missing(), want: false},
		{name: "integer lower boundary", operator: OpLessThan, left: Int(math.MinInt64), right: Int(0), want: true},
		{name: "integer less or equal", operator: OpLessOrEqual, left: Int(0), right: Int(0), want: true},
		{name: "float greater", operator: OpGreaterThan, left: Float(1.5), right: Float(1.25), want: true},
		{name: "string greater or equal unicode", operator: OpGreaterOrEqual, left: String("å"), right: String("z"), want: true},
		{name: "time order", operator: OpLessThan, left: Time(now), right: Time(now.Add(time.Second)), want: true},
		{name: "duration order", operator: OpGreaterThan, left: Duration(time.Second), right: Duration(time.Millisecond), want: true},
		{name: "membership", operator: OpIn, left: String("FI"), right: List(String("SE"), String("FI")), want: true},
		{name: "negative membership", operator: OpNotIn, left: String("NO"), right: List(String("SE"), String("FI")), want: true},
		{name: "list contains", operator: OpContains, left: List(Int(1), Int(2)), right: Int(2), want: true},
		{name: "string contains unicode", operator: OpContains, left: String("Helsinki 🧭"), right: String("🧭"), want: true},
		{name: "prefix", operator: OpStartsWith, left: String("express"), right: String("ex"), want: true},
		{name: "suffix", operator: OpEndsWith, left: String("express"), right: String("ss"), want: true},
		{name: "bounded regex", operator: OpMatches, left: String(strings.Repeat("a", 4_096) + "!"), right: String(`^(a+)+$`), want: false},
		{name: "type mismatch", operator: OpGreaterThan, left: Int(1), right: String("1"), wantCode: CodeTypeMismatch},
		{name: "unknown", operator: "unknown", left: Int(1), right: Int(1), wantCode: CodeUnknownOperator},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := evaluateBuiltin(test.operator, test.left, test.right)
			if test.wantCode != "" {
				if !IsCode(err, test.wantCode) {
					t.Fatalf("evaluateBuiltin() error = %v, want %s", err, test.wantCode)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("evaluateBuiltin() = %v, %v; want %v, nil", got, err, test.want)
			}
		})
	}
}

func TestCompilerRejectsUnsafeRegexAndNonFiniteLiterals(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	value := MustPath("fact", "value")
	pattern := MustPath("fact", "pattern")
	tests := []struct {
		name string
		when Predicate
		code Code
	}{
		{name: "dynamic pattern", when: Compare(OpMatches, Variable(value), Variable(pattern)), code: CodeInvalidRule},
		{name: "invalid pattern", when: Compare(OpMatches, Variable(value), Literal(String("["))), code: CodeInvalidRule},
		{name: "oversized pattern", when: Compare(OpMatches, Variable(value), Literal(String(strings.Repeat("a", limits.MaxRegexBytes+1)))), code: CodeLimitExceeded},
		{name: "nan", when: Compare(OpEqual, Literal(Float(math.NaN())), Literal(Float(math.NaN()))), code: CodeInvalidFact},
		{name: "infinity", when: Compare(OpEqual, Literal(Float(math.Inf(1))), Literal(Float(math.Inf(1)))), code: CodeInvalidFact},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			set := RuleSet{ID: "unsafe", Rules: []Rule{{ID: "unsafe", When: test.when}}}
			_, _, err := NewCompiler(limits).Compile(context.Background(), set)
			if !IsCode(err, test.code) {
				t.Fatalf("Compile() error = %v, want %s", err, test.code)
			}
		})
	}
}
