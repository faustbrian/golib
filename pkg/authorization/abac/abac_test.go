package abac_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/abac"
)

func TestEvaluatorUsesTypedAttributeSources(t *testing.T) {
	t.Parallel()

	condition := abac.All(
		abac.Equal(
			abac.Reference{Source: abac.Subject, Name: "department"},
			authorization.StringValue("finance"),
		),
		abac.Equal(
			abac.Reference{Source: abac.Resource, Name: "classification"},
			authorization.StringValue("internal"),
		),
		abac.Equal(
			abac.Reference{Source: abac.Request, Name: "mfa"},
			authorization.BoolValue(true),
		),
		abac.Equal(
			abac.Reference{Source: abac.Environment, Name: "risk"},
			authorization.IntValue(2),
		),
	)
	evaluator, err := abac.New([]abac.Rule{
		{
			ID:           "finance-read",
			Action:       "document.read",
			ResourceType: "document",
			Tenant:       "tenant-1",
			Effect:       authorization.Allow,
			Condition:    condition,
		},
	}, nil)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}

	decision, err := evaluator.Evaluate(context.Background(), attributedRequest())
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Allow {
		t.Errorf("Decision.Outcome = %v, want Allow", decision.Outcome)
	}
	if len(decision.MatchedPolicyIDs) != 1 || decision.MatchedPolicyIDs[0] != "finance-read" {
		t.Errorf("Decision.MatchedPolicyIDs = %v, want finance-read", decision.MatchedPolicyIDs)
	}
}

func TestConditionMissingNullAndTypeMismatchSemantics(t *testing.T) {
	t.Parallel()

	request := attributedRequest()
	tests := map[string]struct {
		condition abac.Condition
		want      abac.Status
	}{
		"missing": {
			condition: abac.Equal(
				abac.Reference{Source: abac.Subject, Name: "missing"},
				authorization.StringValue("value"),
			),
			want: abac.StatusMissing,
		},
		"null": {
			condition: abac.Equal(
				abac.Reference{Source: abac.Subject, Name: "manager"},
				authorization.StringValue("alice"),
			),
			want: abac.StatusNull,
		},
		"type mismatch": {
			condition: abac.Equal(
				abac.Reference{Source: abac.Subject, Name: "department"},
				authorization.IntValue(1),
			),
			want: abac.StatusTypeMismatch,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := abac.EvaluateCondition(
				context.Background(),
				tt.condition,
				request,
				abac.Limits{},
			)
			if err != nil {
				t.Fatalf("abac.EvaluateCondition() error = %v", err)
			}
			if result.Matched || result.Status != tt.want {
				t.Errorf("condition result = %+v, want status %v", result, tt.want)
			}
		})
	}
}

func TestBoundedConditionOperators(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	request := attributedRequest()
	request.Attributes["age"] = authorization.IntValue(42)
	request.Attributes["score"] = authorization.MustFloatValue(9.5)
	request.Attributes["created_at"] = authorization.TimeValue(now)
	request.Attributes["ip"] = authorization.IPValue(netip.MustParseAddr("192.0.2.10"))
	request.Attributes["tags"] = authorization.StringSetValue([]string{"finance", "trusted"})
	request.Attributes["name"] = authorization.StringValue("Ålice Example")

	ref := func(name authorization.AttributeName) abac.Reference {
		return abac.Reference{Source: abac.Request, Name: name}
	}

	tests := map[string]abac.Condition{
		"exists":          abac.Exists(ref("age")),
		"is null":         abac.IsNull(abac.Reference{Source: abac.Subject, Name: "manager"}),
		"any":             abac.Any(abac.Equal(ref("age"), authorization.IntValue(0)), abac.Exists(ref("age"))),
		"not":             abac.Not(abac.Equal(ref("age"), authorization.IntValue(0))),
		"greater integer": abac.GreaterThan(ref("age"), authorization.IntValue(18)),
		"less float":      abac.LessThan(ref("score"), authorization.MustFloatValue(10)),
		"greater time":    abac.GreaterThan(ref("created_at"), authorization.TimeValue(now.Add(-time.Hour))),
		"in":              abac.In(ref("age"), authorization.IntValue(21), authorization.IntValue(42)),
		"set contains":    abac.SetContains(ref("tags"), "trusted"),
		"prefix":          abac.HasPrefix(ref("name"), "Ålice"),
		"suffix":          abac.HasSuffix(ref("name"), "Example"),
		"string contains": abac.StringContains(ref("name"), "ice Ex"),
		"CIDR":            abac.IPIn(ref("ip"), netip.MustParsePrefix("192.0.2.0/24")),
	}

	for name, condition := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := abac.EvaluateCondition(
				context.Background(), condition, request, abac.Limits{},
			)
			if err != nil {
				t.Fatalf("abac.EvaluateCondition() error = %v", err)
			}
			if !result.Matched || result.Status != abac.StatusMatch {
				t.Errorf("condition result = %+v, want match", result)
			}
		})
	}

	missingNot, err := abac.EvaluateCondition(
		context.Background(),
		abac.Not(abac.Equal(ref("missing"), authorization.StringValue("value"))),
		request,
		abac.Limits{},
	)
	if err != nil {
		t.Fatalf("missing Not() error = %v", err)
	}
	if missingNot.Matched || missingNot.Status != abac.StatusMissing {
		t.Errorf("missing Not() = %+v, want missing non-match", missingNot)
	}
}

