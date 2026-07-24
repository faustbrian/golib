package abac_test

import (
	"context"
	"fmt"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/abac"
)

func Example() {
	evaluator, err := abac.New(
		[]abac.Rule{
			{
				ID:           "finance-read",
				Action:       "report.read",
				ResourceType: "report",
				Effect:       authorization.Allow,
				Condition: abac.Equal(
					abac.Reference{Source: abac.Subject, Name: "department"},
					authorization.StringValue("finance"),
				),
			},
		},
		nil,
	)
	if err != nil {
		panic(err)
	}

	decision, err := evaluator.Evaluate(context.Background(), authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "alice",
			Attributes: authorization.Attributes{
				"department": authorization.StringValue("finance"),
			},
		},
		Action:   "report.read",
		Resource: authorization.Resource{Type: "report", ID: "quarterly"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(decision.Outcome == authorization.Allow)
	// Output: true
}
