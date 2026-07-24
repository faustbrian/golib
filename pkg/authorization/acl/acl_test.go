package acl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/acl"
)

type cancelAfterContext struct {
	context.Context
	remaining int
}

func (ctx *cancelAfterContext) Err() error {
	if ctx.remaining == 0 {
		return context.Canceled
	}
	ctx.remaining--
	return nil
}

func TestEvaluatorMatchesInstanceAndTypeEntries(t *testing.T) {
	t.Parallel()

	evaluator, err := acl.New([]acl.Entry{
		{
			ID:           "documents-read",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
		{
			ID:           "document-delete",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.delete",
			ResourceType: "document",
			ResourceID:   "document-1",
			Effect:       authorization.Allow,
		},
	})
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	tests := map[string]struct {
		action     authorization.Action
		resourceID authorization.ResourceID
		want       authorization.Outcome
	}{
		"type entry applies to every instance": {
			action:     "document.read",
			resourceID: "document-2",
			want:       authorization.Allow,
		},
		"instance entry applies to its instance": {
			action:     "document.delete",
			resourceID: "document-1",
			want:       authorization.Allow,
		},
		"instance entry does not cross resources": {
			action:     "document.delete",
			resourceID: "document-2",
			want:       authorization.NotApplicable,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := request(tt.action, tt.resourceID, "")
			decision, evaluateErr := evaluator.Evaluate(context.Background(), request)
			if evaluateErr != nil {
				t.Fatalf("Evaluator.Evaluate() error = %v", evaluateErr)
			}
			if decision.Outcome != tt.want {
				t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, tt.want)
			}
		})
	}
}

func TestEvaluatorExplicitDenyOverridesAllow(t *testing.T) {
	t.Parallel()

	evaluator, err := acl.New([]acl.Entry{
		{
			ID:           "allow",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
		{
			ID:           "deny",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			ResourceID:   "document-1",
			Effect:       authorization.Deny,
		},
	})
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	decision, err := evaluator.Evaluate(
		context.Background(),
		request("document.read", "document-1", ""),
	)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Deny {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, authorization.Deny)
	}
	if decision.Reason != acl.ReasonExplicitDeny {
		t.Errorf("Decision.Reason = %q, want %q", decision.Reason, acl.ReasonExplicitDeny)
	}
	assertPolicyIDs(t, decision.MatchedPolicyIDs, []authorization.PolicyID{"allow", "deny"})
}

func TestEvaluatorTenantAndGlobalScopes(t *testing.T) {
	t.Parallel()

	entries := []acl.Entry{
		{
			ID:           "global",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
		{
			ID:           "tenant",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			Tenant:       "tenant-1",
			Effect:       authorization.Allow,
		},
	}

	isolated, err := acl.New(entries)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	inherited, err := acl.New(entries, acl.WithGlobalInheritance())
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	tests := map[string]struct {
		evaluator *acl.Evaluator
		tenant    authorization.TenantID
		want      []authorization.PolicyID
	}{
		"global request uses global entry": {
			evaluator: isolated,
			want:      []authorization.PolicyID{"global"},
		},
		"tenant uses its entry without implicit globals": {
			evaluator: isolated,
			tenant:    "tenant-1",
			want:      []authorization.PolicyID{"tenant"},
		},
		"other tenant cannot use tenant entry": {
			evaluator: isolated,
			tenant:    "tenant-2",
		},
		"explicit inheritance includes globals": {
			evaluator: inherited,
			tenant:    "tenant-1",
			want:      []authorization.PolicyID{"global", "tenant"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision, evaluateErr := tt.evaluator.Evaluate(
				context.Background(),
				request("document.read", "document-1", tt.tenant),
			)
			if evaluateErr != nil {
				t.Fatalf("Evaluator.Evaluate() error = %v", evaluateErr)
			}
			assertPolicyIDs(t, decision.MatchedPolicyIDs, tt.want)
		})
	}
}

func TestNewRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	valid := acl.Entry{
		ID:           "entry",
		Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}

	tests := map[string]func(*acl.Entry){
		"id":            func(entry *acl.Entry) { entry.ID = "" },
		"subject kind":  func(entry *acl.Entry) { entry.Subject.Kind = "" },
		"subject id":    func(entry *acl.Entry) { entry.Subject.ID = "" },
		"action":        func(entry *acl.Entry) { entry.Action = "" },
		"resource type": func(entry *acl.Entry) { entry.ResourceType = "" },
		"effect":        func(entry *acl.Entry) { entry.Effect = authorization.NotApplicable },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entry := valid
			mutate(&entry)
			_, err := acl.New([]acl.Entry{entry})
			if !errors.Is(err, acl.ErrInvalidEntry) {
				t.Errorf("acl.New() error = %v, want ErrInvalidEntry", err)
			}
		})
	}

	_, err := acl.New([]acl.Entry{valid, valid})
	if !errors.Is(err, acl.ErrDuplicateEntry) {
		t.Errorf("duplicate acl.New() error = %v, want ErrDuplicateEntry", err)
	}
}

