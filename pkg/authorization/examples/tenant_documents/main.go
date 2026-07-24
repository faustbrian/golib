// Package main demonstrates composing tenant RBAC, resource ACL, and a trusted
// ownership attribute under deny-overrides semantics.
package main

import (
	"context"
	"fmt"
	"log"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/abac"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func main() {
	rolePolicy, err := rbac.New(
		[]rbac.Role{{ID: "reader", Tenant: "tenant-1"}},
		[]rbac.Permission{{
			ID: "documents-read", RoleID: "reader", Tenant: "tenant-1",
			Action: "document.read", ResourceType: "document",
			Effect: authorization.Allow,
		}},
		[]rbac.Assignment{{
			ID: "alice-reader", RoleID: "reader", Tenant: "tenant-1",
			Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		}},
	)
	if err != nil {
		log.Fatal(err)
	}

	accessList, err := acl.New([]acl.Entry{{
		ID: "protected-delete", Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "alice",
		},
		Action: "document.delete", ResourceType: "document",
		ResourceID: "document-1", Tenant: "tenant-1",
		Effect: authorization.Deny,
	}})
	if err != nil {
		log.Fatal(err)
	}

	ownerPolicy, err := abac.New([]abac.Rule{{
		ID: "owner-update", Tenant: "tenant-1", Action: "document.update",
		ResourceType: "document", Effect: authorization.Allow,
		Condition: abac.Equal(
			abac.Reference{Source: abac.Resource, Name: "owned_by_subject"},
			authorization.BoolValue(true),
		),
	}}, nil)
	if err != nil {
		log.Fatal(err)
	}

	snapshot, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "tenant-rbac", Evaluator: rolePolicy},
		authorization.PolicyDefinition{ID: "resource-acl", Evaluator: accessList},
		authorization.PolicyDefinition{ID: "ownership-abac", Evaluator: ownerPolicy},
	)
	if err != nil {
		log.Fatal(err)
	}
	engine, err := authorization.NewEngine(snapshot)
	if err != nil {
		log.Fatal(err)
	}

	request := authorization.Request{
		Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		Action:  "document.read",
		Resource: authorization.Resource{
			Type: "document", ID: "document-1",
			Attributes: authorization.Attributes{
				"owned_by_subject": authorization.BoolValue(true),
			},
		},
		Tenant: "tenant-1",
	}
	decision, err := engine.Decide(context.Background(), request)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s revision=%d\n", decision.Outcome, decision.Revision)
}
