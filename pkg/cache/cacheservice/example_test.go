package cacheservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/cache/cacheservice"
)

func ExampleNew() {
	value := &resource{}
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
		Readiness: func(context.Context, *resource) error {
			return nil
		},
	})
	if err != nil {
		fmt.Println("adapter setup failed")

		return
	}

	component := adapter.Component()
	_ = component.Start(context.Background())
	check, _ := adapter.Readiness()
	_ = check.Run(context.Background())
	_ = component.Stop(context.Background())

	fmt.Println(adapter.Resource() == value)
	// Output: true
}
