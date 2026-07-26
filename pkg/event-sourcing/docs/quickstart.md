# Five-minute quickstart

This example event-sources one aggregate with the conformant in-memory store.
The same aggregate and repository composition can use the PostgreSQL adapter
without changing domain behavior.

```go
package account

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

type Account struct {
	id        string
	owner     string
	lifecycle eventsourcing.Lifecycle
}

type Opened struct {
	Owner string `json:"owner"`
}

func (account *Account) Open(owner string) error {
	if account.owner != "" || owner == "" {
		return errors.New("account cannot be opened")
	}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "account.opened",
			Version: 1,
			Value:   Opened{Owner: owner},
		},
	)
	if err != nil {
		return err
	}
	return account.lifecycle.Record(event, account.apply)
}

func (account *Account) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case Opened:
		account.owner = value.Owner
		return nil
	default:
		return eventsourcing.ErrUnknownEvent
	}
}

func NewRepository(
	store eventsourcing.EventStore,
) (*eventsourcing.AggregateRepository[string, *Account], error) {
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[Opened]("account.opened", 1),
	)
	if err != nil {
		return nil, err
	}
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		return nil, err
	}
	decorators, err := eventsourcing.NewMessageDecoratorChain()
	if err != nil {
		return nil, err
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher()
	if err != nil {
		return nil, err
	}
	return eventsourcing.NewRepository(
		eventsourcing.RepositoryConfig[string, *Account]{
			AggregateType: "account",
			EncodeID: func(id string) (string, error) {
				if id == "" {
					return "", eventsourcing.ErrInvalidArgument
				}
				return id, nil
			},
			Identify: func(account *Account) string {
				return account.id
			},
			NewAggregate: func(id string) (*Account, error) {
				return &Account{id: id}, nil
			},
			Lifecycle: func(
				account *Account,
			) *eventsourcing.Lifecycle {
				return &account.lifecycle
			},
			Apply: func(
				account *Account,
				event eventsourcing.DecodedEvent,
			) error {
				return account.apply(event)
			},
			Store:      store,
			Codec:      codec,
			Upcasters:  upcasters,
			Clock:      eventsourcing.SystemClock{},
			MessageIDs: eventsourcing.NewCryptoRandomMessageIDGenerator(),
			Decorators: decorators,
			Dispatcher: dispatcher,
		},
	)
}

func Example() error {
	store := memory.NewStore()
	repository, err := NewRepository(store)
	if err != nil {
		return err
	}

	account := &Account{id: "account-42"}
	if err := account.Open("Ada"); err != nil {
		return err
	}
	result, err := repository.Save(context.Background(), account)
	if err != nil {
		return err
	}
	if !result.Persisted() {
		return errors.New("account was not persisted")
	}

	loaded, err := repository.Load(
		context.Background(),
		"account-42",
	)
	if err != nil {
		return err
	}
	if loaded.owner != "Ada" ||
		loaded.lifecycle.CommittedVersion() != 1 {
		return errors.New("reconstitution mismatch")
	}
	return nil
}
```

The lifecycle immediately applies `Opened` when behavior records it. `Save`
encodes the pending event, appends it with optimistic concurrency, acknowledges
the exact persisted message, and then invokes the selected dispatcher. `Load`
reads the ordered stream, decodes known events, and reconstitutes a fresh
aggregate without external side effects.

The in-memory store is process-local and not durable. Replace only `store` with
the PostgreSQL adapter for durable persistence. Add consumers, metadata
decorators, upcasters, snapshots, projections, or asynchronous adapters only
when their explicit policies are needed.