func TestNamedConditionsAreVersionedAndReusable(t *testing.T) {
	t.Parallel()

	named := []abac.NamedCondition{
		{
			Name:      "mfa-present",
			Version:   1,
			Condition: abac.Exists(abac.Reference{Source: abac.Request, Name: "mfa"}),
		},
	}
	rules := []abac.Rule{
		{
			ID:               "named-allow",
			Priority:         10,
			Tenant:           "tenant-1",
			Action:           "document.read",
			ResourceType:     "document",
			Effect:           authorization.Allow,
			ConditionName:    "mfa-present",
			ConditionVersion: 1,
		},
		{
			ID:           "risk-deny",
			Priority:     1,
			Tenant:       "tenant-1",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Deny,
			Condition: abac.Equal(
				abac.Reference{Source: abac.Environment, Name: "risk"},
				authorization.IntValue(2),
			),
		},
	}

	evaluator, err := abac.New(rules, named)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	decision, err := evaluator.Evaluate(context.Background(), attributedRequest())
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != abac.ReasonExplicitDeny {
		t.Errorf("Decision = %+v, want explicit deny", decision)
	}
	if len(decision.MatchedPolicyIDs) != 2 ||
		decision.MatchedPolicyIDs[0] != "named-allow" ||
		decision.MatchedPolicyIDs[1] != "risk-deny" {
		t.Errorf("Decision.MatchedPolicyIDs = %v, want priority order", decision.MatchedPolicyIDs)
	}

	rules[0].ConditionVersion = 2
	_, err = abac.New(rules[:1], named)
	if !errors.Is(err, abac.ErrUnknownNamedCondition) {
		t.Errorf("unknown named condition error = %v, want ErrUnknownNamedCondition", err)
	}

	rules[0].ConditionVersion = 1
	if _, err := abac.New(
		rules[:1],
		named,
		abac.WithLimits(abac.Limits{MaxNamedConditions: 1}),
	); err != nil {
		t.Errorf("abac.New(at named condition limit) error = %v", err)
	}
}

func TestEvaluatorOrderingIsDeterministic(t *testing.T) {
	t.Parallel()

	condition := abac.Exists(abac.Reference{Source: abac.Request, Name: "mfa"})
	rules := []abac.Rule{
		{ID: "low-a", Priority: 1, Tenant: "tenant-1", Action: "document.read", ResourceType: "document", Effect: authorization.Allow, Condition: condition},
		{ID: "tie-z", Priority: 2, Tenant: "tenant-1", Action: "document.read", ResourceType: "document", Effect: authorization.Allow, Condition: condition},
		{ID: "tie-a", Priority: 2, Tenant: "tenant-1", Action: "document.read", ResourceType: "document", Effect: authorization.Allow, Condition: condition},
	}
	evaluator, err := abac.New(rules, nil)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	decision, err := evaluator.Evaluate(context.Background(), attributedRequest())
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	want := []authorization.PolicyID{"tie-a", "tie-z", "low-a"}
	if !slices.Equal(decision.MatchedPolicyIDs, want) {
		t.Errorf("Decision.MatchedPolicyIDs = %v, want %v", decision.MatchedPolicyIDs, want)
	}
}

func TestEvaluatorFailsClosedOnLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	condition := abac.All(
		abac.Exists(abac.Reference{Source: abac.Request, Name: "mfa"}),
		abac.Exists(abac.Reference{Source: abac.Subject, Name: "department"}),
	)
	rules := []abac.Rule{
		{
			ID:           "first",
			Tenant:       "tenant-1",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
			Condition:    condition,
		},
		{
			ID:           "second",
			Tenant:       "tenant-1",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
			Condition:    condition,
		},
	}

	if _, err := abac.New(rules, nil, abac.WithLimits(abac.Limits{MaxRules: 1})); !errors.Is(err, abac.ErrRuleLimitExceeded) {
		t.Errorf("rule-limited abac.New() error = %v, want ErrRuleLimitExceeded", err)
	}
	if _, err := abac.New(
		rules[:1], nil, abac.WithLimits(abac.Limits{MaxRules: 1}),
	); err != nil {
		t.Errorf("abac.New(at rule limit) error = %v", err)
	}
	if _, err := abac.New(
		rules[:1], nil, abac.WithLimits(abac.Limits{MaxDepth: 1}),
	); !errors.Is(err, abac.ErrDepthExceeded) {
		t.Errorf("depth-limited abac.New() error = %v, want ErrDepthExceeded", err)
	}
	if _, err := abac.New(
		[]abac.Rule{{
			ID:           "set",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
			Condition: abac.In(
				abac.Reference{Source: abac.Request, Name: "age"},
				authorization.IntValue(1),
				authorization.IntValue(2),
			),
		}},
		nil,
		abac.WithLimits(abac.Limits{MaxSetSize: 1}),
	); !errors.Is(err, abac.ErrSetLimitExceeded) {
		t.Errorf("set-limited abac.New() error = %v, want ErrSetLimitExceeded", err)
	}

	costLimited, err := abac.New(
		rules[:1], nil, abac.WithLimits(abac.Limits{MaxCost: 1}),
	)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	decision, err := costLimited.Evaluate(context.Background(), attributedRequest())
	if !errors.Is(err, abac.ErrCostExceeded) {
		t.Fatalf("cost-limited Evaluate() error = %v, want ErrCostExceeded", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != abac.ReasonLimitExceeded {
		t.Errorf("cost-limited Decision = %+v, want limit deny", decision)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err = costLimited.Evaluate(ctx, attributedRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Evaluate() error = %v, want context.Canceled", err)
	}
	if decision.Reason != authorization.ReasonContextCanceled {
		t.Errorf("canceled Decision = %+v, want cancellation reason", decision)
	}

	matchLimited, err := abac.New(
		rules, nil, abac.WithLimits(abac.Limits{MaxMatches: 1, MaxBatchSize: 1}),
	)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	decision, err = matchLimited.Evaluate(context.Background(), attributedRequest())
	if !errors.Is(err, abac.ErrMatchLimitExceeded) {
		t.Fatalf("match-limited Evaluate() error = %v, want ErrMatchLimitExceeded", err)
	}
	if decision.Outcome != authorization.Deny {
		t.Errorf("match-limited Decision.Outcome = %v, want Deny", decision.Outcome)
	}

	exactSetRequest := attributedRequest()
	exactSetRequest.Attributes["set"] = authorization.StringSetValue([]string{"one"})
	result, err := abac.EvaluateCondition(
		context.Background(),
		abac.SetContains(
			abac.Reference{Source: abac.Request, Name: "set"},
			"one",
		),
		exactSetRequest,
		abac.Limits{MaxSetSize: 1},
	)
	if err != nil || !result.Matched {
		t.Errorf("EvaluateCondition(at set limit) = (%+v, %v), want match", result, err)
	}

	_, err = matchLimited.EvaluateBatch(
		context.Background(),
		[]authorization.Request{attributedRequest(), attributedRequest()},
	)
	if !errors.Is(err, abac.ErrBatchLimitExceeded) {
		t.Errorf("oversized EvaluateBatch() error = %v, want ErrBatchLimitExceeded", err)
	}
	if _, err := matchLimited.EvaluateBatch(
		context.Background(),
		[]authorization.Request{attributedRequest()},
	); !errors.Is(err, abac.ErrMatchLimitExceeded) {
		t.Errorf("EvaluateBatch(at batch limit) error = %v, want ErrMatchLimitExceeded", err)
	}
}

func TestEvaluateConditionRejectsMalformedConditions(t *testing.T) {
	t.Parallel()

	tests := map[string]abac.Condition{
		"nil":       nil,
		"empty in":  abac.In(abac.Reference{Source: abac.Request, Name: "age"}),
		"empty all": abac.All(),
		"empty any": abac.Any(),
		"nil not":   abac.Not(nil),
	}

	for name, condition := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := abac.EvaluateCondition(
				context.Background(), condition, attributedRequest(), abac.Limits{},
			)
			if !errors.Is(err, abac.ErrInvalidCondition) {
				t.Errorf("EvaluateCondition() error = %v, want ErrInvalidCondition", err)
			}
		})
	}
}

