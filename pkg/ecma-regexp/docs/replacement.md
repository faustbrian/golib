# Replacement and split

`Replace` and `ReplaceUTF16` implement ECMAScript substitution tokens:

| Token | Meaning |
|---|---|
| `$$` | literal dollar sign |
| `$&` | complete match |
| ``$` `` | prefix before the match |
| `$'` | suffix after the match |
| `$n`, `$nn` | numbered capture when present |
| `$<name>` | named capture when names exist |

An unmatched capture contributes the empty string. Text that does not form a
valid substitution token is preserved according to ECMAScript rules. The `g`
flag selects repeated replacement; without it, only the first match is
replaced.

`Split` inserts separator captures in order. `SplitValue.Defined` preserves
the distinction between an unmatched capture (`undefined` in ECMAScript) and
an empty string. Result count and total UTF-16 output are bounded by the match
options.
