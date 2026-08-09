# Contributing

Open an issue before changing a public contract. Keep core backend-neutral and
put engine integrations in independent adapter modules. Behavioral changes need
red-green tests, exact statement coverage, viable mutation kills, race proof,
bounded hostile-input tests, documentation, and an Unreleased changelog entry.

Run `make check` with a disposable `GOCACHE`. Real OpenSearch tests must target
an explicitly supplied disposable cluster.
