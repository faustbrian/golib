package abac

import (
	"context"
	"fmt"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func BenchmarkEvaluatePredicateCount(b *testing.B) {
	for _, count := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("predicates-%d", count), func(b *testing.B) {
			conditions := make([]Condition, count)
			attributes := make(authorization.Attributes, count)
			for index := range conditions {
				name := authorization.AttributeName(fmt.Sprintf("attribute-%03d", index))
				conditions[index] = Equal(
					Reference{Source: Request, Name: name},
					authorization.IntValue(int64(index)),
				)
				attributes[name] = authorization.IntValue(int64(index))
			}
			evaluator, err := New([]Rule{{
				ID: "rule", Tenant: "tenant-1", Action: "read",
				ResourceType: "document", Effect: authorization.Allow,
				Condition: All(conditions...),
			}}, nil)
			if err != nil {
				b.Fatal(err)
			}
			request := authorization.Request{
				Subject: authorization.Subject{Kind: authorization.SubjectUser, ID: "user-1"},
				Action:  "read", Resource: authorization.Resource{Type: "document"},
				Tenant: "tenant-1", Attributes: attributes,
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
