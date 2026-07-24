# Releasing

## Preconditions

Release from a clean, synchronized `main` branch. Add one dated release
section to `CHANGELOG.md`, document compatibility or migration impact, and
review `NOTICE` plus `THIRD_PARTY_NOTICES.md` whenever XLS provenance or
dependencies change.

## Verification

```sh
make check
```

The release is blocked unless format checks, race tests, meaningful 100%
coverage, parser fuzz smoke, benchmarks, documentation links, lint, and
vulnerability scanning pass.

## Tagging

Use `make release-patch`, `make release-minor`, or
`make release-major`. The GitHub release workflow validates the tag,
repeats release gates, and publishes a deterministic source archive with a
checksum and changelog-derived notes.
