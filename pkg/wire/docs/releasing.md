# Releasing

## Preconditions

A release must originate from a clean, synchronized `main` branch. Update
`CHANGELOG.md` with one dated version section, confirm compatibility and
migration notes, and verify dependency/provenance changes.

## Verification

```sh
make check
```

Format-specific fuzz targets, 100% meaningful production coverage, API
inventory checks, documentation links, and vulnerability scanning are release
gates.

## Tagging

Use `make release-patch`, `make release-minor`, or
`make release-major`. Review the local annotated tag before pushing it.
The release workflow validates the tag, rebuilds all gates, creates a
deterministic source archive and checksum, and publishes changelog-derived
notes.
