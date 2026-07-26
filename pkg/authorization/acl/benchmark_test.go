package acl

import (
	"context"
	"fmt"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func BenchmarkEvaluateByPolicySize(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 10_000} {
		b.Run(fmt.Sprintf("entries-%d", size), func(b *testing.B) {
			entries := make([]Entry, size)
			for index := range entries {
				entries[index] = Entry{
					ID: authorization.PolicyID(fmt.Sprintf("entry-%05d", index)),
					Subject: authorization.Subject{
						Kind: authorization.SubjectUser,
						ID:   "user-1",
					},
					Action: "read", ResourceType: "document",
					ResourceID: authorization.ResourceID(fmt.Sprintf("document-%05d", index)),
					Tenant:     "tenant-1", Effect: authorization.Allow,
				}
			}
			evaluator, err := New(entries)
			if err != nil {
				b.Fatal(err)
			}
			request := authorization.Request{
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
				Action:  "read", Resource: authorization.Resource{
					Type: "document", ID: authorization.ResourceID(fmt.Sprintf("document-%05d", size-1)),
				},
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
