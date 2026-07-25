# Testing and release gates

`make check` runs formatting, documentation validation, `go vet`, tests,
meaningful 100% statement coverage, race tests, strict linting, static analysis,
embedded-list integrity, examples, dependency verification, vulnerability
scanning, license review, and workflow validation.

`make release-check` adds fuzz smoke tests, mutation testing, and benchmark
execution. The repository-root `keyphrase` workflow runs that target from
the module directory for changes, merge queues, dispatches, and a weekly
schedule. The module-local workflows preserve the same gates if the module is
split into its own repository. CI-only test logic is avoided.

`make stable-release-check` first verifies the independent-review record, then
runs `make release-check`. Tag builds use this stricter target. It intentionally
fails while [the review record](security-review.md) is pending; ordinary CI can
continue proving the implementation without weakening the stable-release
blocker.

Official vectors cover every official language and entropy size. The
interoperability tests compare every English entropy size, parsing, checksum
rejection, normalized Japanese seed input, and PBKDF2 output with an independent
mature Go implementation. Property tests cover policy satisfaction and round
trips.
Statistical checks use documented false-positive thresholds only to catch
obvious selection regressions. Race tests share immutable lists and default
generators. Fuzz targets cover alphabets, lists, mnemonic normalization and
malformed input, and passphrase parsing.

Mutation configuration targets checksum, policy, normalization, selection, and
error paths. Surviving relevant mutations block release; equivalent or
unreachable mutations require a reviewed exclusion with rationale.
