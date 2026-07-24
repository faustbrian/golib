package abac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func TestDocumentDecodeEncodeAndEvaluate(t *testing.T) {
	t.Parallel()

	stringValue := "finance"
	document := Document{
		Version: DocumentVersion,
		Limits:  Limits{MaxRules: 10, MaxNamedConditions: 50},
		NamedConditions: []NamedConditionDocument{
			{Name: "gate", Version: 1, Condition: conditionDocument(OperatorEqual, SourceSubject, ValueDocument{Kind: ValueString, String: stringValue})},
			{Name: "null", Version: 1, Condition: conditionDocument(OperatorEqual, SourceResource, ValueDocument{Kind: ValueNull})},
			{Name: "bool", Version: 1, Condition: conditionDocument(OperatorEqual, SourceRequest, ValueDocument{Kind: ValueBool, Bool: true})},
			{Name: "int", Version: 1, Condition: conditionDocument(OperatorEqual, SourceEnvironment, ValueDocument{Kind: ValueInt, Int: 7})},
			{Name: "float", Version: 1, Condition: conditionDocument(OperatorEqual, SourceSubject, ValueDocument{Kind: ValueFloat, Float: 1.5})},
			{Name: "time", Version: 1, Condition: conditionDocument(OperatorEqual, SourceSubject, ValueDocument{Kind: ValueTime, Time: "2026-07-15T10:00:00Z"})},
			{Name: "ip-value", Version: 1, Condition: conditionDocument(OperatorEqual, SourceSubject, ValueDocument{Kind: ValueIP, IP: "192.0.2.1"})},
			{Name: "set-value", Version: 1, Condition: conditionDocument(OperatorEqual, SourceSubject, ValueDocument{Kind: ValueStringSet, StringSet: []string{"a", "b"}})},
			{Name: "exists", Version: 1, Condition: ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"}},
			{Name: "is-null", Version: 1, Condition: ConditionDocument{Operator: OperatorIsNull, Source: SourceSubject, Attribute: "value"}},
			{Name: "all", Version: 1, Condition: ConditionDocument{Operator: OperatorAll, Conditions: []ConditionDocument{{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"}}}},
			{Name: "any", Version: 1, Condition: ConditionDocument{Operator: OperatorAny, Conditions: []ConditionDocument{{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"}}}},
			{Name: "not", Version: 1, Condition: ConditionDocument{Operator: OperatorNot, Condition: &ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"}}},
			{Name: "greater", Version: 1, Condition: conditionDocument(OperatorGreaterThan, SourceSubject, ValueDocument{Kind: ValueInt, Int: 1})},
			{Name: "less", Version: 1, Condition: conditionDocument(OperatorLessThan, SourceSubject, ValueDocument{Kind: ValueInt, Int: 10})},
			{Name: "in", Version: 1, Condition: ConditionDocument{Operator: OperatorIn, Source: SourceSubject, Attribute: "value", Values: []ValueDocument{{Kind: ValueString, String: "a"}, {Kind: ValueString, String: "b"}}}},
			{Name: "set-contains", Version: 1, Condition: ConditionDocument{Operator: OperatorSetContains, Source: SourceSubject, Attribute: "value", Text: "a"}},
			{Name: "prefix", Version: 1, Condition: ConditionDocument{Operator: OperatorHasPrefix, Source: SourceSubject, Attribute: "value", Text: "a"}},
			{Name: "suffix", Version: 1, Condition: ConditionDocument{Operator: OperatorHasSuffix, Source: SourceSubject, Attribute: "value", Text: "a"}},
			{Name: "contains", Version: 1, Condition: ConditionDocument{Operator: OperatorStringContains, Source: SourceSubject, Attribute: "value", Text: "a"}},
			{Name: "cidr", Version: 1, Condition: ConditionDocument{Operator: OperatorIPIn, Source: SourceSubject, Attribute: "value", Prefix: "192.0.2.0/24"}},
		},
		Rules: []RuleDocument{{
			ID: "finance-reader", Action: "read", ResourceType: "report",
			Effect: EffectAllow, ConditionName: "gate", ConditionVersion: 1,
		}, {
			ID: "deny-other", Action: "write", ResourceType: "report",
			Effect:    EffectDeny,
			Condition: &ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "department"},
		}},
	}
	encoded, err := EncodeDocument(document)
	if err != nil {
		t.Fatalf("EncodeDocument() error = %v", err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("EncodeDocument() = %s", encoded)
	}
	evaluator, err := DecodeDocument(encoded)
	if err != nil {
		t.Fatalf("DecodeDocument() error = %v", err)
	}
	decision, err := evaluator.Evaluate(context.Background(), authorization.Request{
		Subject: authorization.Subject{Attributes: authorization.Attributes{
			"department": authorization.StringValue("finance"),
		}},
		Action: "read", Resource: authorization.Resource{Type: "report"},
	})
	if err != nil || decision.Outcome != authorization.Allow {
		t.Fatalf("Evaluate() = (%+v, %v), want allow", decision, err)
	}
	compiled, err := (Decoder{}).Decode(encoded)
	if err != nil || compiled == nil {
		t.Fatalf("Decoder.Decode() = (%v, %v), want evaluator", compiled, err)
	}
}

func TestConditionDocumentPreservesOperatorSemantics(t *testing.T) {
	t.Parallel()

	request := authorization.Request{
		Subject: authorization.Subject{
			Attributes: authorization.Attributes{
				"value":      authorization.IntValue(7),
				"department": authorization.IntValue(7),
			},
		},
	}
	exists := ConditionDocument{
		Operator: OperatorExists, Source: SourceSubject, Attribute: "value",
	}
	missing := ConditionDocument{
		Operator: OperatorExists, Source: SourceSubject, Attribute: "missing",
	}
	all, err := (ConditionDocument{
		Operator: OperatorAll, Conditions: []ConditionDocument{exists, missing},
	}).build()
	if err != nil {
		t.Fatalf("ConditionDocument.build(all) error = %v", err)
	}
	result, err := EvaluateCondition(context.Background(), all, request, Limits{})
	if err != nil {
		t.Fatalf("EvaluateCondition(all) error = %v", err)
	}
	if result.Matched {
		t.Errorf("EvaluateCondition(all) = %+v, want non-match", result)
	}

	greater, err := conditionDocument(
		OperatorGreaterThan,
		SourceSubject,
		ValueDocument{Kind: ValueInt, Int: 5},
	).build()
	if err != nil {
		t.Fatalf("ConditionDocument.build(greater) error = %v", err)
	}
	result, err = EvaluateCondition(context.Background(), greater, request, Limits{})
	if err != nil {
		t.Fatalf("EvaluateCondition(greater) error = %v", err)
	}
	if !result.Matched {
		t.Errorf("EvaluateCondition(greater) = %+v, want match", result)
	}
}

func TestDocumentRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		encoded string
		want    error
	}{
		"malformed":     {`{`, ErrInvalidDocument},
		"unknown":       {`{"version":1,"rules":[],"named_conditions":[],"extra":true}`, ErrInvalidDocument},
		"trailing":      {`{"version":1,"rules":[],"named_conditions":[]} {}`, ErrInvalidDocument},
		"version":       {`{"version":2,"rules":[],"named_conditions":[]}`, ErrUnsupportedDocumentVersion},
		"operator":      {ruleDocumentJSON(`{"operator":"unknown","source":"subject","attribute":"x"}`, "allow"), ErrInvalidDocument},
		"source":        {ruleDocumentJSON(`{"operator":"exists","source":"unknown","attribute":"x"}`, "allow"), ErrInvalidDocument},
		"attribute":     {ruleDocumentJSON(`{"operator":"exists","source":"subject"}`, "allow"), ErrInvalidDocument},
		"missing value": {ruleDocumentJSON(`{"operator":"equal","source":"subject","attribute":"x"}`, "allow"), ErrInvalidDocument},
		"value kind":    {ruleDocumentJSON(`{"operator":"equal","source":"subject","attribute":"x","value":{"kind":"unknown"}}`, "allow"), ErrInvalidDocument},
		"time":          {ruleDocumentJSON(`{"operator":"equal","source":"subject","attribute":"x","value":{"kind":"time","time":"bad"}}`, "allow"), ErrInvalidDocument},
		"ip":            {ruleDocumentJSON(`{"operator":"equal","source":"subject","attribute":"x","value":{"kind":"ip","ip":"bad"}}`, "allow"), ErrInvalidDocument},
		"prefix":        {ruleDocumentJSON(`{"operator":"ip_in","source":"subject","attribute":"x","prefix":"bad"}`, "allow"), ErrInvalidDocument},
		"all child":     {ruleDocumentJSON(`{"operator":"all","conditions":[{"operator":"unknown","source":"subject","attribute":"x"}]}`, "allow"), ErrInvalidDocument},
		"not missing":   {ruleDocumentJSON(`{"operator":"not"}`, "allow"), ErrInvalidDocument},
		"not child":     {ruleDocumentJSON(`{"operator":"not","condition":{"operator":"unknown","source":"subject","attribute":"x"}}`, "allow"), ErrInvalidDocument},
		"in value":      {ruleDocumentJSON(`{"operator":"in","source":"subject","attribute":"x","values":[{"kind":"unknown"}]}`, "allow"), ErrInvalidDocument},
		"named":         {`{"version":1,"rules":[],"named_conditions":[{"name":"bad","version":1,"condition":{"operator":"unknown","source":"subject","attribute":"x"}}]}`, ErrInvalidDocument},
		"effect":        {ruleDocumentJSON(`{"operator":"exists","source":"subject","attribute":"x"}`, "maybe"), ErrInvalidDocument},
		"rule":          {`{"version":1,"rules":[{"id":"","action":"read","resource_type":"doc","effect":"allow","condition":{"operator":"exists","source":"subject","attribute":"x"}}],"named_conditions":[]}`, ErrInvalidRule},
		"limit":         {`{"version":1,"limits":{"max_rules":1},"rules":[{"id":"a","action":"read","resource_type":"doc","effect":"allow","condition":{"operator":"exists","source":"subject","attribute":"x"}},{"id":"b","action":"read","resource_type":"doc","effect":"allow","condition":{"operator":"exists","source":"subject","attribute":"x"}}],"named_conditions":[]}`, ErrRuleLimitExceeded},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeDocument([]byte(tt.encoded)); !errors.Is(err, tt.want) {
				t.Errorf("DecodeDocument() error = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := EncodeDocument(Document{Version: 2}); !errors.Is(err, ErrUnsupportedDocumentVersion) {
		t.Errorf("EncodeDocument(invalid) error = %v, want version error", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes+1)); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
	if _, err := DecodeDocument(bytes.Repeat([]byte{'x'}, maxEncodedDocumentBytes)); errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("DecodeDocument(at limit) error = %v, do not want ErrDocumentLimitExceeded", err)
	}
	atLimit := Document{Version: DocumentVersion, Rules: []RuleDocument{{
		ID: "rule", Action: "x", ResourceType: "document", Effect: EffectAllow,
		Condition: &ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"},
	}}}
	baseline, err := json.MarshalIndent(atLimit, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(at limit baseline) error = %v", err)
	}
	atLimit.Rules[0].Action = authorization.Action(strings.Repeat("x", maxEncodedDocumentBytes-len(baseline)+1))
	encoded, err := EncodeDocument(atLimit)
	if err != nil {
		t.Fatalf("EncodeDocument(at limit) error = %v", err)
	}
	if len(encoded) != maxEncodedDocumentBytes {
		t.Errorf("len(EncodeDocument(at limit)) = %d, want %d", len(encoded), maxEncodedDocumentBytes)
	}
	oversized := Document{Version: DocumentVersion, Rules: []RuleDocument{{
		ID: "rule", Action: authorization.Action(strings.Repeat("x", maxEncodedDocumentBytes)),
		ResourceType: "document", Effect: EffectAllow,
		Condition: &ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "value"},
	}}}
	if _, err := EncodeDocument(oversized); !errors.Is(err, ErrDocumentLimitExceeded) {
		t.Errorf("EncodeDocument(oversized) error = %v, want ErrDocumentLimitExceeded", err)
	}
}

func conditionDocument(operator, source string, value ValueDocument) ConditionDocument {
	return ConditionDocument{
		Operator: operator, Source: source, Attribute: "department", Value: &value,
	}
}

func ruleDocumentJSON(condition, effect string) string {
	return `{"version":1,"rules":[{"id":"rule","action":"read","resource_type":"doc","effect":"` + effect + `","condition":` + condition + `}],"named_conditions":[]}`
}

func TestValueDocumentTimeUsesUTC(t *testing.T) {
	t.Parallel()

	value, err := (ValueDocument{Kind: ValueTime, Time: "2026-07-15T13:00:00+03:00"}).build()
	if err != nil {
		t.Fatalf("ValueDocument.build() error = %v", err)
	}
	got, _ := value.Time()
	if !got.Equal(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("built time = %v", got)
	}
}

type trackingCondition struct {
	called *bool
}

func (condition trackingCondition) evaluate(*evaluationState) (Result, error) {
	*condition.called = true
	return Result{}, nil
}

func (condition trackingCondition) validate() error {
	*condition.called = true
	return nil
}

func TestConditionValidationAppliesBoundsBeforeDescending(t *testing.T) {
	t.Parallel()

	called := false
	condition := Not(Not(trackingCondition{called: &called}))
	_, err := New([]Rule{{
		ID: "bounded", Action: "read", ResourceType: "document",
		Effect: authorization.Allow, Condition: condition,
	}}, nil, WithLimits(Limits{MaxDepth: 1}))
	if !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("New(deep condition) error = %v, want ErrDepthExceeded", err)
	}
	if called {
		t.Error("condition validation descended beyond MaxDepth")
	}

	atDepthLimit := Not(Exists(Reference{Source: Subject, Name: "department"}))
	if _, err := New([]Rule{{
		ID: "exact-depth", Action: "read", ResourceType: "document",
		Effect: authorization.Allow, Condition: atDepthLimit,
	}}, nil, WithLimits(Limits{MaxDepth: 2})); err != nil {
		t.Errorf("New(condition at exact depth) error = %v", err)
	}

	setOne := authorization.StringSetValue([]string{"one"})
	aggregate := All(
		Equal(Reference{Source: Subject, Name: "first"}, setOne),
		Equal(Reference{Source: Subject, Name: "second"}, setOne),
	)
	if _, err := New([]Rule{{
		ID: "exact-set", Action: "read", ResourceType: "document",
		Effect: authorization.Allow, Condition: aggregate,
	}}, nil, WithLimits(Limits{MaxSetSize: 2})); err != nil {
		t.Errorf("New(condition at exact aggregate set size) error = %v", err)
	}
	if _, err := New([]Rule{{
		ID: "over-set", Action: "read", ResourceType: "document",
		Effect: authorization.Allow, Condition: aggregate,
	}}, nil, WithLimits(Limits{MaxSetSize: 1})); !errors.Is(err, ErrSetLimitExceeded) {
		t.Errorf("New(condition over aggregate set size) error = %v, want ErrSetLimitExceeded", err)
	}
}

func TestDocumentBuildAppliesDepthBeforeNestedSemantics(t *testing.T) {
	t.Parallel()

	invalid := ConditionDocument{Operator: "invalid"}
	document := Document{
		Version: DocumentVersion,
		Limits:  Limits{MaxDepth: 1},
		Rules: []RuleDocument{{
			ID: "deep", Action: "read", ResourceType: "document", Effect: EffectAllow,
			Condition: &ConditionDocument{Operator: OperatorNot, Condition: &invalid},
		}},
	}
	if _, err := document.Build(); !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("Document.Build(deep condition) error = %v, want ErrDepthExceeded", err)
	}

	document.Rules = nil
	document.NamedConditions = []NamedConditionDocument{{
		Name: "deep", Version: 1,
		Condition: ConditionDocument{Operator: OperatorNot, Condition: &invalid},
	}}
	if _, err := document.Build(); !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("Document.Build(deep named condition) error = %v, want ErrDepthExceeded", err)
	}

	leaf := ConditionDocument{Operator: OperatorExists, Source: SourceSubject, Attribute: "department"}
	document.NamedConditions = nil
	document.Rules = []RuleDocument{{
		ID: "exact-depth", Action: "read", ResourceType: "document", Effect: EffectAllow,
		Condition: &ConditionDocument{Operator: OperatorNot, Condition: &leaf},
	}}
	document.Limits.MaxDepth = 2
	if _, err := document.Build(); err != nil {
		t.Errorf("Document.Build(condition at exact depth) error = %v", err)
	}
}
