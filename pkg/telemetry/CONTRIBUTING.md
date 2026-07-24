# Contributing

## Development environment

Use a supported Go version and install `golangci-lint` v2.12 and
`govulncheck`. No Collector is required for the normal suite; protocol tests
run in-process.

```sh
go mod download
make check
make race
make fuzz
make benchmark
```

`make check` verifies formatting, vetting, unit and protocol integration tests,
meaningful 100% library coverage, safety constraints, and example builds.

## Changes

- Add behavior-focused tests before implementation changes.
- Keep configuration explicit and return standard OpenTelemetry APIs.
- Do not add vendor SDKs or direct-to-vendor defaults.
- Do not record secrets, payloads, raw identifiers, or unbounded labels.
- Add every user-visible change to `CHANGELOG.md`.
- Update compatibility and upgrade documentation when dependencies or public
  contracts change.

Benchmarks that materially change should include before/after `-benchmem`
output in the pull request. New integrations belong in isolated
`instrumentation/*` packages and must not introduce dependency cycles.

## Pull requests

Explain the operational reason for the change, failure behavior, cardinality
and privacy effects, compatibility impact, and the exact checks run. CI must be
green before release.
