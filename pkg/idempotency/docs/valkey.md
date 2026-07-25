# Valkey adapter

The `valkey` package implements `idempotency.Store` with native
`github.com/valkey-io/valkey-go` clients. Every transition runs in one Lua
script and uses Valkey `TIME`, so process clock skew does not authorize a stale
owner.

## Five-minute setup

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	idempotencyvalkey "github.com/faustbrian/golib/pkg/idempotency/valkey"
	valkeygo "github.com/valkey-io/valkey-go"
)

func newService() (*idempotency.Service, func(), error) {
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress: []string{"127.0.0.1:6379"},
	})
	if err != nil {
		return nil, nil, err
	}
	tokens := func() (string, error) {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return "", err
		}
		return hex.EncodeToString(value[:]), nil
	}
	store, err := idempotencyvalkey.Open(
		context.Background(),
		client,
		idempotencyvalkey.Options{
			Prefix:      "my-service-idempotency",
			Retention:   7 * 24 * time.Hour,
			OwnerTokens: tokens,
		},
	)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return service, client.Close, nil
}
```

`Open` verifies that the server is Valkey 9 or newer and that
`maxmemory-policy` is `noeviction`. The Valkey user therefore needs permission
to run `INFO server` and `CONFIG GET maxmemory-policy` during startup. Treat a
failed check as a readiness failure: serving traffic without it can turn an
evicted record into an apparent first attempt.

Owner tokens must be unpredictable and unique. Do not use a process-local
counter outside deterministic tests.

## Key and cluster design

The physical key is `<prefix>:{<sha256>}`. The digest covers length-delimited
namespace, tenant, operation, caller, and key value fields. Raw identities do
not appear in the physical key. The braces form one Valkey Cluster hash tag.

Each operation supplies exactly one key to one script, so the adapter is
cluster-safe for its claimed operations. It does not perform scans or
multi-record transactions. Prefixes containing braces are rejected because an
earlier brace pair could change slot selection.

The hash value contains the full logical key because persisted records must be
reconstructable and conflicts must remain diagnosable through the semantic API.
Protect Valkey access and backups accordingly; opaque physical keys are not
encryption.

## TTL and retention

Active records receive a TTL equal to `lease + retention`. A heartbeat resets
that TTL using its new lease. Completed, failed, abandoned, and explicitly
expired records receive exactly the configured retention TTL.

Retention must cover all of the following:

- the maximum client retry and queue-redelivery window;
- incident recovery and reconciliation time;
- rolling-deployment compatibility windows;
- the period during which a business system may still observe an old fence;
- operational clock and scheduling delays outside Valkey.

When a TTL deletes a record, a later request starts a new record with attempt
and fencing token `1`. Therefore a physical record lifetime is one fencing
domain. Applications must not reuse the same logical key after retention when a
business entity still remembers a fence from the previous domain. Use a new key
generation or retain records for the full business lifetime when numeric fence
comparison crosses that boundary.

## Eviction policy

Use a dedicated Valkey deployment or database with `maxmemory-policy
noeviction`. An eviction policy can remove an unexpired ownership or replay
record. The next acquisition then sees a missing key and may authorize a
duplicate handler execution; the adapter cannot distinguish eviction from a key
that never existed.

Capacity alerts must fire before memory exhaustion. Estimate record count from
peak unique keys over the retention window, then include logical key fields,
results, metadata, hash overhead, allocator fragmentation, replication, and
persistence buffers.

## Failure behavior

Network, timeout, authentication, script, and client-closed errors are returned
unchanged by the adapter. `idempotency.Service` converts non-semantic backend
errors into `unavailable` and fails closed unless a caller explicitly selects
`AvailabilityAllowUntracked` for a duplicate-tolerant operation.

The adapter uses `EVALSHA` with the `valkey-go` fallback to `EVAL` after
`NOSCRIPT`. Script loading and each transition remain atomic at the server. A
lost connection leaves the result unknown: inspect the record after reconnect;
never assume the script did or did not execute from the transport error alone.

Valkey replication durability is an infrastructure policy, not a consequence
of script atomicity. The failure suite waits until a Valkey 9 replica has the
ownership record, kills the primary, promotes the replica, verifies the same
owner and fence, and completes there. Without an acknowledgement or persistence
policy that meets the deployment's loss objective, an acknowledged write can
still be absent after failover.

## Compatibility

Persisted hashes carry schema version `1`. Unknown schema versions, malformed
counters, timestamps, fingerprints, metadata, states, and oversized results
fail closed as `invalid_payload`. Rolling releases must retain decoding support
for every schema that may remain within the retention window.

The integration suite requires Valkey 9 and runs the same conformance contract
against both standalone and three-primary cluster deployments. It also verifies
active and terminal TTLs, binary result replay, closed-client failure behavior,
unknown results after the server executes a script but its reply is lost, and
replica-promotion recovery of synchronized ownership.
