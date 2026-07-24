// Package authorizationtest provides deterministic fixtures, assertions, and
// conformance checks for authorization integrations.
package authorizationtest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func FixedTime() time.Time {
	return time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
}

type RequestBuilder struct {
	request authorization.Request
}

func NewRequest() *RequestBuilder {
	return &RequestBuilder{request: authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "user-1",
		},
		Action:   "read",
		Resource: authorization.Resource{Type: "document", ID: "document-1"},
		Tenant:   "tenant-1",
		Environment: authorization.Environment{
			Time: FixedTime(),
		},
	}}
}

func (builder *RequestBuilder) WithSubject(
	kind authorization.SubjectKind,
	id authorization.SubjectID,
) *RequestBuilder {
	builder.request.Subject.Kind = kind
	builder.request.Subject.ID = id
	return builder
}

func (builder *RequestBuilder) WithGroup(id authorization.SubjectID) *RequestBuilder {
	builder.request.Subject.Groups = append(builder.request.Subject.Groups, id)
	return builder
}

func (builder *RequestBuilder) WithAction(action authorization.Action) *RequestBuilder {
	builder.request.Action = action
	return builder
}

func (builder *RequestBuilder) WithResource(
	typeName authorization.ResourceType,
	id authorization.ResourceID,
) *RequestBuilder {
	builder.request.Resource.Type = typeName
	builder.request.Resource.ID = id
	return builder
}

func (builder *RequestBuilder) WithTenant(tenant authorization.TenantID) *RequestBuilder {
	builder.request.Tenant = tenant
	return builder
}

func (builder *RequestBuilder) WithTime(at time.Time) *RequestBuilder {
	builder.request.Environment.Time = at.Round(0).UTC()
	return builder
}

func (builder *RequestBuilder) WithSubjectAttribute(
	name authorization.AttributeName,
	value authorization.Value,
) *RequestBuilder {
	builder.request.Subject.Attributes = setAttribute(builder.request.Subject.Attributes, name, value)
	return builder
}

func (builder *RequestBuilder) WithResourceAttribute(
	name authorization.AttributeName,
	value authorization.Value,
) *RequestBuilder {
	builder.request.Resource.Attributes = setAttribute(builder.request.Resource.Attributes, name, value)
	return builder
}

func (builder *RequestBuilder) WithEnvironmentAttribute(
	name authorization.AttributeName,
	value authorization.Value,
) *RequestBuilder {
	builder.request.Environment.Attributes = setAttribute(builder.request.Environment.Attributes, name, value)
	return builder
}

func (builder *RequestBuilder) WithAttribute(
	name authorization.AttributeName,
	value authorization.Value,
) *RequestBuilder {
	builder.request.Attributes = setAttribute(builder.request.Attributes, name, value)
	return builder
}

func (builder *RequestBuilder) Build() authorization.Request {
	request := builder.request
	request.Subject.Groups = append([]authorization.SubjectID(nil), request.Subject.Groups...)
	request.Subject.Attributes = cloneAttributes(request.Subject.Attributes)
	request.Resource.Attributes = cloneAttributes(request.Resource.Attributes)
	request.Environment.Attributes = cloneAttributes(request.Environment.Attributes)
	request.Attributes = cloneAttributes(request.Attributes)
	return request
}

func setAttribute(
	attributes authorization.Attributes,
	name authorization.AttributeName,
	value authorization.Value,
) authorization.Attributes {
	if attributes == nil {
		attributes = make(authorization.Attributes)
	}
	attributes[name] = value
	return attributes
}

func cloneAttributes(attributes authorization.Attributes) authorization.Attributes {
	if attributes == nil {
		return nil
	}
	clone := make(authorization.Attributes, len(attributes))
	for name, value := range attributes {
		clone[name] = value
	}
	return clone
}

type StaticEvaluator struct {
	Decision authorization.Decision
	Err      error
}

func (evaluator StaticEvaluator) Evaluate(
	context.Context,
	authorization.Request,
) (authorization.Decision, error) {
	decision := evaluator.Decision
	decision.MatchedPolicyIDs = append([]authorization.PolicyID(nil), decision.MatchedPolicyIDs...)
	decision.Trace = append([]authorization.TraceEntry(nil), decision.Trace...)
	return decision, evaluator.Err
}

func AllowEvaluator(reason authorization.ReasonCode) authorization.Evaluator {
	return StaticEvaluator{Decision: authorization.Decision{
		Outcome: authorization.Allow,
		Reason:  reason,
	}}
}

func DenyEvaluator(reason authorization.ReasonCode) authorization.Evaluator {
	return StaticEvaluator{Decision: authorization.Decision{
		Outcome: authorization.Deny,
		Reason:  reason,
	}}
}

func NotApplicableEvaluator() authorization.Evaluator {
	return StaticEvaluator{Decision: authorization.Decision{Outcome: authorization.NotApplicable}}
}

func ErrorEvaluator(err error) authorization.Evaluator {
	return StaticEvaluator{Err: err}
}

type TestingT interface {
	Helper()
	Fatalf(string, ...any)
}

