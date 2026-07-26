# GS1 recipes

Parse bracketed human input into validated elements before encoding:

```go
elements, err := gs1.ParseBracketed(
    "(01)09501101530003(10)LOT42",
    gs1.ParseLimits{MaxInputBytes: 1024, MaxElements: 32},
)
if err != nil {
    return err
}
symbol, err := code128.EncodeGS1(elements, code128.Options{})
```

`ElementString.Raw()` emits scanner data with ASCII group separators where a
variable-length element is followed by another element. `Bracketed()` emits a
human-readable representation. The embedded application-identifier dictionary
is pinned in `specification/manifest.json`; update it only with the checksum-
verifying synchronization script.

`CalculateCheckDigit` accepts digits without their GS1 modulo-10 digit.
`ValidateCheckDigit` accepts the complete value. Both reject empty, non-digit,
and malformed inputs.

The dictionary parser enforces declared lengths, character classes, known
component checks, repeated `req=` groups, conjunctive/alternative requirements,
wildcard AI patterns, `ex=` exclusions, allocation tables, alphanumeric check
pairs, and coupon grammars. Company-prefix position checks remain structural
when an allocation data source is unavailable, matching the reference
implementation's offline behavior. Independent software reader and writer
tests provide the interoperability evidence for advertised GS1 formats.
