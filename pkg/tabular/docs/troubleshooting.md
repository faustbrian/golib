# Troubleshooting

## `invalid configuration`

Check nil sources, negative limits, unsupported spreadsheet formats, invalid
delimiters, and negative field counts.

## `malformed row`

Inspect `Error.Row` and `Error.Field`. For delimited input, verify quoting and
field-count policy. For fixed-width input, verify byte offsets and short or
trailing record policy.

## `invalid encoding`

Confirm the source contract. UTF-8 is validated strictly. Do not retry with a
legacy encoding merely to make arbitrary bytes decode.

## `archive error` or `limit exceeded`

Inspect entry names and declared expanded sizes. Raising a limit should be a
deliberate policy decision, not an automatic retry.

## Spreadsheet opens but rows differ

Confirm the exact sheet, header consumption, raw-value expectation, field
count, and cell-error policy. XLS formulas are not evaluated and formatting is
not a value contract.
