# Authorization and tenant constraints

Authorization changes capabilities before they enter a plan. The callback sees
only a declared capability kind and canonical public name. Return `false` for a
field, filter, sort, or relationship edge that the principal cannot use.
Nested includes authorize each prefix independently.

Do not encode tenant identity as a client filter. Supply it as a protected
mandatory constraint:

```go
options := apiquery.CompileOptions{
    Authorize: authorizeFor(principal),
    MandatoryConstraints: []apiquery.Constraint{{
        Name: "tenant_id",
        Value: apiquery.StringValue(principal.TenantID),
        Protected: true,
    }},
}
```

Persistence adapters must compose all mandatory constraints with `AND` before
client filters and must fail closed if any constraint mapping is absent. Never
simplify, deduplicate, or replace them with a client expression. Add other
server policy such as soft-delete, ownership, region, or snapshot boundaries in
the same list or in an application-owned SQL variant that cannot be disabled.

Authorization rejection deliberately does not say whether a hidden capability
exists. Avoid reflecting schema listings to principals that cannot inspect
them. Cache plans only inside the same schema revision, authorization identity,
tenant, and mandatory-policy context.
