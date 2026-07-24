# Hostile input and resource safety

Always lower defaults at a trust boundary when the application knows tighter
limits. A parser for a single header may reasonably use `ParseBytes: 256`; a
bulk API should cap both `InputPeriods` and `OutputPeriods` per request.

Precision is zero through nine decimal digits. Split and step operations reject
non-positive steps and stop at `Limits.Steps`. Arithmetic checks overflow before
allocation or returned mutation. Formatting checks output bytes.

Unicode punctuation that resembles ASCII brackets, commas, slashes, signs, or
digits is rejected. There is no natural-language or locale-dependent parser.

Do not resolve a local time onto a date without an explicit location and DST
policy. A local time alone is not an instant. Do not treat a calendar day as
`24*time.Hour`; DST transitions disprove that conversion.
