# Testing and release gates

The root and optional packages maintain meaningful 100% production statement
coverage. Coverage is a floor; vectors, boundary assertions, fuzzing, race
tests, mutation checks, and integration fixtures prove behavior.

## Local commands

```sh
make format
make vet
make boundaries
make portability
make test
make race
make resource
make coverage
make fuzz
make bench
make kubernetes-bench
make lint
make staticcheck
make nilaway       # advisory, visible, non-blocking findings
make vuln
make mutation
make docs
make api
make check
make release-artifact VERSION=v1.0.0 REF=HEAD
make release-check VERSION=v1.0.0 REF=HEAD
```

Fuzz targets cover parser grammar and bounded verification. `FUZZ_TIME` controls
each campaign; CI uses ten seconds, while longer local/security runs should use
minutes or hours. Seed corpora include valid PHP hashes, truncation, duplicate
fields, overflow, invalid base64, unsupported versions, and parameter bombs.

Race tests share immutable policies, deterministic entropy, admission, hashing,
verification, queue wakeup, and shutdown across goroutines.

`make kubernetes-bench` verifies its 2-CPU/512 MiB cgroup before measuring all
approved policies in pinned Go Linux. The build-tagged `make resource` gate
runs approved 64 MiB Argon2id work under the race detector and asserts the
configured active-memory ceiling. Go runtime
out-of-memory termination cannot be injected or recovered as an ordinary
allocation error; policy/encoding boundary tests and pre-primitive rejection
are the executable allocation-failure boundary.

Mutation scope targets match/mismatch, canonical parser boundaries, parameter
ceilings, admission decisions, and rehash downgrade prevention. Timeouts are
bounded and reported; survivors require review, not automatic acceptance.

`passwordtest` credentials are synthetic. Never add real hashes, usernames,
tokens, database dumps, or incident artifacts to fixtures.
See [vector and fixture provenance](vector-provenance.md) for exact independent
producers and maintenance requirements.
