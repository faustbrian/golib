# localized

[![CI](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/ci.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/ci.yml)
[![PostgreSQL](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/postgres.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/postgres.yml)
[![Mutation](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/mutation.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/mutation.yml)
[![Fuzz](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/fuzz.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/fuzz.yml)
[![Security](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/security.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/security.yml)
[![Benchmarks](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/benchmark.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/benchmark.yml)
[![Release](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/release.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/localized/actions/workflows/release.yml)

`localized` provides immutable UTF-8 text keyed by canonical BCP 47 language
tags. Exact lookup, language matching, and application fallback are separate
operations. The package does not provide catalogs, formatting, pluralization,
translation loading, language detection, or global locale policy.

## Install

```sh
go get github.com/faustbrian/golib/pkg/localized
```

Go 1.26.5 or later is required.

## Five-minute tour

```go
englishTag, err := locale.Parse("en")
if err != nil {
    return err
}
finnishTag, _ := locale.Parse("fi")
canadianEnglish, _ := locale.Parse("en-CA")
swedishTag, _ := locale.Parse("sv")

text, err := localized.NewText(
    localized.Entry{Locale: englishTag, Text: "Hello"},
    localized.Entry{Locale: finnishTag, Text: "Hei"},
)
if err != nil {
    return err
}

english, present := text.Get(englishTag) // exact only
_ = english
_ = present

matched, err := localizedmatch.Best(text,
    localizedmatch.Preference{Locale: canadianEnglish, Weight: 1},
)
if err != nil {
    return err
}

plan, err := localizedmatch.NewFallbackPlan(
    []locale.Tag{swedishTag, englishTag}, nil, 4,
)
if err != nil {
    return err
}
fallback := plan.Resolve(text)

overlay, _ := localized.TextFromMap(map[string]string{"en": "Hi"})
merged, err := text.Merge(overlay, localized.RightWins)
if err != nil {
    return err
}

canonicalJSON, err := localized.EncodeJSON(merged)
_ = matched
_ = fallback
_ = canonicalJSON
```

For SQL and pgx, use `postgres.NewText(value)` and
`postgres.JSONBCodec()`. See the [quickstart](docs/quickstart.md) for complete
construction, fallback, merge, JSON, and PostgreSQL examples.

## Guarantees

- caller maps, entry slices, rows, iterators, and encoded bytes do not alias
  retained state;
- locale keys use canonical `international/locale.Tag` identity;
- missing and present-empty are distinguished by every lookup result;
- iteration and canonical encoding are lexically deterministic;
- fallback never inserts an invented translation;
- parser, locale, text, matching, fallback, merge, and telemetry work is
  bounded;
- production code has no mutable globals, unsafe, cgo, `go:linkname`, cache,
  goroutine, registry refresh, or process-global locale;
- package-generated errors and events never include localized content.

## Documentation

Start at the [documentation index](docs/README.md). The normative behavior is
in [semantics](docs/semantics.md), the complete public surface in the
[API reference](docs/api.md), and operational constraints in
[security](docs/security.md) and [performance](docs/performance.md).

## Development

`make check` runs the complete local gate stack. Hosted workflows mirror these
commands, but local development does not depend on a remote branch or CI run.
PostgreSQL integration uses an explicitly supplied disposable database:

```sh
make postgres \
  POSTGRES_URL='postgres://postgres:postgres@127.0.0.1:5432/localized?sslmode=disable'
```

With Docker and `pg_isready`, `make postgres-matrix` creates isolated ephemeral
containers and verifies PostgreSQL 14 through 18 without using ambient
services.

## License

MIT. See [LICENSE](LICENSE), [NOTICE](NOTICE), and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
