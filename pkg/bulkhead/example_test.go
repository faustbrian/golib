package bulkhead_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func ExampleExecute() {
	database, err := bulkhead.New(bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  2,
		Admission: bulkhead.Wait{MaxQueued: 4, MaxWait: 25 * time.Millisecond},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	value, timing, err := bulkhead.Execute(
		context.Background(),
		database,
		1,
		func(context.Context) (string, error) {
			return "available", nil
		},
	)
	fmt.Println(value, err)
	fmt.Println(timing.Resource, timing.Weight)
	// Output:
	// available <nil>
	// inventory-db 1
}

func ExampleRegistry() {
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: 2})
	if err != nil {
		fmt.Println(err)
		return
	}
	inventory, err := registry.Create(bulkhead.Config{Resource: "inventory-db", Capacity: 1})
	if err != nil {
		fmt.Println(err)
		return
	}
	payments, err := registry.Create(bulkhead.Config{Resource: "payments-api", Capacity: 1})
	if err != nil {
		fmt.Println(err)
		return
	}

	inventoryPermit, err := inventory.Acquire(context.Background(), 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	paymentPermit, paymentErr := payments.Acquire(context.Background(), 1)
	if paymentErr != nil {
		fmt.Println(paymentErr)
		return
	}
	fmt.Println(paymentErr, registry.Len())
	_ = inventoryPermit.Release()
	_ = paymentPermit.Release()
	// Output:
	// <nil> 2
}

func ExampleBulkhead_Drain() {
	database, err := bulkhead.New(bulkhead.Config{Resource: "inventory-db", Capacity: 1})
	if err != nil {
		fmt.Println(err)
		return
	}
	permit, err := database.Acquire(context.Background(), 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = database.Close()
	_ = permit.Release()

	drainContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	fmt.Println(database.Drain(drainContext))
	// Output:
	// <nil>
}