func TestEvaluatorMatchesTrustedSubjectGroups(t *testing.T) {
	t.Parallel()

	evaluator, err := acl.New([]acl.Entry{
		{
			ID:           "editors",
			Subject:      authorization.Subject{Kind: authorization.SubjectGroup, ID: "editors"},
			Action:       "document.update",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
	})
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	request := request("document.update", "document-1", "")
	request.Subject.Groups = []authorization.SubjectID{"editors", "editors"}
	decision, err := evaluator.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Allow {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, authorization.Allow)
	}
	assertPolicyIDs(t, decision.MatchedPolicyIDs, []authorization.PolicyID{"editors"})
}

func TestEvaluatorEnforcesWorkLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	entry := acl.Entry{
		ID:           "allow",
		Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}

	if _, err := acl.New(
		[]acl.Entry{entry, {ID: "second", Subject: entry.Subject, Action: entry.Action, ResourceType: entry.ResourceType, Effect: entry.Effect}},
		acl.WithLimits(acl.Limits{MaxEntries: 1}),
	); !errors.Is(err, acl.ErrEntryLimitExceeded) {
		t.Errorf("entry-limited acl.New() error = %v, want ErrEntryLimitExceeded", err)
	}
	if _, err := acl.New(
		[]acl.Entry{entry},
		acl.WithLimits(acl.Limits{MaxEntries: 1}),
	); err != nil {
		t.Errorf("acl.New(at entry limit) error = %v", err)
	}

	defaultLimited, err := acl.New(
		[]acl.Entry{entry},
		acl.WithLimits(acl.Limits{}),
	)
	if err != nil {
		t.Fatalf("acl.New(zero limits) error = %v", err)
	}
	defaultRequest := request("document.read", "document-1", "")
	defaultRequest.Subject.Groups = []authorization.SubjectID{"group"}
	decision, err := defaultLimited.Evaluate(context.Background(), defaultRequest)
	if err != nil || decision.Outcome != authorization.Allow {
		t.Errorf("Evaluate(zero limits) = (%+v, %v), want allow", decision, err)
	}
	if _, err := defaultLimited.EvaluateBatch(
		context.Background(),
		[]authorization.Request{defaultRequest},
	); err != nil {
		t.Errorf("EvaluateBatch(zero limits) error = %v", err)
	}

	evaluator, err := acl.New(
		[]acl.Entry{entry},
		acl.WithLimits(acl.Limits{MaxGroups: 1, MaxMatches: 1}),
	)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	groupRequest := request("document.read", "document-1", "")
	groupRequest.Subject.Groups = []authorization.SubjectID{"first", "second"}
	decision, err = evaluator.Evaluate(context.Background(), groupRequest)
	if !errors.Is(err, acl.ErrGroupLimitExceeded) {
		t.Fatalf("group-limited Evaluate() error = %v, want ErrGroupLimitExceeded", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != acl.ReasonLimitExceeded {
		t.Errorf("group-limited Decision = %+v, want limit deny", decision)
	}

	groupRequest.Subject.Groups = groupRequest.Subject.Groups[:1]
	decision, err = evaluator.Evaluate(context.Background(), groupRequest)
	if err != nil || decision.Outcome != authorization.Allow {
		t.Errorf("Evaluate(at group limit) = (%+v, %v), want allow", decision, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err = evaluator.Evaluate(ctx, request("document.read", "document-1", ""))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Evaluate() error = %v, want context.Canceled", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != authorization.ReasonContextCanceled {
		t.Errorf("canceled Decision = %+v, want cancellation deny", decision)
	}

	second := entry
	second.ID = "second"
	matchLimited, err := acl.New(
		[]acl.Entry{entry, second},
		acl.WithLimits(acl.Limits{MaxMatches: 1}),
	)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	decision, err = matchLimited.Evaluate(
		context.Background(),
		request("document.read", "document-1", ""),
	)
	if !errors.Is(err, acl.ErrMatchLimitExceeded) {
		t.Fatalf("match-limited Evaluate() error = %v, want ErrMatchLimitExceeded", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != acl.ReasonLimitExceeded {
		t.Errorf("match-limited Decision = %+v, want limit deny", decision)
	}

	betweenPrincipals := &cancelAfterContext{Context: context.Background(), remaining: 1}
	decision, err = evaluator.Evaluate(
		betweenPrincipals,
		request("document.read", "document-1", ""),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-evaluation error = %v, want context.Canceled", err)
	}
	if decision.Reason != authorization.ReasonContextCanceled {
		t.Errorf("mid-evaluation Decision = %+v, want cancellation deny", decision)
	}
}

func TestEvaluatorBatchMatchesIndependentDecisions(t *testing.T) {
	t.Parallel()

	entry := acl.Entry{
		ID:           "allow",
		Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}
	evaluator, err := acl.New(
		[]acl.Entry{entry},
		acl.WithLimits(acl.Limits{MaxBatchSize: 2, MaxGroups: 1}),
	)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	requests := []authorization.Request{
		request("document.read", "document-1", ""),
		request("document.delete", "document-1", ""),
	}
	batch, err := evaluator.EvaluateBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("Evaluator.EvaluateBatch() error = %v", err)
	}
	for index, item := range requests {
		independent, evaluateErr := evaluator.Evaluate(context.Background(), item)
		if evaluateErr != nil {
			t.Fatalf("Evaluator.Evaluate() error = %v", evaluateErr)
		}
		if batch[index].Outcome != independent.Outcome {
			t.Errorf("batch[%d].Outcome = %v, want %v", index, batch[index].Outcome, independent.Outcome)
		}
	}

	_, err = evaluator.EvaluateBatch(context.Background(), append(requests, requests[0]))
	if !errors.Is(err, acl.ErrBatchLimitExceeded) {
		t.Errorf("oversized EvaluateBatch() error = %v, want ErrBatchLimitExceeded", err)
	}

	invalid := requests[0]
	invalid.Subject.Groups = []authorization.SubjectID{"one", "two"}
	decisions, err := evaluator.EvaluateBatch(context.Background(), []authorization.Request{invalid})
	if !errors.Is(err, acl.ErrGroupLimitExceeded) {
		t.Errorf("failing EvaluateBatch() error = %v, want ErrGroupLimitExceeded", err)
	}
	if len(decisions) != 1 || decisions[0].Outcome != authorization.Deny {
		t.Errorf("failing EvaluateBatch() decisions = %+v, want one deny", decisions)
	}
}

func TestEvaluatorListsExplicitlyAllowedResourceIDs(t *testing.T) {
	t.Parallel()

	base := acl.Entry{
		Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}
	entries := []acl.Entry{
		withEntryIDAndResource(base, "allow-1", "document-1"),
		withEntryIDAndResource(base, "allow-2", "document-2"),
		withEffect(withEntryIDAndResource(base, "deny-2", "document-2"), authorization.Deny),
		withTenant(withEntryIDAndResource(base, "other-tenant", "document-3"), "tenant-2"),
	}
	evaluator, err := acl.New(entries)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}

	resourceIDs, err := evaluator.ListResourceIDs(
		context.Background(),
		authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		"document.read",
		"document",
		"",
	)
	if err != nil {
		t.Fatalf("Evaluator.ListResourceIDs() error = %v", err)
	}
	if len(resourceIDs) != 1 || resourceIDs[0] != "document-1" {
		t.Errorf("Evaluator.ListResourceIDs() = %v, want [document-1]", resourceIDs)
	}

	typeWide := base
	typeWide.ID = "all-documents"
	unbounded, err := acl.New([]acl.Entry{typeWide})
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	_, err = unbounded.ListResourceIDs(
		context.Background(),
		base.Subject,
		base.Action,
		base.ResourceType,
		base.Tenant,
	)
	if !errors.Is(err, acl.ErrUnboundedResourceSet) {
		t.Errorf("type-wide ListResourceIDs() error = %v, want ErrUnboundedResourceSet", err)
	}
}

func TestEvaluatorPreservesEntryIdentityThroughRootEngine(t *testing.T) {
	t.Parallel()

	evaluator, err := acl.New([]acl.Entry{
		{
			ID:           "document-reader",
			Subject:      authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
	})
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	snapshot, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "application-acl", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	engine, err := authorization.NewEngine(snapshot)
	if err != nil {
		t.Fatalf("authorization.NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(
		context.Background(),
		request("document.read", "document-1", ""),
	)
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	assertPolicyIDs(
		t,
		decision.MatchedPolicyIDs,
		[]authorization.PolicyID{"document-reader"},
	)
	if len(decision.Trace) != 1 || decision.Trace[0].PolicyID != "application-acl" {
		t.Errorf("Decision.Trace = %+v, want application-acl", decision.Trace)
	}
}

func TestListResourceIDsHonorsBoundsGroupsAndTypeDeny(t *testing.T) {
	t.Parallel()

	group := authorization.Subject{
		Kind: authorization.SubjectGroup,
		ID:   "editors",
	}
	entries := []acl.Entry{
		{
			ID:           "group-2",
			Subject:      group,
			Action:       "document.read",
			ResourceType: "document",
			ResourceID:   "document-2",
			Effect:       authorization.Allow,
		},
		{
			ID:           "group-1",
			Subject:      group,
			Action:       "document.read",
			ResourceType: "document",
			ResourceID:   "document-1",
			Effect:       authorization.Allow,
		},
	}
	evaluator, err := acl.New(entries)
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	subject := authorization.Subject{
		Kind:   authorization.SubjectUser,
		ID:     "user-1",
		Groups: []authorization.SubjectID{"editors", "editors"},
	}
	resourceIDs, err := evaluator.ListResourceIDs(
		context.Background(), subject, "document.read", "document", "",
	)
	if err != nil {
		t.Fatalf("Evaluator.ListResourceIDs() error = %v", err)
	}
	if len(resourceIDs) != 2 || resourceIDs[0] != "document-1" || resourceIDs[1] != "document-2" {
		t.Errorf("Evaluator.ListResourceIDs() = %v, want sorted IDs", resourceIDs)
	}

	limited, err := acl.New(entries, acl.WithLimits(acl.Limits{MaxGroups: 1, MaxMatches: 1}))
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	_, err = limited.ListResourceIDs(
		context.Background(), subject, "document.read", "document", "",
	)
	if !errors.Is(err, acl.ErrGroupLimitExceeded) {
		t.Errorf("group-limited ListResourceIDs() error = %v, want ErrGroupLimitExceeded", err)
	}
	subject.Groups = []authorization.SubjectID{"editors"}
	_, err = limited.ListResourceIDs(
		context.Background(), subject, "document.read", "document", "",
	)
	if !errors.Is(err, acl.ErrMatchLimitExceeded) {
		t.Errorf("match-limited ListResourceIDs() error = %v, want ErrMatchLimitExceeded", err)
	}

	canceled, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = evaluator.ListResourceIDs(canceled, subject, "document.read", "document", "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("canceled ListResourceIDs() error = %v, want DeadlineExceeded", err)
	}

	betweenPrincipals := &cancelAfterContext{Context: context.Background(), remaining: 1}
	_, err = evaluator.ListResourceIDs(
		betweenPrincipals, subject, "document.read", "document", "",
	)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("mid-list ListResourceIDs() error = %v, want context.Canceled", err)
	}

	typeDeny := entries[0]
	typeDeny.ID = "deny-all"
	typeDeny.ResourceID = ""
	typeDeny.Effect = authorization.Deny
	denied, err := acl.New(append(entries, typeDeny))
	if err != nil {
		t.Fatalf("acl.New() error = %v", err)
	}
	resourceIDs, err = denied.ListResourceIDs(
		context.Background(), subject, "document.read", "document", "",
	)
	if err != nil {
		t.Fatalf("denied ListResourceIDs() error = %v", err)
	}
	if len(resourceIDs) != 0 {
		t.Errorf("denied ListResourceIDs() = %v, want empty", resourceIDs)
	}
}

func withEntryIDAndResource(
	entry acl.Entry,
	id acl.EntryID,
	resourceID authorization.ResourceID,
) acl.Entry {
	entry.ID = id
	entry.ResourceID = resourceID
	return entry
}

func withEffect(entry acl.Entry, effect authorization.Outcome) acl.Entry {
	entry.Effect = effect
	return entry
}

func withTenant(entry acl.Entry, tenant authorization.TenantID) acl.Entry {
	entry.Tenant = tenant
	return entry
}

func assertPolicyIDs(t *testing.T, got, want []authorization.PolicyID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("policy IDs = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("policy IDs[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func request(
	action authorization.Action,
	resourceID authorization.ResourceID,
	tenant authorization.TenantID,
) authorization.Request {
	return authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "user-1",
		},
		Action: action,
		Resource: authorization.Resource{
			Type: "document",
			ID:   resourceID,
		},
		Tenant: tenant,
	}
}
