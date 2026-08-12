# Goal: pkg/identity/i18n

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/i18n`
- Canonical module: `pkg/identity/i18n`
- Canonical goal after scaffolding: `pkg/identity/i18n/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/i18n:v1`; owned operation IDs: `contract:operation:identity.i18n.resolve:v1`
- Requires: `identity`
- Consumes existing primitives: `identifier`, `audit`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with `identity` verified. Build a
transport-neutral localization layer for identity-facing messages that
preserves stable machine error identity and the original message while
selecting a safe localized public rendering.

## Ownership and public contract

The module owns BCP 47 locale parsing/canonicalization, supported-locale
matching, fallback chains, message catalogs, interpolation rules, plural/select
behavior required by supplied catalogs, and locale-source precedence. It does
not translate logs/audit records, infer sensitive demographics, own HTTP
cookies/headers, or replace typed domain errors.

Public contracts MUST define immutable `Catalog`, `MessageID`, `ErrorRegistry`,
`LocalizableError`, `SafeParameter`, locale matcher,
resolver inputs for explicit preference, authenticated session preference,
cookie, `Accept-Language` and default, and a localized error wrapper that
retains `errors.Is`/`errors.As`, machine code, original message identity and
safe parameters. Missing locale/message/parameter and malformed input behavior
MUST be deterministic. Catalog loading MUST reject duplicate IDs, invalid
templates and incompatible placeholder sets across translations.
`LocalizableError` MUST expose stable machine error identity and bounded typed
safe parameters without a rendered message. `ErrorRegistry` MUST expose exactly
`Register(ErrorIdentity, MessageID, ParameterSchema) error` during construction
and `Resolve(error) (MessageID, []SafeParameter, bool)` after sealing. Each
machine identity MUST have one owning-package registration; duplicate or
conflicting registrations fail construction, unknown errors preserve their
typed identity without guessing a message, and lookup MUST NOT inspect error
strings or expose secret parameters.

## Required behavior and privacy

Precedence MUST be explicit preference, authenticated stored preference,
trusted cookie, bounded `Accept-Language`, then configured fallbacks. Matching
MUST handle weights, wildcards, aliases and region fallback without locale
explosion or attacker-controlled catalog lookup. Unsupported locales MUST NOT
be persisted as accepted. Interpolation MUST escape at the transport boundary
and MUST never format secrets, raw provider diagnostics or enumeration state.

Locale preference is personal data: persistence, consent/notice, cookie
integrity, retention and deletion MUST be documented. Locale, message ID and
bounded result may be telemetry dimensions only through controlled cardinality.
Concurrent reads MUST be immutable or synchronized; catalog updates MUST be
atomic and deterministic.

Locale ownership MUST remain split explicitly: this module owns preference
mutation policy and defines the consumer-owned `PreferenceStore` and
`PreferenceLifecycleContributor` interfaces; session may cache a resolved
preference, and HTTP owns trusted cookie/header extraction and response
metadata. `PreferenceStore` MUST expose exactly
`GetPreference(context.Context, PreferenceKey) (Preference, error)` and
`SetPreference(context.Context, PreferenceCommand) (Preference, error)`.
`PreferenceLifecycleContributor` MUST expose exactly
`ContributePreferenceLifecycle(context.Context, PreferenceLifecycleCommand) (PreferenceLifecycleResult, error)`
for export, anonymization, and deletion. Commands MUST bind tenant, subject,
expected version, canonical locale, consent/notice version, stable command ID,
and lifecycle snapshot/generation where applicable. Concrete persistence and
reference composition implement these interfaces downstream; no already-
scheduled upstream package is retroactively assigned a new public contract.
Persistence, export and deletion MUST follow
`.ai/identity-platform/LIFECYCLE_CASCADES.md`.

`identity/i18n` MUST be the exact personal-data contributor named in the
version-1 privacy-export manifest and in identity anonymization/deletion
cascades. For export, `PreferenceLifecycleContributor` MUST validate the full
`PrivacyExportSnapshotV1` contributor binding and return a canonical
`PrivacyExportFragmentV1` containing only the canonical stored locale,
preference version, consent/notice version, and registered preference-update
checkpoint, or the exact `not-applicable` fragment when no preference exists.
It MUST NOT export trusted-cookie contents, raw `Accept-Language` input,
catalog lookup history, rendered messages, or telemetry. The fragment contract
version, projection ID, scope-key digest, requested/observed checkpoint,
snapshot digest, classification, byte length, and content digest are mandatory;
an export ID or timestamp alone is insufficient authority.

For anonymization and deletion, the contributor MUST atomically remove the
durable preference and persist its cascade ID, manifest version, generation,
privacy epoch, command ID, prior preference version, and resulting tombstone or
absence checkpoint. Results are exactly `applied`, `not-applicable`, `pending`,
or `outcome-unknown`; only a generation-matching `applied` or proved
`not-applicable` acknowledges closure. Duplicate commands return the persisted
result, stale snapshot/version/generation is rejected, and an unknown commit is
reconciled before retry. Session preference caches and HTTP cookies are
non-authoritative projections: their invalidation/expiry is owned by their
respective lifecycle consumers and cannot substitute for durable preference
deletion or this acknowledgement.

Escaping MUST occur exactly once in the owner of the final output context.
Catalog interpolation MUST preserve typed parameters and reject templates that
place values in undeclared contexts; this module MUST NOT claim that generic
HTML escaping makes URL, header, JSON, SMS or plain-text output safe. The
delivery renderer owns message-channel context escaping, and `identity/http`
owns HTTP/HTML/JSON context escaping, while machine error identity and original
safe parameters remain unchanged.

## Acceptance and blockers

Tests MUST cover canonicalization, weights, fallbacks, missing messages,
placeholder mismatch, original-error preservation, custom locale detection,
cookie/session precedence, exact-snapshot privacy export, absent-preference
`not-applicable`, anonymization/deletion, duplicate delivery, stale generation,
unknown-commit reconciliation, escaping and concurrent catalog swaps.
Official BCP 47/HTTP language fixtures, parser/template fuzzing, exact
coverage/mutation, race, lookup benchmarks, clean-consumer, API/docs with
catalog authoring/update procedure, changelog and supply-chain gates MUST pass.
This unit MUST prove transport-neutral locale selection and error translation;
`identity/http` and `identity/reference` own the later localized-envelope and
composed journey proof.

The unit MUST remain unverified if localization changes machine semantics,
breaks error unwrapping, uses unbounded attacker locale keys, drops original
identity, or exposes sensitive interpolation data.
