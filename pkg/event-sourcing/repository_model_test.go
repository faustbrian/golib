package eventsourcing_test

import (
	"context"
	"strconv"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func FuzzAggregateRepositoryLiveAndReplayAreEquivalent(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 1, 2, 3, 255})
	f.Add([]byte("a bounded event history"))

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			operations = operations[:128]
		}
		store := memory.NewStore()
		upcasters, err := eventsourcing.NewUpcasterChain()
		if err != nil {
			t.Fatal(err)
		}
		repository := newAccountRepository(
			t,
			store,
			repositoryCodec(t),
			upcasters,
			nil,
			nil,
		)
		live := &repositoryAccount{id: "model-account"}
		recordRepositoryEvent(
			t,
			live,
			"account.opened",
			repositoryAccountOpened{Owner: "initial-owner"},
		)
		for index, operation := range operations {
			switch operation % 2 {
			case 0:
				recordRepositoryEvent(
					t,
					live,
					"account.owner-set",
					repositoryOwnerSet{
						Owner: "owner-" +
							strconv.Itoa(int(operation)) +
							"-" +
							strconv.Itoa(index),
					},
				)
			case 1:
				recordRepositoryEvent(
					t,
					live,
					"account.email-changed",
					repositoryEmailChanged{
						Email: "email-" +
							strconv.Itoa(index) +
							"-" +
							strconv.Itoa(int(operation)),
					},
				)
			}
		}

		if _, err := repository.Save(context.Background(), live); err != nil {
			t.Fatalf("save live aggregate: %v", err)
		}
		replayed, err := repository.Load(
			context.Background(),
			"model-account",
		)
		if err != nil {
			t.Fatalf("replay aggregate: %v", err)
		}
		if replayed.owner != live.owner ||
			replayed.email != live.email ||
			replayed.lifecycle.CommittedVersion() !=
				live.lifecycle.CommittedVersion() ||
			replayed.lifecycle.Version() != live.lifecycle.Version() {
			t.Fatalf(
				"live/replay mismatch: live=%q/%q/%d/%d replay=%q/%q/%d/%d",
				live.owner,
				live.email,
				live.lifecycle.CommittedVersion(),
				live.lifecycle.Version(),
				replayed.owner,
				replayed.email,
				replayed.lifecycle.CommittedVersion(),
				replayed.lifecycle.Version(),
			)
		}
	})
}

func recordRepositoryEvent(
	t testing.TB,
	account *repositoryAccount,
	name string,
	value any,
) {
	t.Helper()

	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    name,
			Version: 1,
			Value:   value,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := account.lifecycle.Record(event, account.apply); err != nil {
		t.Fatal(err)
	}
}
