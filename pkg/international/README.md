# international

Typed, immutable international identifiers and metadata for Go services.
Countries, subdivisions, languages, locales, currencies, phone numbers, and
postal values remain distinct types with strict parsing, explicit
canonicalization, offline behavior, and versioned dataset provenance.

```go
finland, err := country.Parse("FI")
if err != nil { return err }

tag, err := locale.Parse("fi-FI")
if err != nil { return err }

number, err := phone.Parse("040 123 4567", phone.ParseOptions{
    RegionHint: finland,
})
```

The zero value of every scalar means absent. Text encoding rejects absent
values; JSON and SQL encode them as `null`/`NULL`. Parsing never performs
country inference, locale detection, delivery validation, identity claims, or
runtime network access.

Start with the [five-minute quickstarts](docs/quickstart.md), then read the
[API and standards reference](docs/reference.md), [integration guide](docs/integrations.md),
and [security model](SECURITY.md). Dataset versions and licenses are documented
in [provenance](docs/provenance.md); the checked semantic baseline and update
classification procedure are in the [dataset report](docs/dataset-report.md).
The requirement-to-test mapping, resource budgets, and local gate evidence are
in the [verification report](docs/verification.md).

Requires Go 1.26.5 or newer. Licensed under MIT; dataset licenses remain with
their upstream publishers.
