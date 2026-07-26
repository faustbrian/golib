package rbac_test

import (
	"context"
	"fmt"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func Example() {
	evaluator, err := rbac.New(
		[]rbac.Role{{ID: "reader", Tenant: "acme"}},
		[]rbac.Permission{
			{
				ID:           "read-documents",
				RoleID:       "reader",
				Tenant:       "acme",
				Action:       "document.read",
				ResourceType: "document",
				Effect:       authorization.Allow,
			},
		},
		[]rbac.Assignment{
			{
				ID: "alice-reader",
				Subject: authorization.Subject{
					Kind: authorization.SubjectUser,
					ID:   "alice",
				},
				RoleID: "reader",
				Tenant: "acme",
			},
		},
	)
	if err != nil {
		panic(err)
	}

	decision, err := evaluator.Evaluate(context.Background(), authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "alice",
		},
		Action: "document.read",
		Resource: authorization.Resource{
			Type: "document",
			ID:   "document-1",
		},
		Tenant: "acme",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(decision.Outcome == authorization.Allow)
	// Output: true
}
