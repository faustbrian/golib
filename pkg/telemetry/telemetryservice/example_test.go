package telemetryservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/telemetry"
	"github.com/faustbrian/golib/pkg/telemetry/telemetryservice"
)

func ExampleNew() {
	config := telemetry.DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Enabled = false
	config.Metrics.Enabled = false

	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:    "telemetry",
		Config:  config,
		Failure: telemetryservice.FailureRequired,
	})
	if err != nil {
		fmt.Println("adapter setup failed")

		return
	}

	component := adapter.Component()
	_ = component.Start(context.Background())
	runtime, active := adapter.Runtime()
	_ = component.Stop(context.Background())

	fmt.Println(runtime != nil, active)
	// Output: true true
}
