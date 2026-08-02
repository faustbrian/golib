package concurrencylimit_test

import (
	"context"
	"fmt"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func ExampleExecute() {
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit:     1,
		MaxLimit:     100,
		InitialLimit: 10,
		Algorithm:    concurrencylimit.NewDefaultAlgorithm(),
	})
	if err != nil {
		panic(err)
	}

	value, err := concurrencylimit.Execute(context.Background(), limiter,
		func(context.Context) (string, error) {
			return "accepted", nil
		})
	fmt.Println(value, err, limiter.Snapshot().Outcomes.Success)
	// Output: accepted <nil> 1
}
