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
        Clock:       time.Now,
        Resolver:    resolver,
        LocaleAnalyzers: map[string]string{
            "fi": "finnish",
            "en": "english",
        },
    },
    Lifecycle: &opensearch.LifecycleConfig{Authorizer: lifecycleAuthorizer},
})
if err != nil {
    return err
}
defer func() { _ = client.Close() }()
```

The resolver authorizes tenant/logical-index access and returns a safe physical
index plus mapping fingerprint. Lifecycle calls use a separate authorizer.
Authentication options are mutually exclusive, credentials are resolved for
each request, implicit retries are disabled, response bodies are bounded, and
borrowed transports remain caller-owned.

## Semantics

- Cursor searches use point-in-time plus `search_after`; PIT cleanup failures
  are observable.
- Writes use external version semantics and preserve partial or unknown bulk
  outcomes.
- Typed queries, filters, sorts, projection, aggregations, highlights,
  suggestions, geo values, and approved locale analyzers are encoded directly.
- Trusted callers may use a bounded `RawExtensionQuery` explicitly bound to
  `opensearch`; unrestricted caller-supplied DSL remains outside the contract.
- Unsupported or unsafe combinations fail before network execution.
- Index create, resumable reindex, verification, alias cutover, rollback seam,
  and cleanup are separately authorized.

See the [documentation index](docs/README.md), including deployment, AWS,
security, pagination, migration/rebuild, observability, upgrades, backups, and
compatibility.

## Integration tests

Real-backend tests require an explicitly supplied disposable OpenSearch URL and
never use a running production service. See `make integration`.

## License

MIT. See [LICENSE](LICENSE).
