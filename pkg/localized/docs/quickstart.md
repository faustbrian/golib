# Quickstart

## Construct and look up exactly

```go
value, err := localized.TextFromMap(map[string]string{
    "en-US": "Hello",
    "fi":    "",
})
if err != nil {
    return err
}

finnish, _ := locale.Parse("fi")
text, present := value.Get(finnish)
// text == "" and present == true: present-empty, not missing.
```

`TextFromMap` copies the map, validates UTF-8, canonicalizes tags, rejects
canonical duplicates, enforces default limits, and orders entries lexically.
Use `NewTextWithOptions` for duplicate or locale acceptance policies, or
`NewBuilder` for incremental typed construction. Use `TextFromPairs` when an
ordered boundary already supplies `localized.Pair` values.

## Match a preference

```go
canadianEnglish, _ := locale.Parse("en-CA")
result, err := localizedmatch.Best(value,
    localizedmatch.Preference{
        Locale: canadianEnglish,
        Weight: 1,
    },
)
```

Matching is explicit. `Get(en-CA)` remains missing even if `Best` selects an
English value.

## Configure fallback

```go
traditionalTaiwan, _ := locale.Parse("zh-Hant-TW")
plan, err := localizedmatch.NewPlan(
    []localizedmatch.Chain{{
        From: traditionalTaiwan,
        Candidates: []localizedmatch.Candidate{{
            Kind: localizedmatch.ParentRange,
            Locale: traditionalTaiwan,
        }},
    }},
    localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8},
)
result := plan.Resolve(value, traditionalTaiwan)
```

Parent traversal uses the locale dependency's explicit `FallbackParent`
behavior. Plans are copied, bounded, duplicate-checked, and cycle-checked
before use.

## Merge

```go
merged, err := base.MergeWithOptions(overlay, localized.MergeOptions{
    Conflicts: localized.RightWins,
    Empty:     localized.EmptyIsValue,
})
```

Choose `LeftWins`, `RightWins`, `RejectConflict`, or `ResolveConflict`. Resolver
callbacks receive a canonical tag and both strings; failure returns no partial
result.

## JSON

```go
encoded, err := localized.EncodeJSON(value)
decoded, err := localized.DecodeJSON(encoded, localized.DecodeOptions{})
```

Canonical JSON is an object ordered by canonical tags. `{}` is the zero value;
strict mode rejects `null`, invalid UTF-8, duplicate canonical tags, non-string
values, trailing input, and oversize input. Entry-array encoding is available
from `encoding.MarshalEntries`.

## PostgreSQL

With `database/sql`, use the nullable wrapper:

```go
argument := postgres.NewText(value)
_, err := db.ExecContext(ctx,
    `UPDATE products SET name = $1 WHERE id = $2`, argument, id,
)

var scanned postgres.Text
err = db.QueryRowContext(ctx,
    `SELECT name FROM products WHERE id = $1`, id,
).Scan(&scanned)
```

With pgx, register `postgres.JSONBCodec()` for JSONB OID on the connection type
map. See [migration](migration.md) before changing existing columns.