func TestNewRejectsInvalidRulesAndNamedConditions(t *testing.T) {
	t.Parallel()

	valid := abac.Rule{
		ID:           "rule",
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
		Condition: abac.Exists(
			abac.Reference{Source: abac.Request, Name: "mfa"},
		),
	}

	invalidRules := map[string]func(*abac.Rule){
		"id":            func(rule *abac.Rule) { rule.ID = "" },
		"action":        func(rule *abac.Rule) { rule.Action = "" },
		"resource type": func(rule *abac.Rule) { rule.ResourceType = "" },
		"effect":        func(rule *abac.Rule) { rule.Effect = authorization.NotApplicable },
		"condition":     func(rule *abac.Rule) { rule.Condition = nil },
		"orphan version": func(rule *abac.Rule) {
			rule.ConditionVersion = 1
		},
		"direct and named": func(rule *abac.Rule) {
			rule.ConditionName = "named"
			rule.ConditionVersion = 1
		},
	}
	for name, mutate := range invalidRules {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule := valid
			mutate(&rule)
			_, err := abac.New([]abac.Rule{rule}, nil)
			if !errors.Is(err, abac.ErrInvalidRule) {
				t.Errorf("abac.New() error = %v, want ErrInvalidRule", err)
			}
		})
	}

	_, err := abac.New([]abac.Rule{valid, valid}, nil)
	if !errors.Is(err, abac.ErrInvalidRule) {
		t.Errorf("duplicate rule error = %v, want ErrInvalidRule", err)
	}

	namedCondition := abac.NamedCondition{
		Name:      "trusted",
		Version:   1,
		Condition: valid.Condition,
	}
	namedTests := map[string][]abac.NamedCondition{
		"missing name":      {{Version: 1, Condition: valid.Condition}},
		"missing version":   {{Name: "trusted", Condition: valid.Condition}},
		"missing condition": {{Name: "trusted", Version: 1}},
	}
	for name, definitions := range namedTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, namedErr := abac.New(nil, definitions)
			if !errors.Is(namedErr, abac.ErrInvalidCondition) {
				t.Errorf("abac.New() error = %v, want ErrInvalidCondition", namedErr)
			}
		})
	}

	_, err = abac.New(nil, []abac.NamedCondition{namedCondition, namedCondition})
	if !errors.Is(err, abac.ErrDuplicateNamedCondition) {
		t.Errorf("duplicate named condition error = %v, want ErrDuplicateNamedCondition", err)
	}
	_, err = abac.New(
		nil,
		[]abac.NamedCondition{namedCondition, {
			Name: "other", Version: 1, Condition: valid.Condition,
		}},
		abac.WithLimits(abac.Limits{MaxNamedConditions: 1}),
	)
	if !errors.Is(err, abac.ErrRuleLimitExceeded) {
		t.Errorf("named condition limit error = %v, want ErrRuleLimitExceeded", err)
	}
}

func TestConditionValidationRejectsInvalidOperands(t *testing.T) {
	t.Parallel()

	validRef := abac.Reference{Source: abac.Request, Name: "value"}
	tests := map[string]abac.Condition{
		"invalid source":             abac.Exists(abac.Reference{Source: abac.Source(255), Name: "value"}),
		"empty name":                 abac.IsNull(abac.Reference{Source: abac.Request}),
		"missing equality literal":   abac.Equal(validRef, authorization.Value{}),
		"invalid comparison literal": abac.GreaterThan(validRef, authorization.BoolValue(true)),
		"mixed in literals": abac.In(
			validRef, authorization.IntValue(1), authorization.StringValue("one"),
		),
		"null in literal": abac.In(validRef, authorization.NullValue()),
		"invalid CIDR":    abac.IPIn(validRef, netip.Prefix{}),
		"nil all child":   abac.All(nil),
		"nil any child":   abac.Any(nil),
	}

	for name, condition := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := abac.EvaluateCondition(
				context.Background(), condition, attributedRequest(), abac.Limits{},
			)
			if !errors.Is(err, abac.ErrInvalidCondition) {
				t.Errorf("EvaluateCondition() error = %v, want ErrInvalidCondition", err)
			}
		})
	}
}

