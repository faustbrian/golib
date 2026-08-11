# OpenSearch adapter

This module is the first production adapter for
`github.com/faustbrian/golib/pkg/search`. It translates typed requests to the
OpenSearch API without making engine-specific ranking behavior part of the core
contract.

## Configure

```go
client, err := opensearch.New(opensearch.Config{
    Endpoints:            []string{"https://search.example.internal:9200"},
    RequestTimeout:       3 * time.Second,
    MaximumResponseBytes: 8 << 20,
    Signer:               signer,
    Search: &opensearch.SearchConfig{
        Limits:      search.DefaultLimits(),
        CursorCodec: cursorCodec,
        Resolver:    resolver,
		Authorizer: searchAuthorizer,
		WriteGuard: writeGuard,
		MaximumOpenPointInTimes: 64,
        LocaleAnalyzers: map[string]string{
            "fi": "finnish",
            "en": "english",
        },
    },
	Lifecycle: &opensearch.LifecycleConfig{
		Authorizer:         lifecycleAuthorizer,
		Verifier:           lifecycleVerifier,
		CutoverGuard:       applicationWriteFence,
		MutationGuard:      durableLifecycleMutationCoordinator,
		CleanupGuard:       durableCleanupEligibilityGuard,
		ReindexCursorCodec: reindexCursorCodec,
	},
})
if err != nil {
    return err
}
defer func() { _ = client.Close() }()
```

The search authorizer approves the complete logical query, disclosure scope,
and bounded pagination cost intent before the resolver maps
tenant/logical-index access to a safe physical index plus mapping fingerprint.
Opaque cursor bytes are not disclosed to policy. Lifecycle calls use a separate
authorizer, semantic verifier, application-owned write fence, durable
cross-instance mutation coordinator, and encrypted task-cursor codec.
Irreversible deletion additionally requires a durable cleanup-eligibility guard
spanning final generation checks and backend delete while the same mutation
coordinator excludes competing create and alias operations.
`WriteGuard` separately approves every fully cloned write or bulk unit before
physical index resolution. Omitting it intentionally constructs a read-only
client: `Write` and `Bulk` fail with `ErrWriteDisabled` before network I/O.
The guard must consult application-owned durable current documents or
tombstones: OpenSearch delete-version tombstones expire after `index.gc_deletes`
and cannot by themselves prevent an older replay from resurrecting a deleted
projection. A target resolver returns the request alias as `IndexTarget.Name`
and the exact backing generation expected in response metadata as
`IndexTarget.PhysicalName`; update that physical generation atomically inside
the write-fenced alias cutover before writers resume.
Authentication options are mutually exclusive, credentials are resolved for
each request, implicit retries are disabled, response bodies are bounded, and
borrowed transports remain caller-owned.

## Semantics

- Cursor searches use point-in-time plus `search_after`; PIT cleanup failures
  are observable, and continuation keep-alive is capped by the signed cursor's
  remaining absolute lifetime. Continuations are single-consumer per client;
  the per-client process-local PIT budget is configured explicitly.
- Writes use external version semantics and preserve partial or unknown bulk
  outcomes.
- Typed queries, filters, sorts, projection, aggregations, highlights,
  suggestions, geo values, and approved locale analyzers are encoded directly.
- Trusted callers may use a bounded `RawExtensionQuery` explicitly bound to
  `opensearch`; unrestricted caller-supplied DSL remains outside the contract.
- Unsupported or unsafe combinations fail before network execution.
- Index create, resumable reindex, verification, fenced alias cutover, rollback
  seam, and cleanup are separately authorized. `CutoverAlias` retains the
  application write fence across final verification and alias mutation.

See the [documentation index](docs/README.md), including deployment, AWS,
security, pagination, migration/rebuild, observability, upgrades, backups, and
compatibility. Observable REST and adapter policy choices are recorded in the
[specification decision register](docs/specification-decisions.md).

## Integration tests

Real-backend tests require an explicitly supplied disposable OpenSearch URL and
never use a running production service. See `make integration`.

## License

MIT. See [LICENSE](LICENSE).
