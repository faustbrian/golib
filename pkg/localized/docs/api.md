# API reference

## Root package

### Values and construction

- `Text`: immutable canonical entries; zero value is empty and concurrent-read
  safe.
- `Entry`: typed `international/locale.Tag` and UTF-8 `Text` pair.
- `Pair`: canonical string tag and text pair for boundaries.
- `Builder`, `NewBuilder`, `Add`, `AddString`, `Build`: incremental owned
  construction.
- `NewText`, `NewTextWithLimits`, `NewTextWithOptions`, `TextFromMap`:
  constructors with validation and canonical duplicate handling.
- `TextFromPairs`, `TextFromPairsWithOptions`: strict string-pair constructors
  with the same ownership, duplicate, locale, and limit policies.
- `ConstructionOptions`: `Limits`, `Duplicates`, and `Locales`.
- `DuplicatePolicy`: `RejectDuplicates`, `FirstWins`, `LastWins`.
- `LocalePolicy`: optional rejection of `und`, `mul`, and private use.
- `Limits`, `DefaultLimits`: locale, tag, per-text, and total byte budgets.

### Inspection and transforms

- `Len`, `IsEmpty`, `Has`, `Get`, `Require`: presence and exact lookup.
- `Locales`, `Entries`, `Pairs`: caller-owned deterministic slices.
- `All`: deterministic range-over-function iterator.
- `Set`, `Remove`, `Filter`, `Map`: persistent transforms.
- `Equal`, `Hash`: stable structural identity and length-framed SHA-256.

### Merge

- `Merge`, `MergeWithOptions`: immutable merge operations.
- `MergePolicy`: `LeftWins`, `RightWins`, `RejectConflict`,
  `ResolveConflict`.
- `EmptyPolicy`: `EmptyIsValue`, `EmptyIsAbsent`.
- `MergeResolver`: canonical tag plus left/right strings to one result.
- `MergeOptions`: conflict, empty, resolver, and output limit policy.

### JSON and errors

- `EncodeJSON`, `DecodeJSON`: canonical strict object codec.
- `JSONMode`: `StrictJSON`, `PermissiveJSON`.
- `DecodeOptions`: mode, parser input bytes, and value limits.
- `MarshalJSON`, `UnmarshalJSON`: standard-library integration.
- `Error`: immutable comparable error identity.
- Errors: `ErrConflict`, `ErrDuplicateLocale`, `ErrInvalidEncoding`,
  `ErrInvalidLocale`, `ErrInvalidPolicy`, `ErrInvalidUTF8`,
  `ErrLimitExceeded`, `ErrLocaleRejected`, `ErrMissingLocale`,
  `ErrNullValue`, `ErrResolverRequired`, and `ErrTrailingInput`.

## `match`

- `Preference`: tag and inclusive 0..1 weight.
- `Result`: kind, requested and selected tags, text, presence, and empty state.
- `Kind`: `Missing`, `Exact`, `Matched`, `Fallback`, `Default`.
- `Best`, `BestWithOptions`, `Options`: explicit standards matching and bounds.
- `FallbackPlan`, `NewFallbackPlan`, `Resolve`: ordered exact fallback.
- `CandidateKind`: `ExactLocale`, `ParentRange`.
- `Candidate`, `Chain`, `PlanOptions`, `Plan`, `NewPlan`, `Resolve`: validated
  fallback graphs.
- `Operation`: `OperationMatch`, `OperationFallback`.
- `Event`, `Observer`, `ObserverFunc`: content-free panic-isolated hooks.
- Errors: `ErrCandidateLimit`, `ErrDepthLimit`, `ErrDuplicateCandidate`,
  `ErrFallbackCycle`, `ErrInvalidCandidate`, `ErrInvalidWeight`.

## `encoding`

- `Entry`: `locale`/`text` JSON entry.
- `DecodeOptions`: entry-array input and value limits.
- `MarshalEntries`, `UnmarshalEntries`: stable strict entry arrays.

## `http`

- `ParseOptions`: header byte and candidate limits.
- `ParseAcceptLanguage`: strict weighted range parser.
- `Select`: parse and match, including explicit wildcard behavior.
- `Error` and errors: candidate/header limits, duplicate range, invalid
  parameter/range/weight.

## `postgres`

- `Text`, `NewText`, `Value`, `Scan`: nullable transactional SQL boundary.
- `JSONBCodec`: native pgx JSONB codec factory.
- `Row`, `Rows`, `FromRows`: deterministic normalized-row helpers.
- `Error`, `ErrUnsupportedDatabaseType`: safe persistence error identity.

## `localizedvalidation`

- `Rule`, `Validate`: composable deterministic validation.
- `Validator`: typed `validation` adapter with canonical locale-key paths.
- `RequireNonEmpty`, `RequireNonWhitespace`, `MaxBytes`, `MaxRunes`,
  `MaxLines`, `NoControlCharacters`: built-in rules.
- `Form`: `NFC`, `NFD`, `NFKC`, `NFKD`.
- `Normalize`: explicit persistent normalization.
- `Error` and errors for each built-in failure class.

## `localizedwire`

`EncodeJSON`/`DecodeJSON`, `EncodeYAML`/`DecodeYAML`,
`EncodeTOML`/`DecodeTOML`, and
`EncodeMessagePack`/`DecodeMessagePack` delegate bounded I/O to corresponding
`wire` packages and revalidate localized ownership.

## `localizedconfig`

- `Text`, `NewText`: present/null wrapper.
- `ConfigTextTarget`, `UnmarshalConfigValue`: config structural hooks.
- `Error`, `ErrInvalidValue`: safe input-shape error.

## `localizedquery`

- `ExactValue`: convert only a present exact locale to `apiquery.Value`, while
  preserving present-empty.
- `ExactPredicate`: build one exact-value predicate or return
  `localized.ErrMissingLocale`; matching and fallback are never implicit.

## `localizedhttpclient`

- `WithPreferences`: write or remove a canonical bounded `Accept-Language`
  header on an immutable http-client request spec.
- `SelectResponse`: select from the originating request preferences using
  matching only, never configured fallback.
- `Error`, `ErrInvalidResponse`: safe response-shape error identity.

## `localizedtest`

- `Builder`, `New`, `Add`, `Build`: fail-fast fixtures.
- `AssertEqual`, `AssertExact`, `AssertResult`: consumer assertions.
- `CanonicalizationVector`, `CanonicalizationVectors`: caller-owned standards
  cases with provenance labels.
