# external-sort

`external-sort` performs bounded external sorting of fixed-size opaque records
while encrypting every temporary record with AES-256-GCM. It is intended for
large reconciliation and migration datasets that cannot safely be retained in
memory or written to plaintext temporary files.

## Quick start

```go
factory, err := externalsort.NewFactory(externalsort.Config{
    ParentDirectory: "/run/private/reconciliation",
    RecordBytes:     32,
    ChunkRecords:    100_000,
    MaximumRecords:  5_000_000,
})
if err != nil {
    return err
}

store, err := factory.Open(ctx, derivedAES256Key)
if store != nil {
    defer store.Close()
}
if err != nil {
    return err
}

if err := store.Add(ctx, digest[:]); err != nil {
    return err
}
return store.ForEachSorted(ctx, func(record []byte) error {
    return consume(record)
})
```

The parent directory must already exist, must not be a symlink, and must have
no group or other permission bits. Existing ancestor links are resolved when
the factory is created; each store binds the resolved directory to a rooted
handle and rejects later identity or permission changes. The key must contain
exactly 32 bytes.

## Guarantees

- fixed record size and explicit total-record limit;
- bounded contiguous in-memory chunks;
- at most 64 files in one merge;
- lexicographic byte ordering with duplicates preserved;
- a random-seeded process-unique nonce domain, a retry-safe record counter, and
  AES-256-GCM authentication for every temporary record;
- authentication of store identity, format version, chunk, ordinal, and record
  size;
- exact owner-only temporary modes (`0700` directories and `0600` files),
  independent of process umask;
- descriptor-relative storage and cleanup that cannot be redirected by a
  renamed or replaced parent pathname ancestor; and
- complete temporary-directory removal after a successful `Close`.

Stores permit one active lifecycle operation. Overlapping or reentrant calls
fail with `ErrConcurrentUse`; callers can retry after the active operation
returns. A record passed to the iteration callback is valid only until that
callback returns. Copy it when retention is required.

## Adoption and tradeoffs

Use this module when the data is fixed-width, sorting must be bounded, and
plaintext spill files are unacceptable. Prefer an in-memory sort for small,
public datasets. This implementation deliberately rejects configurations that
need more than 64 chunks instead of hiding an unbounded or multi-pass merge.
Increase the chunk size within the declared byte ceiling or partition the
dataset at a higher semantic layer.

`Close` removes process-owned artifacts, but no process can guarantee cleanup
after abrupt termination or host loss. Operators should place the parent on an
encrypted ephemeral filesystem and apply the descriptor-relative ownership and
age checks in the [operations guide](docs/operations.md) before removing stale
directories.

## Documentation

- [API and lifecycle](docs/api.md)
- [Architecture and file format](docs/architecture.md)
- [Adoption and migration](docs/adoption.md)
- [Compatibility](docs/compatibility.md)
- [Threat model](docs/threat-model.md)
- [Performance](docs/performance.md)
- [Operations and Kubernetes](docs/operations.md)
- [FAQ](docs/faq.md)
- [Security policy](SECURITY.md)
- [Release notes](CHANGELOG.md)

## License

MIT. See [LICENSE](LICENSE).
