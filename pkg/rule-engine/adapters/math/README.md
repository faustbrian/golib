# Exact decimal operators for rule-engine

The `rule-engine/adapters/math` module is the optional bridge between
[`math/decimal`](../../../math/decimal) and [`rule-engine`](../..). It encodes
finite decimals as tagged string values and supplies deterministic equality and
ordering operators. The core rule engine does not depend on the math module.

## Quick start

```go
operators := ruleenginemath.Operators()
compiler, err := ruleengine.NewCompilerWithOperators(
	ruleengine.DefaultLimits(),
	operators...,
)
if err != nil {
	return err
}

minimum := ruleenginemath.Decimal(decimal.MustParse("10.00"))
predicate := ruleengine.Compare(
	ruleenginemath.OpDecimalGreaterOrEqual,
	ruleengine.Variable(ruleengine.MustPath("order", "total")),
	ruleengine.Literal(minimum),
)
```

The application owns the returned operator slice and explicitly registers it
with each compiler that needs decimal support. Importing this package changes
no global state.

## Encoding

`Decimal` returns a `ruleengine.KindString` containing:

```text
golib.rule-engine.decimal/v1:<canonical-decimal>
```

The prefix is exported as `EncodingV1Prefix`. The payload is the decimal's
canonical text representation, including explicit fractional scale. Examples
include `golib.rule-engine.decimal/v1:0`,
`golib.rule-engine.decimal/v1:-12.50`, and
`golib.rule-engine.decimal/v1:0.0001`.

Evaluation accepts only the v1 tag and the strict, non-exponent decimal grammar.
A payload must round-trip through `decimal.ParseWithOptions` to the same string.
This rejects malformed data and alternate text such as `-0`; separately encoded
representations such as `1.0` and `1.00` remain canonical because their scale is
intentional.
Positive-exponent zero is persisted as `0`, matching `decimal.MarshalText`, so
the encoder never emits a leading-zero form that the strict parser rejects.

## Operators and API

All operators accept exactly `(KindString, KindString)`:

| Constant | Stable name | Relation |
| --- | --- | --- |
| `OpDecimalEqual` | `golib_decimal_v1_equal` | `left == right` |
| `OpDecimalLessThan` | `golib_decimal_v1_less_than` | `left < right` |
| `OpDecimalLessOrEqual` | `golib_decimal_v1_less_or_equal` | `left <= right` |
| `OpDecimalGreaterThan` | `golib_decimal_v1_greater_than` | `left > right` |
| `OpDecimalGreaterOrEqual` | `golib_decimal_v1_greater_or_equal` | `left >= right` |

`Operators()` returns a new five-element set using `math.DefaultLimits()`.
`OperatorsWithLimits(limits)` validates and copies application-supplied limits
into a new set. Each call returns fresh slice storage; operators and their
signatures expose no shared mutable state.

## Exactness and limits

Payloads are parsed directly as base-10 decimals and compared with
`decimal.Decimal.Cmp`. There is no binary floating-point conversion, expression
evaluation, rounding, or numeric precision loss. Numeric comparison normalizes
scale, so `1.0` and `1.00` compare equal while both retain their persisted
fractional scale. As defined by `decimal.Decimal.String`, a positive exponent
(negative scale) is expanded into integral zeros: coefficient `12` at exponent
`2` persists as `1200` and reparses at scale `0` without changing its value.

The selected math limits bound input digits, output digits, coefficient size,
and exponent magnitude. Invalid limit configurations retain
`math.ErrInvalidArgument`; oversized magnitudes and scales retain
`math.ErrLimitExceeded`; invalid decimal syntax retains `decimal.ErrInvalid`.
Wrong rule value kinds and unknown tags return `ErrInvalidTaggedValue`, and
noncanonical payloads return `ErrNonCanonicalDecimal`. Context cancellation is
checked before and between parsing operations and is returned without wrapping.
Digit parsing stops allocating as soon as `MaxInputDigits` is exhausted.

## Composition and adoption

Use the adapter only at boundaries that already model values as exact decimals.
Keep `math/decimal` values in domain code, call `Decimal` when constructing rule
literals or facts, and register `Operators()` on the isolated compiler owned by
the application. Persist the complete tagged string, not only its payload.

Choose `OperatorsWithLimits` when persisted or untrusted values need limits
tighter than the math defaults. All engines reading the same rule data should
use the same encoding version and limits. The adapter performs no I/O, starts no
goroutines, and reads no clock, environment, network, or registry.

For canonical RuleSet persistence, construct the compiler with this adapter's
operators and call `compiler.MarshalCanonical`, `compiler.CanonicalHash`, and
`compiler.ParseJSON`.
Package-level rule-engine persistence helpers intentionally recognize only
built-in operators. Reuse the same operator limits when parsing and evaluating
persisted definitions.

## Compatibility and migration

The v1 prefix and the five versioned operator names are persistence contracts.
Future incompatible encodings or comparison contracts will use a new tag and
new operator names so versions can coexist during migration.

The former unversioned `decimal:` tag and operator names such as
`decimal_equal` are intentionally rejected. Before upgrading persisted rules,
rewrite each decimal value from `decimal:<payload>` to
`golib.rule-engine.decimal/v1:<payload>` only after parsing and re-encoding the
payload with `Decimal`. Replace every legacy operator name with its
`golib_decimal_v1_*` equivalent, deploy readers that understand v1, and only
then retire legacy data. Do not perform a textual prefix replacement on
unvalidated values.

## Security notes

Treat persisted rule values as hostile. Use limits appropriate for the storage
boundary, preserve cancellation, and avoid logging rejected payloads. Adapter
errors contain classifications rather than operand contents. Registration is
caller-owned, so applications should reject duplicate operator names during
compiler construction instead of selecting an implementation dynamically.

## FAQ

### Why are decimals represented as strings?

`ruleengine.Value` has no decimal kind. A tagged canonical string keeps the core
module independent and survives canonical rule serialization without a float.

### Does equality distinguish `1.0` from `1.00`?

No. Their persisted scale remains intact, but every relation uses numeric
decimal comparison. Use `decimal.SameRepresentation` outside the rule engine if
representation equality is the domain contract.

### Are operators registered automatically?

No. Importing the package has no side effects. The compiler owner must pass a
fresh operator set to `NewCompilerWithOperators`.

### Can v1 read exponent notation?

No. Encode a parsed decimal with `Decimal`; its canonical output is always a
non-exponent representation.
