# Five-minute quickstart

Install the module:

```sh
go get github.com/faustbrian/golib/pkg/idempotency
```

The in-memory adapter is useful for learning the contract and deterministic
tests. It is not durable and does not coordinate multiple processes.

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func ownerToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func main() {
	ctx := context.Background()
	store, err := memory.New(memory.Options{
		Clock:       clock.System{},
		OwnerTokens: ownerToken,
	})
	if err != nil {
		panic(err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		panic(err)
	}

	key, err := idempotency.NewKey(
		"billing", "tenant-42", "create-invoice", "api-client-7", "request-123",
	)
	if err != nil {
		panic(err)
	}
	fingerprint, err := idempotency.NewFingerprint("invoice-v1", []byte("invoice:9001:EUR"))
	if err != nil {
		panic(err)
	}

	request := idempotency.BeginRequest{Acquire: idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: 30 * time.Second,
	}}
	first, err := service.Begin(ctx, request)
	if err != nil {
		panic(err)
	}
	if first.Execute {
		// Commit the business effect with first.Record.FencingToken before
		// recording completion. See the fencing section below.
		_, err = service.Complete(ctx, idempotency.CompleteRequest{
			Ownership: first.Record.Ownership(),
			Result:    []byte(`{"invoice_id":"inv-9001"}`),
		})
		if err != nil {
			panic(err)
		}
	}

	retry, err := service.Begin(ctx, request)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %s\n", retry.Outcome, retry.Record.Result)
	// Output: replayed: {"invoice_id":"inv-9001"}
}
```

`Begin` defaults to `AvailabilityFailClosed`: a datastore error returns
`unavailable` and does not authorize execution. `AvailabilityAllowUntracked`
exists only for explicitly duplicate-tolerant work. It returns `Execute=true`
and `Durable=false`; there is no ownership proof to heartbeat or complete.

## Choose a durable adapter

Use the [PostgreSQL adapter](postgres.md) when the business write and
idempotency completion can share a `pgx.Tx`, or when PostgreSQL is already the
operational source of truth. Use the [Valkey adapter](valkey.md) for low-latency
single-key transitions when a dedicated Valkey 9 deployment can guarantee
`noeviction`. Apply migrations or startup safety checks before serving traffic.

Do not use the memory adapter for multiple processes, restarts, or durable
replay.

## Carry the fence to the side effect

Acquisition makes the caller the current owner of the idempotency record. It
does not stop an expired process from continuing to run. Read the proof in an
integration handler with:

```go
ownership, ok := idempotency.OwnershipFromContext(ctx)
```

Use `ownership.FencingToken` in the transaction or conditional write that
changes the business entity. For example, update only when the stored fence is
lower than the incoming fence, and store the incoming fence with the change.
When both records live in PostgreSQL, use `postgres.Store.CompleteTx` to commit
the business write or outbox record and idempotency completion atomically.

## Select the next recipe

- [HTTP](http.md)
- [JSON-RPC](json-rpc.md)
- [Webhook](webhooks.md)
- [Queue](queue.md)
- [Transactions and outbox](outbox.md)
- [Commands and imports](commands-and-imports.md)

Before production adoption, read the [state machine](state-machine.md), [crash
semantics](crash-semantics.md), and [operations guide](operations.md).
