package acl_test

import (
	"context"
	"fmt"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/acl"
)

func Example() {
	accessList, err := acl.New([]acl.Entry{
		{
			ID: "reader",
			Subject: authorization.Subject{
				Kind: authorization.SubjectUser,
				ID:   "alice",
			},
			Action:       "document.read",
			ResourceType: "document",
			Effect:       authorization.Allow,
		},
	})
	if err != nil {
		panic(err)
	}

	decision, err := accessList.Evaluate(context.Background(), authorization.Request{
		Subject: authorization.Subject{
			Kind: authorization.SubjectUser,
			ID:   "alice",
		},
		Action: "document.read",
		Resource: authorization.Resource{
			Type: "document",
			ID:   "document-1",
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(decision.Outcome == authorization.Allow)
	// Output: true
}
