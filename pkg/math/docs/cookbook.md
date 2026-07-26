# Cookbook

Parse an exact amount with `decimal.Parse`, then use `Quantize` for a fixed
fractional scale. Use `Context.Quo` when a quotient may repeat and inspect
`Result.Conditions`; use `QuoExact` when repetition must fail. Construct ratios
with `rational.New` and call `Decimal` with a scale and rounding mode for a
bounded expansion. Use `integer.Random` with `crypto/rand.Reader` in production
or a deterministic reader in tests.

For shared configuration, construct a context once and pass it by value. Values
and contexts contain no mutable shared state exposed by the API.