func TestConditionCardinalityLimitsNestedAndLiteralSets(t *testing.T) {
	t.Parallel()

	ref := abac.Reference{Source: abac.Request, Name: "value"}
	conditions := []abac.Condition{
		abac.Equal(ref, authorization.StringSetValue([]string{"one", "two"})),
		abac.All(abac.In(ref, authorization.StringValue("one"), authorization.StringValue("two"))),
		abac.Any(abac.In(ref, authorization.StringValue("one"), authorization.StringValue("two"))),
		abac.Not(abac.In(ref, authorization.StringValue("one"), authorization.StringValue("two"))),
	}
	for _, condition := range conditions {
		_, err := abac.EvaluateCondition(
			context.Background(),
			condition,
			attributedRequest(),
			abac.Limits{MaxSetSize: 1},
		)
		if !errors.Is(err, abac.ErrSetLimitExceeded) {
			t.Errorf("EvaluateCondition() error = %v, want ErrSetLimitExceeded", err)
		}
	}
}

func TestOperatorNonMatchAndTypeSemantics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	request := attributedRequest()
	request.Attributes["age"] = authorization.IntValue(42)
	request.Attributes["score"] = authorization.MustFloatValue(9.5)
	request.Attributes["created_at"] = authorization.TimeValue(now)
	request.Attributes["ip"] = authorization.IPValue(netip.MustParseAddr("192.0.2.10"))
	request.Attributes["tags"] = authorization.StringSetValue([]string{"finance", "trusted"})
	request.Attributes["name"] = authorization.StringValue("Alice")
	request.Attributes["null"] = authorization.NullValue()

	ref := func(name authorization.AttributeName) abac.Reference {
		return abac.Reference{Source: abac.Request, Name: name}
	}
	tests := map[string]struct {
		condition abac.Condition
		status    abac.Status
	}{
		"exists missing":           {abac.Exists(ref("missing")), abac.StatusNoMatch},
		"null non-null":            {abac.IsNull(ref("age")), abac.StatusNoMatch},
		"any no match":             {abac.Any(abac.Equal(ref("age"), authorization.IntValue(1))), abac.StatusNoMatch},
		"any preserves missing":    {abac.Any(abac.Equal(ref("missing"), authorization.IntValue(1))), abac.StatusMissing},
		"not match becomes no":     {abac.Not(abac.Exists(ref("age"))), abac.StatusNoMatch},
		"integer comparison false": {abac.GreaterThan(ref("age"), authorization.IntValue(100)), abac.StatusNoMatch},
		"greater excludes equal":   {abac.GreaterThan(ref("age"), authorization.IntValue(42)), abac.StatusNoMatch},
		"less excludes equal":      {abac.LessThan(ref("age"), authorization.IntValue(42)), abac.StatusNoMatch},
		"comparison mismatch":      {abac.GreaterThan(ref("age"), authorization.MustFloatValue(1)), abac.StatusTypeMismatch},
		"string comparison false":  {abac.GreaterThan(ref("name"), authorization.StringValue("Zulu")), abac.StatusNoMatch},
		"in no match":              {abac.In(ref("age"), authorization.IntValue(1)), abac.StatusNoMatch},
		"in mismatch":              {abac.In(ref("age"), authorization.StringValue("42")), abac.StatusTypeMismatch},
		"set no match":             {abac.SetContains(ref("tags"), "missing"), abac.StatusNoMatch},
		"set after last":           {abac.SetContains(ref("tags"), "zulu"), abac.StatusNoMatch},
		"set mismatch":             {abac.SetContains(ref("age"), "42"), abac.StatusTypeMismatch},
		"prefix no match":          {abac.HasPrefix(ref("name"), "Bob"), abac.StatusNoMatch},
		"string mismatch":          {abac.HasSuffix(ref("age"), "2"), abac.StatusTypeMismatch},
		"IP no match":              {abac.IPIn(ref("ip"), netip.MustParsePrefix("198.51.100.0/24")), abac.StatusNoMatch},
		"IP mismatch":              {abac.IPIn(ref("age"), netip.MustParsePrefix("192.0.2.0/24")), abac.StatusTypeMismatch},
		"missing load":             {abac.LessThan(ref("missing"), authorization.IntValue(1)), abac.StatusMissing},
		"null load":                {abac.StringContains(ref("null"), "x"), abac.StatusNull},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := abac.EvaluateCondition(
				context.Background(), tt.condition, request, abac.Limits{},
			)
			if err != nil {
				t.Fatalf("EvaluateCondition() error = %v", err)
			}
			if result.Matched || result.Status != tt.status {
				t.Errorf("result = %+v, want non-match status %v", result, tt.status)
			}
		})
	}
}

