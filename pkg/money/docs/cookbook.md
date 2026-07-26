# Cookbook

## Historic currency

Parse the code with `AllowHistoric: true`, select an explicit context when the
registry has no applicable minor units, then call `money.Parse`.

## Weighted invoice split

Convert positive weights to `math/integer.Integer` values and call
`Allocate`. Persist returned parts in order if deterministic remainder ownership
matters to downstream reconciliation.

## Cash settlement

Calculate using the accounting context. Convert to `RationalMoney` with a rate
of one, then call `Round` with the cash context and selected mode. Keep the
accounting value and cash settlement as distinct records.

## FX conversion

Construct `ExchangeRate` from an exact decimal or rational rate, the directed
base and quote codes, the observation timestamp, and a bounded source label.
Call `Convert` with the quote context and an explicit mode.

## Database round trip

Use `encoding.SQLMoney` for one versioned text/JSON column, or use
`NumericValue` with separate currency and context columns. Round-trip tests
should include negative, zero, huge, historic, zero-minor-unit, and
three-minor-unit values.
