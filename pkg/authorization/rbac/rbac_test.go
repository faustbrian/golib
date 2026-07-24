package rbac_test

import (
	"context"
	"errors"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func TestEvaluatorCombinesAssignedRolePermissions(t *testing.T) {
	t.Parallel()

	evaluator, err := rbac.New(
		[]rbac.Role{
			{ID: "reader", Tenant: "tenant-1"},
			{ID: "restricted", Tenant: "tenant-1"},
		},
		[]rbac.Permission{
			{
				ID:           "read-documents",
				RoleID:       "reader",
				Tenant:       "tenant-1",
				Action:       "document.read",
				ResourceType: "document",
				Effect:       authorization.Allow,
			},
			{
				ID:           "deny-secret",
				RoleID:       "restricted",
				Tenant:       "tenant-1",
				Action:       "document.read",
				ResourceType: "document",
				ResourceID:   "secret",
				Effect:       authorization.Deny,
			},
		},
		[]rbac.Assignment{
			{
				ID:      "alice-reader",
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
				RoleID:  "reader",
				Tenant:  "tenant-1",
			},
			{
				ID:      "alice-restricted",
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
				RoleID:  "restricted",
				Tenant:  "tenant-1",
			},
		},
	)
	if err != nil {
		t.Fatalf("rbac.New() error = %v", err)
	}

	tests := map[string]struct {
		resourceID authorization.ResourceID
		want       authorization.Outcome
	}{
		"ordinary document is allowed": {
			resourceID: "ordinary",
			want:       authorization.Allow,
		},
		"explicit deny overrides another role": {
			resourceID: "secret",
			want:       authorization.Deny,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision, evaluateErr := evaluator.Evaluate(
				context.Background(),
				request(tt.resourceID, "tenant-1"),
			)
			if evaluateErr != nil {
				t.Fatalf("Evaluator.Evaluate() error = %v", evaluateErr)
			}
			if decision.Outcome != tt.want {
				t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, tt.want)
			}
		})
	}

	decision, err := evaluator.Evaluate(
		context.Background(),
		request("ordinary", "tenant-2"),
	)
	if err != nil {
		t.Fatalf("tenant-isolated Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.NotApplicable {
		t.Errorf("tenant-isolated Decision.Outcome = %v, want NotApplicable", decision.Outcome)
	}
}