func TestEqualSupportsEveryValueKind(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("192.0.2.10")
	request := attributedRequest()
	values := map[authorization.AttributeName]authorization.Value{
		"null":   authorization.NullValue(),
		"string": authorization.StringValue("value"),
		"bool":   authorization.BoolValue(true),
		"int":    authorization.IntValue(42),
		"float":  authorization.MustFloatValue(4.2),
		"time":   authorization.TimeValue(now),
		"ip":     authorization.IPValue(ip),
		"set":    authorization.StringSetValue([]string{"one", "two"}),
	}
	for name, value := range values {
		request.Attributes[name] = value
		result, err := abac.EvaluateCondition(
			context.Background(),
			abac.Equal(abac.Reference{Source: abac.Request, Name: name}, value),
			request,
			abac.Limits{},
		)
		if err != nil {
			t.Fatalf("Equal(%s) error = %v", name, err)
		}
		if !result.Matched {
			t.Errorf("Equal(%s) = %+v, want match", name, result)
		}
	}

	request.Attributes["set"] = authorization.StringSetValue([]string{"one"})
	result, err := abac.EvaluateCondition(
		context.Background(),
		abac.Equal(
			abac.Reference{Source: abac.Request, Name: "set"},
			authorization.StringSetValue([]string{"one", "two"}),
		),
		request,
		abac.Limits{},
	)
	if err != nil {
		t.Fatalf("unequal set error = %v", err)
	}
	if result.Matched {
		t.Error("unequal set matched")
	}

	request.Attributes["set"] = authorization.StringSetValue([]string{"one", "three"})
	result, err = abac.EvaluateCondition(
		context.Background(),
		abac.Equal(
			abac.Reference{Source: abac.Request, Name: "set"},
			authorization.StringSetValue([]string{"one", "two"}),
		),
		request,
		abac.Limits{},
	)
	if err != nil {
		t.Fatalf("different set error = %v", err)
	}
	if result.Matched {
		t.Error("different set matched")
	}
}

func TestEvaluatorSkipsScopesAndNonMatchingConditions(t *testing.T) {
	t.Parallel()

	base := abac.Rule{
		ID:           "base",
		Tenant:       "tenant-1",
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
		Condition: abac.Equal(
			abac.Reference{Source: abac.Request, Name: "mfa"},
			authorization.BoolValue(false),
		),
	}
	rules := []abac.Rule{
		withRuleIDAndTenant(base, "tenant", "tenant-2"),
		withRuleIDAndAction(base, "action", "document.update"),
		withRuleIDAndResourceType(base, "type", "invoice"),
		withRuleIDAndResourceID(base, "instance", "other-document"),
		base,
	}
	evaluator, err := abac.New(rules, nil)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	decision, err := evaluator.Evaluate(context.Background(), attributedRequest())
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.NotApplicable {
		t.Errorf("Decision.Outcome = %v, want NotApplicable", decision.Outcome)
	}
}

