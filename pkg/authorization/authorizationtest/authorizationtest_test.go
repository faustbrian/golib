package authorizationtest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/authorizationtest"
)

func TestRequestBuilderCreatesIndependentDeterministicRequests(t *testing.T) {
	builder := authorizationtest.NewRequest().
		WithGroup("operators").
		WithSubjectAttribute("region", authorization.StringValue("eu")).
		WithResourceAttribute("owner", authorization.StringValue("user-1")).
		WithEnvironmentAttribute("risk", authorization.IntValue(2)).
		WithAttribute("source", authorization.StringValue("test"))

	first := builder.Build()
	second := builder.Build()
	first.Subject.Groups[0] = "changed"
	first.Subject.Attributes["region"] = authorization.StringValue("changed")

	if second.Subject.ID != "user-1" || second.Action != "read" ||
		second.Resource.Type != "document" || second.Resource.ID != "document-1" ||
		second.Tenant != "tenant-1" {
		t.Fatalf("Build() identifiers = %#v, want deterministic defaults", second)
	}
	if second.Environment.Time != authorizationtest.FixedTime() {
		t.Fatalf("Build() time = %v, want %v", second.Environment.Time, authorizationtest.FixedTime())
	}
	region, ok := second.Subject.Attributes["region"].String()
	if second.Subject.Groups[0] != "operators" || !ok || region != "eu" {
		t.Fatalf("Build() aliases prior result: %#v", second)
	}
}

func TestRequestBuilderOverridesTypedFields(t *testing.T) {
	request := authorizationtest.NewRequest().
		WithSubject(authorization.SubjectServiceAccount, "service-1").
		WithAction("write").
		WithResource("invoice", "invoice-1").
		WithTenant("tenant-2").
		WithTime(time.Unix(1, 0).UTC()).
		Build()

	if request.Subject.Kind != authorization.SubjectServiceAccount ||
		request.Subject.ID != "service-1" || request.Action != "write" ||
		request.Resource.Type != "invoice" || request.Resource.ID != "invoice-1" ||
		request.Tenant != "tenant-2" || !request.Environment.Time.Equal(time.Unix(1, 0)) {
		t.Fatalf("Build() = %#v, want overridden request", request)
	}
}

func TestStaticEvaluators(t *testing.T) {
	tests := []struct {
		name      string
		evaluator authorization.Evaluator
		outcome   authorization.Outcome
		wantError bool
	}{
		{name: "allow", evaluator: authorizationtest.AllowEvaluator("allowed"), outcome: authorization.Allow},
		{name: "deny", evaluator: authorizationtest.DenyEvaluator("denied"), outcome: authorization.Deny},
		{name: "not applicable", evaluator: authorizationtest.NotApplicableEvaluator(), outcome: authorization.NotApplicable},
		{name: "error", evaluator: authorizationtest.ErrorEvaluator(errors.New("failure")), outcome: authorization.NotApplicable, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := test.evaluator.Evaluate(context.Background(), authorizationtest.NewRequest().Build())
			if (err != nil) != test.wantError || decision.Outcome != test.outcome {
				t.Fatalf("Evaluate() = (%#v, %v), want outcome %v, error %v", decision, err, test.outcome, test.wantError)
			}
		})
	}
}

func TestCanonicalDecisionJSON(t *testing.T) {
	decision := authorization.Decision{
		Outcome: authorization.Allow, Reason: "role-match", Revision: 7,
		MatchedPolicyIDs:          []authorization.PolicyID{"role-reader"},
		MatchedPolicyIDsTruncated: true,
		Trace:                     []authorization.TraceEntry{{PolicyID: "application", Outcome: authorization.Allow, Reason: "role-match"}},
		TraceTruncated:            true,
	}
	want := "{\n" +
		"  \"outcome\": \"allow\",\n" +
		"  \"reason\": \"role-match\",\n" +
		"  \"matched_policy_ids\": [\n" +
		"    \"role-reader\"\n" +
		"  ],\n" +
		"  \"matched_policy_ids_truncated\": true,\n" +
		"  \"revision\": 7,\n" +
		"  \"trace\": [\n" +
		"    {\n" +
		"      \"policy_id\": \"application\",\n" +
		"      \"outcome\": \"allow\",\n" +
		"      \"reason\": \"role-match\"\n" +
		"    }\n" +
		"  ],\n" +
		"  \"trace_truncated\": true\n" +
		"}\n"
	encoded, err := authorizationtest.CanonicalDecisionJSON(decision)
	if err != nil {
		t.Fatalf("CanonicalDecisionJSON() error = %v", err)
	}
	if got := string(encoded); got != want {
		t.Fatalf("CanonicalDecisionJSON() = %q, want %q", got, want)
	}
}

func TestMustHelpersAndDecisionAssertions(t *testing.T) {
	snapshot := authorizationtest.MustSnapshot(t, 9, authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "allow", Evaluator: authorizationtest.AllowEvaluator("fixture")},
	)
	engine := authorizationtest.MustEngine(t, snapshot)
	decision, err := engine.Decide(context.Background(), authorizationtest.NewRequest().Build())
	authorizationtest.RequireDecision(t, decision, err, authorization.Decision{
		Outcome: authorization.Allow, Reason: "fixture", Revision: 9,
		MatchedPolicyIDs: []authorization.PolicyID{"allow"},
		Trace:            []authorization.TraceEntry{{PolicyID: "allow", Outcome: authorization.Allow, Reason: "fixture"}},
	})
	authorizationtest.RequireOutcome(t, decision, authorization.Allow)
}

func TestRunAuthorizerConformance(t *testing.T) {
	authorizationtest.RunAuthorizerConformance(t, func(evaluator authorization.Evaluator) authorization.Authorizer {
		snapshot := authorizationtest.MustSnapshot(t, 1, authorization.DenyOverrides,
			authorization.PolicyDefinition{ID: "conformance", Evaluator: evaluator},
		)
		return authorizationtest.MustEngine(t, snapshot)
	})
}
