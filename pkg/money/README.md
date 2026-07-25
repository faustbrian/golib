# money

`money` is an immutable exact monetary value package. It delegates decimal,
integer, and rational arithmetic to `math` and delegates ISO 4217 identity
and metadata to `international/currency`.

The package never accepts or emits `float32` or `float64`. Fixed `Money`
operations preserve currency and resolved context identity. Multiplication and
division return `RationalMoney`; callers choose the context and rounding mode at
the boundary where a fixed amount is required.

## Install

```sh
go get github.com/faustbrian/golib/pkg/money
```

The monorepo checkout uses local replacements for the sibling `math` and
`international` modules.

## Quick start

```go
euro, _ := currency.Parse("EUR")
ctx, _ := money.DefaultContext(euro)
left, _ := money.Parse("12.30", euro, ctx)
right, _ := money.Parse("0.45", euro, ctx)
total, _ := left.Add(right) // 12.75 EUR
```

Exact multiplication remains rational until rounding is explicit:

```go
rate, _ := money.ParseRate("1/3")
exact, _ := total.Mul(context.Background(), rate)
fixed, result, _ := exact.Round(ctx, gomath.RoundHalfEven)
_ = result.Inexact()
```

## Guarantees

- Cross-currency or cross-context arithmetic returns an error.
- Default scales come from authoritative ISO metadata; historic currencies are
  accepted only through the explicit historic parse policy.
- Amount, scale, rate, ratio, allocation, output, and diagnostic work is
  bounded.
- Equal and weighted allocations distribute minor-unit remainders
  deterministically and conserve the source total.
- Tax and discount results derive one component by subtraction, so their
  documented totals are conserved.
- Conversion uses only a caller-supplied directed exact rate with timestamp and
  source metadata. There is no live FX client.
- Versioned JSON and SQL representations encode amounts as strings.

## Packages

- `money`: values, contexts, arithmetic, allocation, tax, discount, and FX.
- `money/format`: exact locale display using `international` locale tags.
- `money/encoding`: versioned JSON, text, SQL, and PostgreSQL numeric adapters.
- `money/moneytest`: official edge fixtures and conservation assertions.

## Verification

`make check` runs formatting, analysis, tests, meaningful 100% production
coverage, race checks, docs, compatibility, dependency, float-contamination,
and vulnerability gates. `make release-check` adds fuzzing, mutation testing,
and correctness-gated comparative benchmarks.

See [docs/api.md](docs/api.md),
[docs/contexts-and-rounding.md](docs/contexts-and-rounding.md), and
[docs/cookbook.md](docs/cookbook.md).
