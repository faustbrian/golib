package hedge_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func ExampleDo() {
	budget, _ := hedge.NewOutstandingBudget(1)
	policy, _ := hedge.NewPolicy(hedge.Config[string]{
		MaxHedges:      1,
		ReplaySafe:     true,
		Delay:          time.Hour,
		TotalTimeout:   time.Second,
		CleanupTimeout: time.Second,
		Clock:          hedge.RealClock{},
		Budget:         budget,
		Classifier: hedge.ClassifyFunc[string](func(_ context.Context, result hedge.AttemptResult[string]) (hedge.Classification, error) {
			if result.Err == nil {
				return hedge.ClassificationSuccess, nil
			}
			return hedge.ClassificationFailure, nil
		}),
		Disposer:           hedge.DisposeFunc[string](func(context.Context, string) error { return nil }),
		Resource:           "profile-read",
		FactoryFailureMode: hedge.FactoryFailureStop,
	})
	value, report, err := hedge.Do(context.Background(), policy,
		hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			return func(context.Context) (string, error) { return "profile", nil }, "pod-a", nil
		}))
	fmt.Println(value, report.Reason == hedge.ReasonNoHedgeNeeded, err)
	// Output: profile true <nil>
}
