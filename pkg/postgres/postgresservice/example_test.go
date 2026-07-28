package postgresservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/postgres"
	"github.com/faustbrian/golib/pkg/postgres/postgresservice"
)

var _ postgresservice.Resource = (*postgres.Pool)(nil)

func ExampleNew() {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name:              "database",
		Pool:              pool,
		TransferOwnership: true,
		StartupPing:       true,
	})
	if err != nil {
		fmt.Println("adapter setup failed")

		return
	}

	component := adapter.Component()
	_ = component.Start(context.Background())
	check := adapter.Readiness()
	_ = check.Run(context.Background())
	_ = component.Stop(context.Background())

	fmt.Println(pool.pings, pool.closes)
	// Output: 2 1
}