func MustSnapshot(
	t TestingT,
	revision authorization.Revision,
	algorithm authorization.CombiningAlgorithm,
	definitions ...authorization.PolicyDefinition,
) *authorization.Snapshot {
	t.Helper()
	snapshot, err := authorization.NewSnapshot(revision, algorithm, definitions...)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	return snapshot
}

func MustEngine(
	t TestingT,
	snapshot *authorization.Snapshot,
	options ...authorization.EngineOption,
) *authorization.Engine {
	t.Helper()
	engine, err := authorization.NewEngine(snapshot, options...)
	if err != nil {
		t.Fatalf("authorization.NewEngine() error = %v", err)
	}
	return engine
}

func RequireDecision(
	t TestingT,
	got authorization.Decision,
	err error,
	want authorization.Decision,
) {
	t.Helper()
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Decide() = %#v, want %#v", got, want)
	}
}

func RequireOutcome(
	t TestingT,
	decision authorization.Decision,
	want authorization.Outcome,
) {
	t.Helper()
	if decision.Outcome != want {
		t.Fatalf("decision outcome = %s, want %s", decision.Outcome, want)
	}
}

type decisionJSON struct {
	Outcome                   string      `json:"outcome"`
	Reason                    string      `json:"reason"`
	MatchedPolicyIDs          []string    `json:"matched_policy_ids"`
	MatchedPolicyIDsTruncated bool        `json:"matched_policy_ids_truncated"`
	Revision                  uint64      `json:"revision"`
	Trace                     []traceJSON `json:"trace"`
	TraceTruncated            bool        `json:"trace_truncated"`
}

type traceJSON struct {
	PolicyID string `json:"policy_id"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`
}

func CanonicalDecisionJSON(decision authorization.Decision) ([]byte, error) {
	matched := make([]string, len(decision.MatchedPolicyIDs))
	for index, id := range decision.MatchedPolicyIDs {
		matched[index] = string(id)
	}
	trace := make([]traceJSON, len(decision.Trace))
	for index, entry := range decision.Trace {
		trace[index] = traceJSON{
			PolicyID: string(entry.PolicyID),
			Outcome:  entry.Outcome.String(),
			Reason:   string(entry.Reason),
		}
	}
	encoded, err := json.MarshalIndent(decisionJSON{
		Outcome: decision.Outcome.String(), Reason: string(decision.Reason),
		MatchedPolicyIDs:          matched,
		MatchedPolicyIDsTruncated: decision.MatchedPolicyIDsTruncated,
		Revision:                  uint64(decision.Revision),
		Trace:                     trace,
		TraceTruncated:            decision.TraceTruncated,
	}, "", "  ")
	return append(encoded, '\n'), err
}

type AuthorizerFactory func(authorization.Evaluator) authorization.Authorizer

func RunAuthorizerConformance(t *testing.T, factory AuthorizerFactory) {
	t.Helper()
	tests := []struct {
		name      string
		evaluator authorization.Evaluator
		request   authorization.Request
		prepare   func(context.Context) context.Context
		outcome   authorization.Outcome
		reason    authorization.ReasonCode
		wantError bool
	}{
		{name: "allow", evaluator: AllowEvaluator("fixture-allow"), request: NewRequest().Build(), outcome: authorization.Allow, reason: "fixture-allow"},
		{name: "deny", evaluator: DenyEvaluator("fixture-deny"), request: NewRequest().Build(), outcome: authorization.Deny, reason: "fixture-deny"},
		{name: "default deny", evaluator: NotApplicableEvaluator(), request: NewRequest().Build(), outcome: authorization.Deny, reason: authorization.ReasonDefaultDeny},
		{name: "evaluation error", evaluator: ErrorEvaluator(errors.New("fixture failure")), request: NewRequest().Build(), outcome: authorization.Deny, reason: authorization.ReasonEvaluationError, wantError: true},
		{name: "invalid request", evaluator: AllowEvaluator("unused"), request: authorization.Request{}, outcome: authorization.Deny, reason: authorization.ReasonInvalidRequest, wantError: true},
		{name: "canceled context", evaluator: AllowEvaluator("unused"), request: NewRequest().Build(), prepare: canceledContext, outcome: authorization.Deny, reason: authorization.ReasonContextCanceled, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := factory(test.evaluator)
			ctx := context.Background()
			if test.prepare != nil {
				ctx = test.prepare(ctx)
			}
			decision, err := authorizer.Decide(ctx, test.request)
			requireConformanceDecision(t, decision, err, test.outcome, test.reason, test.wantError)
		})
	}
}

func requireConformanceDecision(
	t TestingT,
	decision authorization.Decision,
	err error,
	outcome authorization.Outcome,
	reason authorization.ReasonCode,
	wantError bool,
) {
	t.Helper()
	if (err != nil) != wantError {
		t.Fatalf("Decide() error = %v, want error %v", err, wantError)
	}
	if decision.Outcome != outcome || decision.Reason != reason {
		t.Fatalf("Decide() = %#v, want outcome %s and reason %q", decision, outcome, reason)
	}
}

func canceledContext(ctx context.Context) context.Context {
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	return canceled
}
