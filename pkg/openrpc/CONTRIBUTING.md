# Contributing

## Development setup

Install the Go version in `.go-version`, clone the repository, and run:

```sh
make test
make race
make conformance
make fuzz FUZZ_TIME=1s
make benchmark BENCH_TIME=1x
```

`make check-all` is the release gate. It intentionally fails until every
required quality target, including meaningful 100% coverage and mutation
testing, is satisfied.

## Changes

- Preserve exact OpenRPC and Draft 7 semantics; do not narrow arbitrary JSON.
- Add a failing behavioral test before changing production behavior.
- Keep parsing, validation, and resolution resource-bounded.
- Do not add implicit network, filesystem, telemetry, or global registry use.
- Update conformance evidence when a normative behavior changes.
- Run `go mod tidy -diff`; dependency changes require provenance and license
  review.

Use focused conventional commits with a body explaining why. Pull requests
should state behavior, security impact, compatibility impact, and exact
verification commands.
