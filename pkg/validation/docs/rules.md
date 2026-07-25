# Rule catalog

Every failure code below is stable machine identity. Parameters contain bounds
only; rejected values are never retained.

| Area | Constructors | Codes |
| --- | --- | --- |
| Presence | `Required`, `Present`, `Omitted`, `Prohibited`, `Empty`, `ZeroValue` | `required`, `present`, `omitted`, `prohibited`, `empty`, `zero` |
| Strings | `ByteLength`, `RuneLength`, `Pattern`, `Prefix`, `Suffix`, `OneOf` | `byte_length`, `rune_length`, `pattern`, `prefix`, `suffix`, `one_of` |
| Numeric | `Range`, `GreaterThan`, `LessThan`, `Finite`, `Precision`, `MultipleOf` | `range`, `greater_than`, `less_than`, `finite`, `precision`, `multiple_of` |
| Collections | `SliceSize`, `MapSize`, `Unique`, `Items`, `Keys`, `Values`, `Nested` | `size`, `unique`, nested codes, `collection_limit` |
| Time | `TimeBetween`, `Before`, `After`, `DurationBetween`, `Future`, `Past`, `Date`, `OrderedInterval` | `time_range`, `before`, `after`, `range`, `future`, `past`, `date`, `interval_order` |
| Cross-field | `FieldsEqual`, `FieldsOrdered`, `RequiredWhen`, `ExcludedWhen` | `equal`, `ordered`, `required`, `excluded` |
| Primitives | `URL`, `Hostname`, `IP`, `CIDR`, `Email`, `UUID`, `Identifier` | constructor name in lowercase |

Lengths and ranges are inclusive. `Before`, `After`, `Past`, and `Future` are
strict. Rune length counts Unicode code points; byte length counts encoded
bytes. `Pattern` compiles once with Go RE2 and rejects expressions beyond
`MaxRegexPatternLength`. `URL` accepts absolute HTTP(S) URLs. `Email` accepts
bare syntactic mailboxes, not display-name forms; it does not claim delivery.

`Items` validates slice indexes in ascending order. `Keys` and `Values` require
an ordered key type and sort keys before validation. Traversal rules reject
oversized collections before calling the child validator. String-facing rules
reject values beyond `MaxStringLength` before parsing, matching, comparison,
sorting, or hashing. `Unique` reports the repeated index. `Nested` applies an
explicit typed accessor at a field path.

`MultipleOf` requires a positive finite divisor. `FieldsOrdered` accepts equal
values and requires the left field not to exceed the right.

Domain-specific country, postal, carrier, authorization, database existence,
and uniqueness policies deliberately remain outside this package.
