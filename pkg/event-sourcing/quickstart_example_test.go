package eventsourcing_test

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

type quickstartAccount struct {
	id        string
	owner     string
	lifecycle eventsourcing.Lifecycle
}

type quickstartOpened struct {
	Owner string `json:"owner"`
}

func (account *quickstartAccount) open(owner string) error {
	if account.owner != "" || owner == "" {
		return errors.New("account cannot be opened")
	}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "account.opened",
			Version: 1,
			Value:   quickstartOpened{Owner: owner},
		},
	)
	if err != nil {
		return err
	}
	return account.lifecycle.Record(event, account.apply)
}

func (account *quickstartAccount) apply(
	event eventsourcing.DecodedEvent,
) error {
	switch value := event.Value().(type) {
	case quickstartOpened:
		account.owner = value.Owner
		return nil
	default:
		return eventsourcing.ErrUnknownEvent
	}
}

func quickstartRepository(
	store eventsourcing.EventStore,
) (*eventsourcing.AggregateRepository[string, *quickstartAccount], error) {
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[quickstartOpened]("account.opened", 1),
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
		eventsourcing.RepositoryConfig[string, *quickstartAccount]{
			AggregateType: "account",
			EncodeID: func(id string) (string, error) {
				if id == "" {
					return "", eventsourcing.ErrInvalidArgument
				}
				return id, nil
			},
			Identify: func(account *quickstartAccount) string {
				return account.id
			},
			NewAggregate: func(
				id string,
			) (*quickstartAccount, error) {
				return &quickstartAccount{id: id}, nil
			},
			Lifecycle: func(
				account *quickstartAccount,
			) *eventsourcing.Lifecycle {
				return &account.lifecycle
			},
			Apply: func(
				account *quickstartAccount,
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

func Example_fiveMinuteQuickstart() {
	store := memory.NewStore()
	repository, err := quickstartRepository(store)
	if err != nil {
		panic(err)
	}
	account := &quickstartAccount{id: "account-42"}
	if err := account.open("Ada"); err != nil {
		panic(err)
	}
	result, err := repository.Save(context.Background(), account)
	if err != nil {
		panic(err)
	}
	if !result.Persisted() {
		panic("account was not persisted")
	}
	loaded, err := repository.Load(
		context.Background(),
		"account-42",
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf(
		"%s at version %d\n",
		loaded.owner,
		loaded.lifecycle.CommittedVersion(),
	)
	// Output: Ada at version 1
}