func TestEvaluatorNeverGrantsMismatchedScopes(t *testing.T) {
	t.Parallel()

	base := abac.Rule{
		ID:           "allow",
		Tenant:       "tenant-1",
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
		Condition: abac.Exists(
			abac.Reference{Source: abac.Request, Name: "mfa"},
		),
	}
	tests := map[string]abac.Rule{
		"tenant":        withRuleIDAndTenant(base, "tenant", "tenant-2"),
		"action":        withRuleIDAndAction(base, "action", "document.update"),
		"resource type": withRuleIDAndResourceType(base, "type", "invoice"),
		"resource ID":   withRuleIDAndResourceID(base, "instance", "other-document"),
	}
	for name, rule := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			evaluator, err := abac.New([]abac.Rule{rule}, nil)
			if err != nil {
				t.Fatalf("abac.New() error = %v", err)
			}
			decision, err := evaluator.Evaluate(context.Background(), attributedRequest())
			if err != nil {
				t.Fatalf("Evaluator.Evaluate() error = %v", err)
			}
			if decision.Outcome != authorization.NotApplicable {
				t.Errorf("Decision = %+v, want NotApplicable", decision)
			}
		})
	}
}

func TestAllRequiresEveryChildToMatch(t *testing.T) {
	t.Parallel()

	ref := abac.Reference{Source: abac.Request, Name: "mfa"}
	result, err := abac.EvaluateCondition(
		context.Background(),
		abac.All(
			abac.Equal(ref, authorization.BoolValue(false)),
			abac.Equal(ref, authorization.BoolValue(true)),
		),
		attributedRequest(),
		abac.Limits{},
	)
	if err != nil {
		t.Fatalf("EvaluateCondition() error = %v", err)
	}
	if result.Matched {
		t.Errorf("EvaluateCondition() = %+v, want non-match", result)
	}
}

func TestEvaluatorBatchReturnsEquivalentDecisionsAndErrors(t *testing.T) {
	t.Parallel()

	rule := abac.Rule{
		ID:           "allow",
		Tenant:       "tenant-1",
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
		Condition: abac.Exists(
			abac.Reference{Source: abac.Request, Name: "mfa"},
		),
	}
	evaluator, err := abac.New([]abac.Rule{rule}, nil)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	requests := []authorization.Request{attributedRequest(), attributedRequest()}
	requests[1].Attributes = nil
	batch, err := evaluator.EvaluateBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("Evaluator.EvaluateBatch() error = %v", err)
	}
	if batch[0].Outcome != authorization.Allow ||
		batch[1].Outcome != authorization.NotApplicable {
		t.Errorf("Evaluator.EvaluateBatch() = %+v", batch)
	}

	compositeRule := rule
	compositeRule.Condition = abac.All(rule.Condition)
	costLimited, err := abac.New(
		[]abac.Rule{compositeRule}, nil, abac.WithLimits(abac.Limits{MaxCost: 1}),
	)
	if err != nil {
		t.Fatalf("abac.New() error = %v", err)
	}
	batch, err = costLimited.EvaluateBatch(context.Background(), requests[:1])
	if !errors.Is(err, abac.ErrCostExceeded) {
		t.Fatalf("failing EvaluateBatch() error = %v, want ErrCostExceeded", err)
	}
	if len(batch) != 1 || batch[0].Outcome != authorization.Deny {
		t.Errorf("failing EvaluateBatch() = %+v, want one deny", batch)
	}
}

func TestConditionCancellationAtEveryNodeBoundary(t *testing.T) {
	t.Parallel()

	ref := abac.Reference{Source: abac.Request, Name: "mfa"}
	conditions := []abac.Condition{
		abac.Equal(ref, authorization.BoolValue(true)),
		abac.IsNull(ref),
		abac.Any(abac.Exists(ref)),
		abac.Not(abac.Exists(ref)),
		abac.GreaterThan(
			abac.Reference{Source: abac.Environment, Name: "risk"},
			authorization.IntValue(1),
		),
		abac.In(ref, authorization.BoolValue(true)),
		abac.SetContains(ref, "value"),
		abac.IPIn(ref, netip.MustParsePrefix("192.0.2.0/24")),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, condition := range conditions {
		_, err := abac.EvaluateCondition(ctx, condition, attributedRequest(), abac.Limits{})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("EvaluateCondition(%T) error = %v, want context.Canceled", condition, err)
		}
	}

	_, err := abac.EvaluateCondition(
		context.Background(),
		abac.Not(abac.Exists(ref)),
		attributedRequest(),
		abac.Limits{MaxCost: 1},
	)
	if !errors.Is(err, abac.ErrCostExceeded) {
		t.Errorf("nested Not() error = %v, want ErrCostExceeded", err)
	}
}