func TestEvaluatorInheritsRolesAndCalculatesEffectivePermissions(t *testing.T) {
	t.Parallel()

	roles := []rbac.Role{
		{ID: "reader", Tenant: "tenant-1"},
		{ID: "contributor", Tenant: "tenant-1", Parents: []rbac.RoleID{"reader"}},
		{ID: "editor", Tenant: "tenant-1", Parents: []rbac.RoleID{"contributor"}},
	}
	permissions := []rbac.Permission{
		{
			ID:           "read",
			RoleID:       "reader",
			Tenant:       "tenant-1",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
	}
	assignments := []rbac.Assignment{
		{
			ID:      "alice-editor",
			Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
			RoleID:  "editor",
			Tenant:  "tenant-1",
		},
	}

	evaluator, err := rbac.New(roles, permissions, assignments)
	if err != nil {
		t.Fatalf("rbac.New() error = %v", err)
	}
	decision, err := evaluator.Evaluate(
		context.Background(),
		request("document-1", "tenant-1"),
	)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Allow {
		t.Errorf("Decision.Outcome = %v, want Allow", decision.Outcome)
	}

	effective, err := evaluator.EffectivePermissions(
		context.Background(),
		authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		"tenant-1",
	)
	if err != nil {
		t.Fatalf("Evaluator.EffectivePermissions() error = %v", err)
	}
	if len(effective) != 1 || effective[0].ID != "read" {
		t.Errorf("Evaluator.EffectivePermissions() = %+v, want read", effective)
	}
}

func TestNewRejectsRoleCyclesAndDepthOverflow(t *testing.T) {
	t.Parallel()

	_, err := rbac.New(
		[]rbac.Role{
			{ID: "first", Parents: []rbac.RoleID{"second"}},
			{ID: "second", Parents: []rbac.RoleID{"first"}},
		},
		nil,
		nil,
	)
	if !errors.Is(err, rbac.ErrRoleCycle) {
		t.Errorf("cyclic rbac.New() error = %v, want ErrRoleCycle", err)
	}

	_, err = rbac.New(
		[]rbac.Role{
			{ID: "first", Parents: []rbac.RoleID{"second"}},
			{ID: "second", Parents: []rbac.RoleID{"third"}},
			{ID: "third"},
		},
		nil,
		nil,
		rbac.WithLimits(rbac.Limits{MaxInheritanceDepth: 1}),
	)
	if !errors.Is(err, rbac.ErrInheritanceDepthExceeded) {
		t.Errorf("deep rbac.New() error = %v, want ErrInheritanceDepthExceeded", err)
	}

	atBoundary := []rbac.Role{
		{ID: "child", Parents: []rbac.RoleID{"parent"}},
		{ID: "parent"},
	}
	if _, err := rbac.New(
		atBoundary,
		nil,
		nil,
		rbac.WithLimits(rbac.Limits{MaxInheritanceDepth: 1}),
	); err != nil {
		t.Errorf("rbac.New(at inheritance depth) error = %v", err)
	}
	if _, err := rbac.New(
		atBoundary,
		nil,
		nil,
		rbac.WithLimits(rbac.Limits{}),
	); err != nil {
		t.Errorf("rbac.New(zero limits with inheritance) error = %v", err)
	}
}

func TestNewValidatesRolesPermissionsAndAssignments(t *testing.T) {
	t.Parallel()

	role := rbac.Role{ID: "reader", Tenant: "tenant-1"}
	permission := rbac.Permission{
		ID:           "read",
		RoleID:       role.ID,
		Tenant:       role.Tenant,
		Action:       "document.read",
		ResourceType: "document",
		Effect:       authorization.Allow,
	}
	assignment := rbac.Assignment{
		ID:      "alice-reader",
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		RoleID:  role.ID,
		Tenant:  role.Tenant,
	}

	tests := map[string]struct {
		roles       []rbac.Role
		permissions []rbac.Permission
		assignments []rbac.Assignment
		want        error
	}{
		"empty role id": {
			roles: []rbac.Role{{}},
			want:  rbac.ErrInvalidRole,
		},
		"duplicate role": {
			roles: []rbac.Role{role, role},
			want:  rbac.ErrDuplicateRole,
		},
		"unknown parent": {
			roles: []rbac.Role{{ID: "child", Parents: []rbac.RoleID{"missing"}}},
			want:  rbac.ErrUnknownParentRole,
		},
		"cross tenant parent": {
			roles: []rbac.Role{
				{ID: "parent", Tenant: "tenant-1"},
				{ID: "child", Tenant: "tenant-2", Parents: []rbac.RoleID{"parent"}},
			},
			want: rbac.ErrCrossTenantInheritance,
		},
		"invalid permission": {
			roles:       []rbac.Role{role},
			permissions: []rbac.Permission{{RoleID: role.ID}},
			want:        rbac.ErrInvalidPermission,
		},
		"permission references unknown role": {
			permissions: []rbac.Permission{permission},
			want:        rbac.ErrUnknownRole,
		},
		"permission role tenant mismatch": {
			roles: []rbac.Role{role},
			permissions: []rbac.Permission{
				withPermissionTenant(permission, "tenant-2"),
			},
			want: rbac.ErrRoleTenantMismatch,
		},
		"invalid assignment": {
			roles:       []rbac.Role{role},
			assignments: []rbac.Assignment{{RoleID: role.ID}},
			want:        rbac.ErrInvalidAssignment,
		},
		"assignment references unknown role": {
			assignments: []rbac.Assignment{assignment},
			want:        rbac.ErrUnknownRole,
		},
		"assignment role tenant mismatch": {
			roles: []rbac.Role{role},
			assignments: []rbac.Assignment{
				withAssignmentTenant(assignment, "tenant-2"),
			},
			want: rbac.ErrRoleTenantMismatch,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := rbac.New(tt.roles, tt.permissions, tt.assignments)
			if !errors.Is(err, tt.want) {
				t.Errorf("rbac.New() error = %v, want %v", err, tt.want)
			}
		})
	}

	_, err := rbac.New(
		[]rbac.Role{role},
		[]rbac.Permission{permission, permission},
		nil,
	)
	if !errors.Is(err, rbac.ErrDuplicatePermission) {
		t.Errorf("duplicate permission error = %v, want ErrDuplicatePermission", err)
	}

	_, err = rbac.New(
		[]rbac.Role{role},
		nil,
		[]rbac.Assignment{assignment, assignment},
	)
	if !errors.Is(err, rbac.ErrDuplicateAssignment) {
		t.Errorf("duplicate assignment error = %v, want ErrDuplicateAssignment", err)
	}
}

