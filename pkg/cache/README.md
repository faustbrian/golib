# cache

`cache` is a typed Go cache library with explicit hit, miss, stale, decode,
and backend-failure semantics. It provides bounded cache-aside loading,
versioned and hashed keys, strict codecs, a bounded memory backend, and native
Redis and Valkey adapters.

The semantic API does not expose Redis or Valkey client types. Backends keep
their native clients and atomic behavior while applications share one portable
contract.

## Install

```sh
go get github.com/faustbrian/golib/pkg/cache
```

Go 1.25 or newer is required.

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/backend/memory"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()
	backend, err := memory.New(memory.Config{
		MaxEntries: 10_000,
		MaxBytes:   64 << 20,
		Clock:      cache.SystemClock{},
	})
	if err != nil {
		panic(err)
	}

	keys, err := cache.NewKeySpace("accounts", "user", 1,
		cache.StringKeyEncoder{}, 128)
	if err != nil {
		panic(err)
	}
	users, err := cache.New(cache.Config[string, User]{
		Backend:  backend,
		Keys:     keys,
		Codec:    cache.JSONCodec[User]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: 5 * time.Minute},
		Clock:    cache.SystemClock{},
		MaxValue: 1 << 20,
		Load: cache.LoadPolicy{
			MaxConcurrent:    64,
			MaxWaitersPerKey: 256,
			NegativeTTL:      30 * time.Second,
		},
	})
	if err != nil {
		panic(err)
	}
	defer users.Close()

	result, err := users.GetOrLoad(ctx, "user-42",
		func(ctx context.Context, id string) (cache.LoadResult[User], error) {
			// Replace this with a context-aware source lookup.
			return cache.LoadResult[User]{Value: User{ID: id, Name: "Ada"}, Found: true}, nil
		})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.State, result.Value.Name)
}
```

Always inspect `Result.State`; a stored zero value can be a `Hit`. A miss is not
an error. Backend, decoding, schema, policy, limit, and loader failures remain
errors and can be classified with `errors.Is`.

## Choose a policy

- Use plain cache-aside for data where a source lookup is affordable.
- Add a short `NegativeTTL` only when source-level absence is authoritative.
- Use `StaleWhileRevalidate` when low latency is more important than returning
  the refresh error.
- Use `StaleIfError` when callers must see refresh failures but may also use the
  stale value. It returns both the stale `Result` and the error.
- Do not enable both stale policies; construction rejects ambiguous precedence.

See [policy decisions](docs/decisions.md) and
[failure modes](docs/failure-modes.md) before enabling stale behavior.

## Backends and observability

- [Bounded memory, Redis, and Valkey setup](docs/backends.md)
- [OpenTelemetry and slog](docs/observability.md)
- [Shared backend conformance suite](docs/api.md#backend-conformance)

## Documentation

- [API reference](docs/api.md)
- [Semantic matrices and hardening audit](docs/hardening.md)
- [Operations guide](docs/operations.md)
- [Key design and invalidation](docs/keys-and-invalidation.md)
- [Codecs and schema evolution](docs/codecs.md)
- [Stampede control and concurrency](docs/concurrency.md)
- [Adoption examples](docs/examples.md)
- [FAQ](docs/faq.md) and [troubleshooting](docs/troubleshooting.md)
- [Security](SECURITY.md), [compatibility](docs/compatibility.md),
  [migration](docs/migration.md), and [performance](docs/performance.md)

## Development

```sh
make check
make integration
make fuzz
make benchmark
```

`make check` enforces formatting, vet, lint, unit tests, meaningful exact
coverage, race safety, vulnerability scanning, GO-SAFETY-1, and docs
compilation. Integration tests use Testcontainers and require Docker.

## Status

The public API is being prepared for `v1.0.0`. Until that release, minor
versions may change APIs. See [CHANGELOG.md](CHANGELOG.md) and the
[compatibility policy](docs/compatibility.md).

## License

MIT. See [LICENSE](LICENSE).
