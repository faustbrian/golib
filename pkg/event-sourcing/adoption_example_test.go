package eventsourcing_test

import "fmt"

type conventionalAccount struct {
	owner string
}

func Example_adoptionPersistenceChoice() {
	current := conventionalAccount{owner: "Ada"}

	sourced := &quickstartAccount{id: "account-42"}
	if err := sourced.open("Ada"); err != nil {
		panic(err)
	}
	changes, err := sourced.lifecycle.Changes()
	if err != nil {
		panic(err)
	}
	event := changes.Events()[0]

	fmt.Printf("current state: owner=%s\n", current.owner)
	fmt.Printf(
		"event history: name=%s schema=%d pending=%d\n",
		event.Name(),
		event.Version(),
		changes.Len(),
	)
	// Output:
	// current state: owner=Ada
	// event history: name=account.opened schema=1 pending=1
}