func TestEvaluatorGlobalInheritanceGroupsAndPriorityOrder(t *testing.T) {
	t.Parallel()

	roles := []rbac.Role{
		{ID: "global-reader"},
		{ID: "tenant-restricted", Tenant: "tenant-1"},
	}
	permissions := []rbac.Permission{
		{
			ID:           "allow-global",
			RoleID:       "global-reader",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
			Priority:     10,
		},
		{
			ID:           "deny-tenant",
			RoleID:       "tenant-restricted",
			Tenant:       "tenant-1",
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Deny,
			Priority:     1,
		},
	}
	assignments := []rbac.Assignment{
		{
			ID:      "alice-global",
			Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
			RoleID:  "global-reader",
		},
		{
			ID:      "editors-restricted",
			Subject: authorization.Subject{Kind: authorization.SubjectGroup, ID: "editors"},
			RoleID:  "tenant-restricted",
			Tenant:  "tenant-1",
		},
	}

	isolated, err := rbac.New(roles, permissions, assignments)
	if err != nil {
		t.Fatalf("rbac.New() error = %v", err)
	}
	inherited, err := rbac.New(
		roles,
		permissions,
		assignments,
		rbac.WithGlobalInheritance(),
	)
	if err != nil {
		t.Fatalf("rbac.New() error = %v", err)
	}

	plain := request("document-1", "tenant-1")
	decision, err := isolated.Evaluate(context.Background(), plain)
	if err != nil {
		t.Fatalf("isolated Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.NotApplicable {
		t.Errorf("isolated Decision.Outcome = %v, want NotApplicable", decision.Outcome)
	}

	decision, err = inherited.Evaluate(context.Background(), plain)
	if err != nil {
		t.Fatalf("inherited Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Allow {
		t.Errorf("inherited Decision.Outcome = %v, want Allow", decision.Outcome)
	}

	plain.Subject.Groups = []authorization.SubjectID{"editors", "editors"}
	decision, err = inherited.Evaluate(context.Background(), plain)
	if err != nil {
		t.Fatalf("group Evaluate() error = %v", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != rbac.ReasonExplicitDeny {
		t.Errorf("group Decision = %+v, want explicit deny", decision)
	}
	assertPolicyIDs(
		t,
		decision.MatchedPolicyIDs,
		[]authorization.PolicyID{"allow-global", "deny-tenant"},
	)
}

func TestEvaluatorEnforcesLimitsCancellationAndBatchBounds(t *testing.T) {
	t.Parallel()

	role := rbac.Role{ID: "reader", Tenant: "tenant-1"}
	permissions := []rbac.Permission{
		{
			ID:           "first",
			RoleID:       role.ID,
			Tenant:       role.Tenant,
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
		{
			ID:           "second",
			RoleID:       role.ID,
			Tenant:       role.Tenant,
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
	}
	assignment := rbac.Assignment{
		ID:      "alice-reader",
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		RoleID:  role.ID,
		Tenant:  role.Tenant,
	}

	if _, err := rbac.New(
		[]rbac.Role{role}, permissions, []rbac.Assignment{assignment},
		rbac.WithLimits(rbac.Limits{MaxPermissions: 1}),
	); !errors.Is(err, rbac.ErrPermissionLimitExceeded) {
		t.Errorf("permission-limited rbac.New() error = %v, want ErrPermissionLimitExceeded", err)
	}
	if _, err := rbac.New(
		[]rbac.Role{role}, permissions[:1], []rbac.Assignment{assignment},
		rbac.WithLimits(rbac.Limits{
			MaxRoles: 1, MaxPermissions: 1, MaxAssignments: 1,
		}),
	); err != nil {
		t.Errorf("rbac.New(at construction limits) error = %v", err)
	}
	if _, err := rbac.New(
		[]rbac.Role{role}, nil, nil,
		rbac.WithLimits(rbac.Limits{MaxRoles: 1}),
	); err != nil {
		t.Fatalf("role limit boundary rbac.New() error = %v", err)
	}
	if _, err := rbac.New(
		[]rbac.Role{role, {ID: "second", Tenant: role.Tenant}}, nil, nil,
		rbac.WithLimits(rbac.Limits{MaxRoles: 1}),
	); !errors.Is(err, rbac.ErrRoleLimitExceeded) {
		t.Errorf("role-limited rbac.New() error = %v, want ErrRoleLimitExceeded", err)
	}
	if _, err := rbac.New(
		[]rbac.Role{role}, nil, []rbac.Assignment{assignment, {
			ID:      "second",
			Subject: assignment.Subject,
			RoleID:  role.ID,
			Tenant:  role.Tenant,
		}},
		rbac.WithLimits(rbac.Limits{MaxAssignments: 1}),
	); !errors.Is(err, rbac.ErrAssignmentLimitExceeded) {
		t.Errorf("assignment-limited rbac.New() error = %v, want ErrAssignmentLimitExceeded", err)
	}

	defaultLimited, err := rbac.New(
		[]rbac.Role{role},
		permissions[:1],
		[]rbac.Assignment{assignment},
		rbac.WithLimits(rbac.Limits{}),
	)
	if err != nil {
		t.Fatalf("rbac.New(zero limits) error = %v", err)
	}
	defaultRequest := request("document-1", "tenant-1")
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

	evaluator, err := rbac.New(
		[]rbac.Role{role}, permissions, []rbac.Assignment{assignment},
		rbac.WithLimits(rbac.Limits{
			MaxGroups:    1,
			MaxMatches:   1,
			MaxBatchSize: 2,
		}),
	)
	if err != nil {
		t.Fatalf("rbac.New() error = %v", err)
	}

	decision, err = evaluator.Evaluate(context.Background(), request("document-1", "tenant-1"))
	if !errors.Is(err, rbac.ErrMatchLimitExceeded) {
		t.Fatalf("match-limited Evaluate() error = %v, want ErrMatchLimitExceeded", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != rbac.ReasonLimitExceeded {
		t.Errorf("match-limited Decision = %+v, want limit deny", decision)
	}

	groupRequest := request("document-1", "tenant-1")
	groupRequest.Subject.Groups = []authorization.SubjectID{"one", "two"}
	decision, err = evaluator.Evaluate(context.Background(), groupRequest)
	if !errors.Is(err, rbac.ErrGroupLimitExceeded) {
		t.Fatalf("group-limited Evaluate() error = %v, want ErrGroupLimitExceeded", err)
	}

	groupRequest.Subject.Groups = groupRequest.Subject.Groups[:1]
	decision, err = evaluator.Evaluate(context.Background(), groupRequest)
	if !errors.Is(err, rbac.ErrMatchLimitExceeded) {
		t.Errorf("Evaluate(at group limit) error = %v, want ErrMatchLimitExceeded", err)
	}
	groupRequest.Subject.Groups = []authorization.SubjectID{"one", "two"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err = evaluator.Evaluate(ctx, request("document-1", "tenant-1"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Evaluate() error = %v, want context.Canceled", err)
	}
	if decision.Reason != authorization.ReasonContextCanceled {
		t.Errorf("canceled Decision = %+v, want cancellation reason", decision)
	}

	requests := []authorization.Request{
		request("document-1", "tenant-1"),
		request("document-2", "tenant-1"),
	}
	_, err = evaluator.EvaluateBatch(context.Background(), append(requests, requests[0]))
	if !errors.Is(err, rbac.ErrBatchLimitExceeded) {
		t.Errorf("oversized EvaluateBatch() error = %v, want ErrBatchLimitExceeded", err)
	}
	if _, err := evaluator.EvaluateBatch(
		context.Background(),
		requests,
	); !errors.Is(err, rbac.ErrMatchLimitExceeded) ||
		errors.Is(err, rbac.ErrBatchLimitExceeded) {
		t.Errorf("EvaluateBatch(at batch limit) error = %v, want match limit", err)
	}

	batch, err := evaluator.EvaluateBatch(context.Background(), []authorization.Request{groupRequest})
	if !errors.Is(err, rbac.ErrGroupLimitExceeded) {
		t.Errorf("failing EvaluateBatch() error = %v, want ErrGroupLimitExceeded", err)
	}
	if len(batch) != 1 || batch[0].Outcome != authorization.Deny {
		t.Errorf("failing EvaluateBatch() = %+v, want one deny", batch)
	}
}

func withPermissionTenant(
	permission rbac.Permission,
	tenant authorization.TenantID,
) rbac.Permission {
	permission.Tenant = tenant
	return permission
}

func withAssignmentTenant(
	assignment rbac.Assignment,
	tenant authorization.TenantID,
) rbac.Assignment {
	assignment.Tenant = tenant
	return assignment
}

func assertPolicyIDs(
	t *testing.T,
	got []authorization.PolicyID,
	want []authorization.PolicyID,
) {
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

func request(resourceID authorization.ResourceID, tenant authorization.TenantID) authorization.Request {
	return authorization.Request{
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		Action:  "document.read",
		Resource: authorization.Resource{
			Type: "document",
			ID:   resourceID,
		},
		Tenant: tenant,
	}
}