func TestNestedValidationPropagatesChildErrors(t *testing.T) {
	t.Parallel()

	invalid := abac.Exists(abac.Reference{Source: abac.Request})
	conditions := []abac.Condition{
		abac.All(invalid),
		abac.Any(invalid),
		abac.GreaterThan(abac.Reference{Source: abac.Request}, authorization.IntValue(1)),
	}
	for _, condition := range conditions {
		_, err := abac.EvaluateCondition(
			context.Background(), condition, attributedRequest(), abac.Limits{},
		)
		if !errors.Is(err, abac.ErrInvalidCondition) {
			t.Errorf("EvaluateCondition(%T) error = %v, want ErrInvalidCondition", condition, err)
		}
	}

	_, err := abac.New(nil, []abac.NamedCondition{
		{Name: "invalid", Version: 1, Condition: invalid},
	})
	if !errors.Is(err, abac.ErrInvalidCondition) {
		t.Errorf("invalid named condition error = %v, want ErrInvalidCondition", err)
	}
}

func TestLoadOperatorsPreserveMissingStatus(t *testing.T) {
	t.Parallel()

	missing := abac.Reference{Source: abac.Request, Name: "missing"}
	conditions := []abac.Condition{
		abac.In(missing, authorization.StringValue("value")),
		abac.SetContains(missing, "value"),
		abac.IPIn(missing, netip.MustParsePrefix("192.0.2.0/24")),
	}
	for _, condition := range conditions {
		result, err := abac.EvaluateCondition(
			context.Background(), condition, attributedRequest(), abac.Limits{},
		)
		if err != nil {
			t.Fatalf("EvaluateCondition(%T) error = %v", condition, err)
		}
		if result.Status != abac.StatusMissing {
			t.Errorf("EvaluateCondition(%T) = %+v, want missing", condition, result)
		}
	}
}

func TestRuntimeAttributeCollectionsAreBounded(t *testing.T) {
	t.Parallel()

	request := attributedRequest()
	request.Attributes["tags"] = authorization.StringSetValue([]string{"one", "two"})
	conditions := []abac.Condition{
		abac.Equal(
			abac.Reference{Source: abac.Request, Name: "tags"},
			authorization.StringSetValue([]string{"one"}),
		),
		abac.SetContains(abac.Reference{Source: abac.Request, Name: "tags"}, "one"),
	}
	for _, condition := range conditions {
		_, err := abac.EvaluateCondition(
			context.Background(), condition, request, abac.Limits{MaxSetSize: 1},
		)
		if !errors.Is(err, abac.ErrSetLimitExceeded) {
			t.Errorf("EvaluateCondition(%T) error = %v, want ErrSetLimitExceeded", condition, err)
		}
	}
}

func withRuleIDAndTenant(
	rule abac.Rule,
	id authorization.PolicyID,
	tenant authorization.TenantID,
) abac.Rule {
	rule.ID = id
	rule.Tenant = tenant
	return rule
}

func withRuleIDAndAction(
	rule abac.Rule,
	id authorization.PolicyID,
	action authorization.Action,
) abac.Rule {
	rule.ID = id
	rule.Action = action
	return rule
}

func withRuleIDAndResourceType(
	rule abac.Rule,
	id authorization.PolicyID,
	resourceType authorization.ResourceType,
) abac.Rule {
	rule.ID = id
	rule.ResourceType = resourceType
	return rule
}

func withRuleIDAndResourceID(
	rule abac.Rule,
	id authorization.PolicyID,
	resourceID authorization.ResourceID,
) abac.Rule {
	rule.ID = id
	rule.ResourceID = resourceID
	return rule
}

func attributedRequest() authorization.Request {
	return authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "alice",
			Attributes: authorization.Attributes{
				"department": authorization.StringValue("finance"),
				"manager":    authorization.NullValue(),
			},
		},
		Action: "document.read",
		Resource: authorization.Resource{
			Type: "document",
			ID:   "document-1",
			Attributes: authorization.Attributes{
				"classification": authorization.StringValue("internal"),
			},
		},
		Tenant: "tenant-1",
		Environment: authorization.Environment{
			Attributes: authorization.Attributes{
				"risk": authorization.IntValue(2),
			},
		},
		Attributes: authorization.Attributes{
			"mfa": authorization.BoolValue(true),
		},
	}
}
