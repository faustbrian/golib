package authorization_test

import (
	"context"
	"fmt"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/abac"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	"github.com/faustbrian/golib/pkg/authorization/authorizationtest"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

type modelFactory func(authorization.Outcome) (authorization.Evaluator, error)

func TestPolicyModelsConformToCommonOutcomes(t *testing.T) {
	t.Parallel()

	models := map[string]modelFactory{
		"ACL":  aclModel,
		"RBAC": rbacModel,
		"ABAC": abacModel,
	}
	for name, factory := range models {
		name, factory := name, factory
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []authorization.Outcome{
				authorization.NotApplicable,
				authorization.Allow,
				authorization.Deny,
			} {
				evaluator, err := factory(want)
				if err != nil {
					t.Fatalf("factory(%s) error = %v", want, err)
				}
				request := authorizationtest.NewRequest().
					WithAttribute("fixture", authorization.BoolValue(true)).
					Build()
				decision, err := evaluator.Evaluate(context.Background(), request)
				if err != nil {
					t.Fatalf("Evaluate(%s) error = %v", want, err)
				}
				if decision.Outcome != want {
					t.Errorf("Evaluate(%s) outcome = %s", want, decision.Outcome)
				}
			}
		})
	}
}

func aclModel(outcome authorization.Outcome) (authorization.Evaluator, error) {
	if outcome == authorization.NotApplicable {
		return acl.New(nil)
	}
	return acl.New([]acl.Entry{{
		ID: "fixture", Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "user-1",
		},
		Action: "read", ResourceType: "document", ResourceID: "document-1",
		Tenant: "tenant-1", Effect: outcome,
	}})
}

func rbacModel(outcome authorization.Outcome) (authorization.Evaluator, error) {
	if outcome == authorization.NotApplicable {
		return rbac.New(nil, nil, nil)
	}
	return rbac.New(
		[]rbac.Role{{ID: "reader", Tenant: "tenant-1"}},
		[]rbac.Permission{{
			ID: "fixture", RoleID: "reader", Tenant: "tenant-1",
			Action: "read", ResourceType: "document", ResourceID: "document-1",
			Effect: outcome,
		}},
		[]rbac.Assignment{{
			ID: "assignment", RoleID: "reader", Tenant: "tenant-1",
			Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
		}},
	)
}

func abacModel(outcome authorization.Outcome) (authorization.Evaluator, error) {
	if outcome == authorization.NotApplicable {
		return abac.New(nil, nil)
	}
	if outcome != authorization.Allow && outcome != authorization.Deny {
		return nil, fmt.Errorf("unsupported fixture outcome %s", outcome)
	}
	return abac.New([]abac.Rule{{
		ID: "fixture", Tenant: "tenant-1", Action: "read",
		ResourceType: "document", ResourceID: "document-1", Effect: outcome,
		Condition: abac.Equal(
			abac.Reference{Source: abac.Request, Name: "fixture"},
			authorization.BoolValue(true),
		),
	}}, nil)
}
