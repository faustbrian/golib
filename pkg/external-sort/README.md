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
if err != nil {
    return err
}
defer store.Close()

if err := store.Add(ctx, digest[:]); err != nil {
    return err
}
return store.ForEachSorted(ctx, func(record []byte) error {
    return consume(record)
})
```

The parent directory must already exist, must not be a symlink, and must have
no group or other permission bits. The key must contain exactly 32 bytes.

## Guarantees

- fixed record size and explicit total-record limit;
- bounded contiguous in-memory chunks;
- at most 64 files in one merge;
- lexicographic byte ordering with duplicates preserved;
- a fresh random nonce and AES-256-GCM authentication per temporary record;
- authentication of format version, chunk, ordinal, and record size;
- owner-only temporary directories and files; and
- complete temporary-directory removal after a successful `Close`.

Stores have one owner and are not safe for concurrent use. A record passed to
the iteration callback is valid only until the callback returns. Copy it when
retention is required.

## Adoption and tradeoffs

Use this module when the data is fixed-width, sorting must be bounded, and
plaintext spill files are unacceptable. Prefer an in-memory sort for small,
public datasets. This implementation deliberately rejects configurations that
need more than 64 chunks instead of hiding an unbounded or multi-pass merge.
Increase the chunk size within the declared byte ceiling or partition the
dataset at a higher semantic layer.

`Close` removes process-owned artifacts, but no process can guarantee cleanup
after abrupt termination or host loss. Operators should place the parent on an
encrypted ephemeral filesystem and apply a conservative stale-directory
cleanup policy when crash recovery requires it.

## Documentation

- [API and lifecycle](docs/api.md)
- [Architecture and file format](docs/architecture.md)
- [Adoption and migration](docs/adoption.md)
- [Compatibility](docs/compatibility.md)
- [Threat model](docs/threat-model.md)
- [Performance](docs/performance.md)
- [FAQ](docs/faq.md)
- [Security policy](SECURITY.md)
- [Release notes](CHANGELOG.md)

## License

MIT. See [LICENSE](LICENSE).
