# Typed prompt definitions

Every definition has a stable identity, localized display metadata,
accessibility metadata, an explicit default and fallback, bounded or explicitly
unlimited retry policy, cancellation behavior, EOF behavior, secret
classification, and headless behavior. `Describe` returns a value snapshot of
that contract without exposing executor state.

The initial typed definitions are:

| Constructor | Result | Accepted explicit input |
| --- | --- | --- |
| `NewText` | `string` | one line without CR or LF |
| `NewMultiline` | `string` | text including line breaks; Ctrl-J inserts a line interactively |
| `NewSecret` | `SecretValue` | classified string input with redacted formatting |
| `NewSecretBytesPrompt` | `*SecretBytes` | classified bytes with explicit best-effort destruction |
| `NewConfirm` | `bool` | caller vocabulary, or the default English boolean vocabulary |
| `NewSelect[T]` | `T` | one stable option identity |
| `NewMultiSelect[T]` | `[]T` | comma-separated stable option identities |
| `NewSearchSelect[T]` | `T` | one stable identity with bounded interactive search |
| `NewInteger` | `int64` | strict base-10 signed integer |
| `NewDecimal` | `Decimal` | exact ordinary decimal notation without exponent or special values |
| `NewDuration` | `time.Duration` | Go duration syntax |
| `NewDate` | `Date` | strict `YYYY-MM-DD` calendar date |
| `NewTime` | `TimeOfDay` | `HH:MM`, `HH:MM:SS`, or fractional seconds through nanoseconds |
| `NewPath` | `Path` | non-empty path text without NUL |

`Decimal` stores canonical decimal digits and scale rather than binary
floating point. It intentionally rejects exponent notation, infinity, and
NaN. `Date` and `TimeOfDay` intentionally carry no location. Callers must apply
their own time zone when combining them into an instant.

`Path.Kind` records caller intent as any path, file, or directory. It is not a
filesystem assertion. Parsing never stats, creates, resolves, opens, or
normalizes a path.

Selection definitions keep identity separate from generic value and display
label. Duplicate identities are invalid; duplicate labels and values are
unambiguous and allowed. Search uses bounded NFKC-normalized Unicode case-folded
ranking. See [Selection and search](selection.md) for ordering, pagination,
dynamic-provider, and large-option-set contracts.

String and byte secret prompts require an explicit `SecretClass`. Their typed
results redact formatting, serialization, and structured logging, but only the
byte-backed form owns memory that can be overwritten. See
[Secret handling](secrets.md) for default, paste, cleanup, and Go memory limits.

`Parse` is for explicit caller-supplied input such as configuration, argv, an
API request, or a batch record. It does not acquire input and is therefore the
preferred non-interactive composition seam. Parsing failures use the same
stable validation exhaustion class as rejected interactive submissions.
