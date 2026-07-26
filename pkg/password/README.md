# password

`password` is a narrowly scoped password hashing, verification, parsing,
and login-time upgrade library. It uses maintained Go implementations of
Argon2id and bcrypt. It does not own users, repositories, registration, login
endpoints, sessions, password reset, MFA, authorization, or reversible secrets.

## Requirements

- Go 1.26.5 or newer.
- `golang.org/x/crypto` v0.54.0.

## Install

```sh
go get github.com/faustbrian/golib/pkg/password
```

## Five-minute Argon2id quickstart

```go
package main

import (
	"context"
	"errors"
	"fmt"

	password "github.com/faustbrian/golib/pkg/password"
)

func main() {
	passwords, err := password.New(password.DefaultPolicy())
	if err != nil {
		panic(err)
	}

	encoded, err := passwords.Hash(
		context.Background(),
		[]byte("caller-owned password"),
	)
	if err != nil {
		panic(err)
	}

	result, err := passwords.Verify(
		context.Background(),
		[]byte("caller-owned password"),
		encoded.String(), // explicit persistence access
	)
	if errors.Is(err, password.ErrMismatch) {
		fmt.Println("rejected")
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Match(), result.NeedsRehash())
}
```

`DefaultPolicy` uses Argon2id version 19, time 2, 64 MiB memory, one lane, a
16-byte salt, and a 32-byte output. On an Apple M4 Max the measured one-shot
baseline is approximately 66 ms and 64 MiB per hash or verification. Benchmark
on deployment hardware before setting concurrency or pod limits.

## Laravel migration

Use `VerifyAndUpgrade` or `passwordauth.Authenticator` during successful login.
Laravel `$2y$` bcrypt and PHC Argon2id strings are accepted. Never replace the
database value until verification succeeds and the new hash is durably written
with an optimistic comparison against the old value.

```sql
UPDATE users
SET password_hash = $1
WHERE id = $2
  AND password_hash = $3;
```

Treat one affected row as success and zero as a benign concurrent update. See
the [Laravel migration guide](docs/laravel-migration.md) for complete Go and
PostgreSQL examples.

## Security defaults

- Parser and primitive resources are bounded before expensive work.
- Active and queued work have explicit hard limits and drainable lifecycle.
- Argon2id verification uses constant-time derived-key comparison.
- Bcrypt verification delegates to the maintained bcrypt primitive.
- Rehash decisions never downgrade stronger parameters or Argon2id to bcrypt.
- Production constructors use `crypto/rand.Reader`; deterministic entropy is
  visibly test-only.
- Password slices are copied and not retained. Best-effort clearing of the copy
  is not a guarantee of runtime memory erasure.
- `EncodedHash.String()` is explicit persistence access; all `fmt` formatting
  is redacted.
- Observations contain bounded enums, upgrade state, and duration only.

## Packages

| Package | Purpose |
| --- | --- |
| root | Policy, limits, parsing, admission, service, errors, observations |
| `argon2id` | Argon2id service constructors |
| `bcrypt` | Bcrypt compatibility constructors |
| `passwordauth` | Application lookup and explicit CAS upgrade adapter |
| `passwordservice` | Admission lifecycle hooks |
| `passwordtest` | Synthetic fixtures and deterministic test entropy |

## Documentation

- [API and error semantics](docs/api.md)
- [Encoded-hash grammar](docs/parser-grammar.md)
- [Laravel migration](docs/laravel-migration.md)
- [Database upgrades and concurrency](docs/database-upgrades.md)
- [Kubernetes sizing and performance](docs/kubernetes-sizing.md)
- [Threat model](docs/threat-model.md)
- [Security and secret handling](docs/secret-handling.md)
- [Testing and release gates](docs/testing.md)
- [Release evidence and external gates](docs/release-evidence.md)
- [Compatibility matrix](docs/compatibility.md)
- [Vector and fixture provenance](docs/vector-provenance.md)
- [FAQ](docs/faq.md) and [troubleshooting](docs/troubleshooting.md)

## License

MIT
