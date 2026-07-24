package correlation_test

import (
	"fmt"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

func ExampleFactory_Next() {
	generator := &sequenceGenerator{values: []string{"child-request"}}
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{Generator: generator})
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("workflow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("parent-request", correlation.Policy{}),
	}
	child, _ := factory.Next(parent)
	fmt.Println(child.CorrelationID, child.RequestID, child.CausationID)
	// Output: workflow child-request parent-request
}
