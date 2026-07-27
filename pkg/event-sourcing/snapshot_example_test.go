package eventsourcing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	snapshotpkg "github.com/faustbrian/golib/pkg/event-sourcing/snapshot"
)

type quickstartSnapshotState struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
}

type quickstartSnapshotCodec struct{}

func (quickstartSnapshotCodec) SchemaVersion() eventsourcing.SchemaVersion {
	return 1
}

func (quickstartSnapshotCodec) Encode(
	account *quickstartAccount,
) ([]byte, error) {
	return json.Marshal(quickstartSnapshotState{
		ID:    account.id,
		Owner: account.owner,
	})
}

func (quickstartSnapshotCodec) Decode(
	state []byte,
	_ map[string]string,
) (*quickstartAccount, error) {
	var decoded quickstartSnapshotState
	if err := json.Unmarshal(state, &decoded); err != nil {
		return nil, err
	}

	return &quickstartAccount{
		id:    decoded.ID,
		owner: decoded.Owner,
	}, nil
}

func Example_snapshotRestoration() {
	ctx := context.Background()
	store := memory.NewStore()
	repository, err := quickstartRepository(store)
	if err != nil {
		panic(err)
	}
	account := &quickstartAccount{id: "account-42"}
	if err := account.open("Ada"); err != nil {
		panic(err)
	}
	if _, err := repository.Save(ctx, account); err != nil {
		panic(err)
	}
	clock, err := eventsourcing.NewFixedClock(
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		panic(err)
	}
	manager, err := snapshotpkg.NewManager(
		snapshotpkg.ManagerConfig[string, *quickstartAccount]{
			AggregateType: "account",
			EncodeID: func(id string) (string, error) {
				return id, nil
			},
			Identify: func(account *quickstartAccount) string {
				return account.id
			},
			Lifecycle: func(
				account *quickstartAccount,
			) *eventsourcing.Lifecycle {
				return &account.lifecycle
			},
			Repository: repository,
			Store:      memory.NewSnapshotStore(),
			Codec:      quickstartSnapshotCodec{},
			Clock:      clock,
			Fallback:   snapshotpkg.FailClosed(),
		},
	)
	if err != nil {
		panic(err)
	}
	if _, err := manager.Refresh(ctx, account.id, account); err != nil {
		panic(err)
	}
	restored, info, err := manager.Load(ctx, account.id)
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"snapshot=%d owner=%s committed=%d\n",
		info.SnapshotVersion(),
		restored.owner,
		restored.lifecycle.CommittedVersion(),
	)
	// Output: snapshot=1 owner=Ada committed=1
}
