# api-query

`api-query` compiles explicitly declared API query capabilities into an
immutable, storage-neutral plan. It covers field projection, relationships,
typed filters, deterministic sorting, cursor or offset pagination, conservative
costs, and strict transport adapters without becoming an ORM or SQL language.

The minimum supported toolchain is Go 1.26.5.

## Five-minute JSON-RPC quickstart

```go
schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
    Resource: "orders",
    Revision: "orders-v1",
    Fields: []apiquery.FieldDefinition{
        {Name: "id", Type: apiquery.TypeString, Required: true},
        {Name: "status", Type: apiquery.TypeString, Default: true},
    },
    Filters: []apiquery.FilterDefinition{{
        Name: "status", Type: apiquery.TypeString,
        Operators: []apiquery.Operator{apiquery.OpEqual, apiquery.OpIn},
    }},
    Sorts: []apiquery.SortDefinition{{
        Name: "id", Type: apiquery.TypeString, TieBreaker: true,
    }},
    Pagination: apiquery.PaginationDefinition{
        Cursor: true, DefaultPageSize: 25,
    },
    Bounds: apiquery.Bounds{
        MaxFields: 10, MaxFilterDepth: 3, MaxFilterNodes: 20,
        MaxValues: 40, MaxSorts: 3, MaxPageSize: 100,
        MaxCursorBytes: 2048, MaxCost: 100,
    },
})
if err != nil {
    return err // invalid server declaration
}

params, err := apiqueryrpc.Parse(rawParams, schema.Bounds().MaxRequestBytes)
if err != nil {
    return err // sanitized transport error
}

plan, err := apiquery.Compile(ctx, schema, params.Request(), apiquery.CompileOptions{
    Authorize: func(ctx context.Context, capability apiquery.Capability) bool {
        return policy.Allows(ctx, capability.Kind, capability.Name)
    },
    MandatoryConstraints: []apiquery.Constraint{{
        Name: "tenant_id", Value: apiquery.StringValue(tenantID), Protected: true,
    }},
    CursorDecoder: cursorCodec,
})
if err != nil {
    return apiqueryvalidation.Report(err, validation.DefaultLimits())
}

// Only a reviewed plan reaches an application-owned persistence adapter.
parts, err := pgCompiler.Compile(plan)
```

An explicitly empty JSON-RPC array such as `"fields": []` remains different
from an absent `fields` member. JSON objects reject unknown or duplicate members
and parsing is byte-bounded before compilation.

## Contract boundary

Servers own every field, filter, operator, relationship, sort, bound, cost,
cursor version, authorization decision, and mandatory predicate. Clients never
supply identifiers, SQL, regular expressions, functions, joins, repositories,
or execution behavior. The core package imports no database or transport.

Start with the [complete API reference](docs/api.md),
[security model](SECURITY.md), and [cursor guide](docs/cursor.md). Transport and
adoption guides cover [HTTP](docs/http.md), [OpenRPC](docs/openrpc.md),
[JSON:API composition](docs/jsonapi.md), [SQLC](docs/sqlc.md), and
[Laravel/Cline RPC migration](docs/migration-laravel-cline.md).

## Local quality gates

```sh
make check       # formatting, analysis, tests, coverage, race, fuzz, docs, API
make mutation    # 100% mutation efficacy and mutant coverage
make postgres    # disposable PostgreSQL integration
make benchmarks  # allocation-reporting performance baselines
make nilaway     # visible advisory diagnostics, intentionally non-blocking
make ci          # complete local release-equivalent gate stack
```

All tools are pinned in `go.mod`. PostgreSQL defaults to a digest-pinned
disposable container; set `APIQUERY_TEST_DATABASE_URL` to use an existing test
database. No package executes queries or contacts a service at runtime.

## Stability

The repository is preparing v1. Public compatibility rules are in
[docs/compatibility.md](docs/compatibility.md), current changes are in
[CHANGELOG.md](CHANGELOG.md), and the exported v1 candidate is recorded in
`api/v1.txt`.
