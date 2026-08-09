# Exact-money knapsack objective

`gomoney` is an optional adapter that lets the Knapsack solvers minimize exact
container costs. It depends on the owned Money module so the Knapsack core can
remain independent of monetary policy.

## Quick start

```go
euro, _ := currency.Parse("EUR")
moneyContext, _ := money.DefaultContext(euro)
small, _ := money.Parse("0.60", euro, moneyContext)
large, _ := money.Parse("1.50", euro, moneyContext)

costs, err := gomoney.New(map[string]money.Money{
    "small": small,
    "large": large,
})
if err != nil {
    // Configuration is invalid.
}

plan, err := (solver.Exact{}).PackAll(ctx, request, solver.Options{
    PlanObjective: costs,
})
```

The compiled examples contain complete cost construction and comparison code.

## API

- `New` copies and validates a Go map with `DefaultPolicy`.
- `NewWithLimits` applies caller-selected collection limits while retaining the
  default negative-cost policy.
- `NewWithPolicy` accepts a map and an explicit `Policy`.
- `NewFromEntries` accepts an entry sequence when duplicate IDs must be detected
  before a Go map would collapse them.
- `Total`, `Compare`, `ComparePlans`, and `Components` expose exact totals,
  deterministic ordering, and the Knapsack `objective.PlanObjective` contract.

All constructors copy input collections. `Costs` is immutable after successful
construction and is safe to share across solver instances.

## Currency and scale policy

Every configured cost must use the same currency and the identical resolved
Money context. The adapter supports `money.ContextDefault` and
`money.ContextCustom`, whose scales are fixed and bounded by the Money module.
Cash contexts and automatic contexts are rejected because aggregation would
otherwise carry rounding-step policy or input-dependent scale. The adapter
never rescales, rounds, converts currency, or accesses exchange rates.

Zero is accepted. Negative values are rejected by default. Set
`Policy.AllowNegativeCosts` only when a negative container value deliberately
models a credit or rebate; this changes optimization behavior because lower
totals remain preferred.

## Exactness and deterministic ties

Totals aggregate fixed-scale integer coefficients before reconstructing the
result through `money.Parse`; no binary floating-point value is introduced.
This makes mixed-sign totals independent of container order while retaining
Money's final amount bounds and `errors.Is` overflow identity. Score components
retain the exact amount string and ISO currency code.

Lower total cost wins. Equal totals are ordered by the plans' canonical bytes,
so ties do not depend on map insertion, map iteration, solver candidate order,
locale, process, or architecture. An empty plan totals to exact zero in the
configured currency and context.

## Solver composition

Pass `Costs` as `solver.Options.PlanObjective`. The exact and heuristic solvers
invoke it only for complete candidate plans. Compose monetary preference with
other plan criteria at the application boundary when multiple objectives are
needed; the adapter itself owns only total packaging cost.

Every selected container instance must have a configured type ID. A missing
type returns `ErrMissingCost`; there is no implicit zero, fallback cost, or
registry lookup.

## Limits and errors

`DefaultLimits` accepts at most 1,000 types and 1,024 bytes per type ID. Custom
limits must keep both bounds positive. Empty or whitespace-only IDs, oversized
IDs, empty mappings, excessive mappings, invalid Money values, duplicates,
mixed currencies, mixed contexts, unsupported contexts, and disallowed
negative values return `ErrInvalidCosts`. Identifier byte limits are checked
before identifiers are copied, trimmed, compared, or sorted. Specific causes
remain detectable:

- `ErrDuplicateTypeID`
- `ErrUnsupportedScale`
- `ErrNegativeCost`
- `money.ErrInvalidMoney`
- `money.ErrCurrencyMismatch`
- `money.ErrContextMismatch`

`ErrMissingCost` is reserved for plans whose container type is absent from a
valid configuration. Context cancellation from objective callbacks and Money
arithmetic errors are returned unchanged.

## Adoption, tradeoffs, and security

Use this adapter when container costs already exist as exact Money values and
currency conversion is outside the packing decision. Validate raw repeated
configuration with `NewFromEntries`; use map constructors only after duplicate
handling is complete. Keep separate objectives for taxes, discounts, shipping
rates, or time-varying quotes.

Construction costs `O(n log n)` to establish canonical lookup order. Lookup is
binary search and total calculation is linear in selected containers. The
adapter starts no goroutines and performs no network, filesystem, clock,
environment, locale, registry, or exchange-rate access.

## Compatibility and migration

The module is pre-v1 and independently released under tags prefixed with
`pkg/knapsack/objective/gomoney/v`. Its public API is checked against
`api/baseline.txt`.

Existing map callers can continue using `New` or `NewWithLimits`. Configurations
that previously relied on negative costs must migrate to `NewWithPolicy` and
explicitly set `AllowNegativeCosts`. Cash and automatic Money contexts must be
recreated with one fixed default or custom context; the adapter never performs
that conversion.

## FAQ

### Why not use `float64` costs?

Binary floating point cannot preserve arbitrary decimal money exactly. This
adapter delegates bounded decimal arithmetic and error identity to Money.

### Does a missing cost mean zero?

No. It returns `ErrMissingCost` so incomplete configuration cannot silently
change solver rankings.

### Can currencies be converted during packing?

No. Convert upstream under an explicit rate and rounding policy, then provide
one currency and one fixed context.

### Why provide both maps and entries?

Go maps are convenient immutable-construction inputs, but duplicate keys have
already been collapsed. Entry sequences let decoders reject duplicates before
constructing the objective.

## Development

Repository gates enforce formatting, tests, race safety, bounded fuzzing,
exactly 100% statement coverage, API compatibility, documentation examples,
benchmarks, and exactly 100% viable mutation kills. Use the repository's
module runner so owned dependencies resolve through its isolated local proxy.
