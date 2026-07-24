package rbac

import (
	"context"
	"fmt"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func BenchmarkEvaluateInheritanceDepth(b *testing.B) {
	for _, depth := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			roles := make([]Role, depth)
			for index := range roles {
				roles[index] = Role{ID: RoleID(fmt.Sprintf("role-%02d", index)), Tenant: "tenant-1"}
				if index > 0 {
					roles[index].Parents = []RoleID{roles[index-1].ID}
				}
			}
			evaluator, err := New(roles, []Permission{{
				ID: "read", RoleID: roles[0].ID, Tenant: "tenant-1",
				Action: "read", ResourceType: "document", Effect: authorization.Allow,
			}}, []Assignment{{
				ID: "assignment", RoleID: roles[depth-1].ID, Tenant: "tenant-1",
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
			}})
			if err != nil {
				b.Fatal(err)
			}
			request := authorization.Request{
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
				Action:  "read", Resource: authorization.Resource{Type: "document"},
				Tenant: "tenant-1",
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := evaluator.Evaluate(context.Background(), request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
