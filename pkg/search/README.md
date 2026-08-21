# search

`search` provides backend-neutral contracts for bounded document indexing,
typed querying, cursor pagination, schema migration, and reconciliation. Search
indexes are rebuildable derived state; an application datastore remains the
source of truth.

The core module has no search-engine dependency. The first production adapter
is [`adapters/opensearch`](adapters/opensearch/README.md). Backend capabilities
are explicit: unsupported typed features fail validation instead of silently
degrading to a query string.

## Quick start

```go
request := search.Request{
    Tenant: "tenant-a",
    Index:  "locations",
    Query: search.BoolQuery{Filter: []search.Query{
        search.TermQuery{Field: "country", Value: search.StringValue("FI")},
        search.ExistsQuery{Field: "coordinates"},
    }},
    Sort: []search.Sort{
        {Field: "updated_at", Direction: search.Descending},
        {Field: search.DocumentIDSortField, Direction: search.Ascending},
    },
    Page: search.CursorPage{Size: 50, KeepAlive: time.Minute},
}
capabilities, err := client.Capabilities(ctx)
if err != nil {
    return err
}
if err := request.Validate(capabilities, search.DefaultLimits()); err != nil {
    return err
}
result, err := client.Search(ctx, request)
```

Every document has a stable ID and positive external version. Every bulk item
retains its own applied, rejected, conflict, or unknown outcome. Returned
documents, fields, diagnostics, aggregations, and suggestions are owned copies.

## Adoption

- Use PostgreSQL full-text search when the relational source of truth, simple
  ranking, and transactional consistency are more important than independent
  search scaling or engine analyzers.
- Use this module when Track or Location needs typed full-text, geo, facets,
  highlighting, suggestions, cursor consistency, or independently rebuilt
  indexes.
- Keep raw backend extensions behind trusted application policy. Never accept
  unrestricted raw DSL from an untrusted caller.

See the [documentation index](docs/README.md), [API guide](docs/api.md),
[operations guide](docs/operations.md), and [FAQ](docs/faq.md).

## Development

```sh
make check
```

Callers should set `GOCACHE` to a disposable directory for local and CI runs.
The module gate enforces exact statement coverage and supports race, fuzz,
mutation, benchmark, API, and clean-consumer checks.

## License

MIT. See [LICENSE](LICENSE).

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
