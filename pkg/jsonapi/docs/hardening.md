# Hardening report

This report records the pre-v1 security, interoperability, resource-safety,
and specification audit completed on 2026-07-14. It is an evidence ledger,
not a substitute for the executable [conformance matrix](conformance.md).

## Verdict

No known high- or medium-severity protocol finding remains open. Core,
Atomic Operations, Cursor Pagination, query parsing, media negotiation, and
configured extension paths are bounded and have adversarial regression
coverage within the package boundary.

The repository has no known local publication blocker. The MIT license is
present, and the release workflow validates it before publishing. Go 1.24 and
non-macOS compatibility remain CI-owned checks; this audit ran locally with
Go 1.26.5 on macOS arm64.

## Finding ledger

Each row records severity, specification classification with a stable section
link where applicable, finding and impact, a directly runnable reproduction or
executable evidence file, and disposition.

| Severity | Classification and section | Finding and impact | Reproduction and evidence | Disposition |
| --- | --- | --- | --- | --- |
| High | Package security | Unbounded documents, query values, and negotiation headers could exhaust memory or CPU. | `resource_limits_test.go`, `query_limits_test.go`, `negotiation_limits_test.go` | Fixed with configurable bounded defaults before semantic processing. |
| High | Package security | Transaction callback panics could escape an Atomic boundary or skip cleanup. | `atomic_execute_test.go`, `error_contract_test.go` | Fixed with typed panic conversion and exactly one rollback attempt after begin. |
| Medium | [JSON:API core](https://jsonapi.org/format/#document-resource-object-identification) | Empty `id` and `lid` strings were confused with absent members, changing valid wire data. | `presence_test.go`, `identity_validation_test.go` | Fixed with explicit presence state and constructors. |
| Medium | [JSON:API compound documents](https://jsonapi.org/format/#document-compound-documents) | Included resources carrying both `id` and `lid` were indexed only by `id`, causing valid linkage through the local identity to be rejected. | `validation_test.go` | Fixed by indexing both identity aliases while traversing included resources once. |
| Medium | [JSON:API resource identity](https://jsonapi.org/format/#document-resource-object-identification) | Canonical uniqueness used only one identity key, allowing duplicate full resource objects when one representation used `id` plus `lid` and another used the same `lid`. | `identity_validation_test.go` | Fixed by detecting canonical duplicates across both identity aliases. |
| Medium | [JSON:API update requests](https://jsonapi.org/format/#crud-updating) | Endpoint identity checks could not represent a valid empty expected resource ID. | `context_validation_test.go` | Fixed with explicit `ExpectedIDPresent` state while preserving non-empty shorthand behavior. |
| Medium | [Atomic Operations](https://jsonapi.org/ext/atomic/#operation-objects) | Atomic relationship-data validation dropped empty `id` and `lid` presence while converting resource identifiers. | `atomic_test.go`, `atomic_codec_test.go` | Fixed by preserving identity presence through the validation adapter. |
| Medium | [Atomic local identities](https://jsonapi.org/ext/atomic/#operation-objects) | Prior-operation resolution was enforced only for `ref.lid`; lid-only update targets and relationship linkage could reference resources not yet created in the linear operation sequence. | `atomic_test.go` | Fixed by resolving all lid-only target and linkage identities against current/prior resource adds. |
| Medium | [Atomic resource updates](https://jsonapi.org/ext/atomic/#updating-resources) | Resource update `ref` and `data` identities were validated independently, allowing directly contradictory target types, IDs, or LIDs. | `atomic_test.go` | Fixed by comparing identity members available in both target forms; mixed ID/LID and href resolution remain application-owned. |
| Medium | [Atomic relationship operations](https://jsonapi.org/ext/atomic/#updating-to-many-relationships) | `href` targets were always treated as resources for add/update and accepted arbitrary data for remove, preventing valid relationship operations and allowing invalid shapes. | `atomic_test.go` | Fixed by inferring unambiguous relationship semantics from data shape and requiring a target. |
| Medium | [Atomic operation results](https://jsonapi.org/ext/atomic/#result-objects) | Response validation checked only result count and accepted data after removals or relationship mutations, as well as non-resource data shapes for resource results. | `atomic_test.go`, `atomic_execute_test.go` | Fixed with request-aware positional result validation before transaction commit. |
| Medium | [Atomic result objects](https://jsonapi.org/ext/atomic/#result-objects) | A server-generated create could return an empty result, leaving the client without the required created representation and allowing an invalid transaction to commit. | `go test ./... -run 'TestAtomicResultRequiresDataForServerGeneratedCreate|TestExecuteAtomicRollsBackMissingServerGeneratedCreateData'` | Fixed by requiring resource data for server-generated creates while preserving the client-generated-ID omission rule. |
| Medium | [JSON:API core](https://jsonapi.org/format/#document-resource-object-fields) | `lid` was incorrectly reserved as a resource field name. | `validation_edge_test.go` | Fixed; only `type` and `id` are reserved in the field namespace. |
| Medium | [JSON:API member names](https://jsonapi.org/format/#document-member-names) | U+007F DELETE was accepted as globally allowed Unicode even though the allowed non-ASCII range starts at U+0080. | `validation_edge_test.go`, `query_test.go` | Fixed by using a strict boundary above ASCII. |
| Medium | [JSON:API extension members](https://jsonapi.org/format/#extension-members) | Extension suffix validation treated an internal `@` as the start of an @-Member, accepting names such as `ext:@value`. | `member_codec_defense_test.go` | Fixed by applying implementation-member grammar without the top-level @ exception. |
| Medium | [JSON:API @-Members](https://jsonapi.org/format/#document-at-members) | Constructed @-Members were rejected by configured marshal yet incorrectly satisfied top-level and relationship content requirements. | `member_codec_defense_test.go`, `validation_edge_test.go` | Fixed by making valid @-Members registration-exempt but structurally invisible. |
| Medium | [JSON:API @-Members](https://jsonapi.org/format/#document-at-members) | The @-Member grammar leaked into semantic values, accepting `@`-prefixed resource types, identifiers, relationship targets, query paths, and profile aliases while interpreting constructed @ relationships and links. | `validation_edge_test.go`, `atomic_test.go`, `query_test.go`, `cursor_meta_test.go` | Fixed by separating semantic member-name validation and ignoring @ members in map-backed object containers. |
| Medium | [JSON:API core](https://jsonapi.org/format/#document-resource-object-relationships) | An empty or unrelated `links` object could incorrectly qualify a relationship. | `validation_edge_test.go` | Fixed; core relationships require `self`, `related`, data, meta, or an applied extension member. |
| Medium | [JSON:API pagination](https://jsonapi.org/format/#fetching-pagination) | Pagination links were accepted beside single-resource, null, meta-only, Atomic, and known to-one relationship data even though pagination links correspond to collections. | `validation_edge_test.go`, `atomic_test.go` | Fixed with data-cardinality-aware link validation; omitted relationship linkage remains application-resolved. |
| Medium | [JSON:API core](https://jsonapi.org/format/#document-links) | URI-reference validation rejected valid empty relative references and did not preserve link-member presence. | `link_test.go`, `presence_test.go` | Fixed with URI-reference parsing and presence-aware link objects. |
| Medium | [JSON:API links](https://jsonapi.org/format/#document-links) and [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986) | Go URL parsing accepted raw spaces, Unicode, and reserved path characters that require wire escaping. | `link_test.go`, `member_codec_defense_test.go`, `negotiation_test.go` | Fixed with component-aware RFC 3986 validation shared by links, Atomic hrefs, document URIs, and registries. |
| Medium | [JSON:API links](https://jsonapi.org/format/#document-links) | A core link name valid in one links-object scope was accepted in unrelated top-level, resource, relationship, or error links. | `validation_edge_test.go` | Fixed with scope-specific core member allowlists plus registered extension handling. |
| Medium | Package availability | Constructed recursive `describedby` links had no depth or pointer-cycle guard, allowing stack exhaustion before encoding. | `go test ./... -run TestConstructedDescribedBy` | Fixed with the package nesting limit and ancestor-cycle detection in link validation. |
| Medium | [JSON:API errors](https://jsonapi.org/format/#error-objects) and [HTTP status codes](https://www.rfc-editor.org/rfc/rfc9110.html#section-15) | Error `status` accepted non-HTTP strings and invalid values outside `100..599`. | `validation_edge_test.go` | Fixed with strict three-digit HTTP status validation. |
| Medium | [JSON:API extensions](https://jsonapi.org/format/#extensions) | Registered extensions could not define a namespaced value directly inside a `links` object. | `member_codec_test.go` | Fixed with a distinct `LinksObjectMemberScope`; unregistered core use remains rejected. |
| Medium | [JSON:API extension members](https://jsonapi.org/format/#extension-members) | An error object whose only semantic content was a registered extension member was rejected as empty, preventing valid extension-defined error semantics. | `go test ./... -run TestRegisteredExtensionMemberCanQualifyErrorObject` | Fixed by counting registered semantic members while keeping ignored @-Members structurally invisible. |
| Medium | [JSON:API object](https://jsonapi.org/format/#document-jsonapi-object) | Configured codecs accepted present `ext` and `profile` arrays that omitted configured applied URIs, making the advertised application set inaccurate. | `profile_codec_test.go`, `atomic_test.go` | Fixed by requiring configured URIs when either optional array is present; unsupported extensions fail and unknown profiles remain ignored. |
| Medium | [HTTP semantics](https://www.rfc-editor.org/rfc/rfc9110.html#section-12.5.1) | A wildcard could override a more specific JSON:API media range with quality zero. | `negotiation_test.go`, `fuzz_test.go` | Fixed by resolving quality per representation after media-range precedence. |
| Medium | [JSON:API profile negotiation](https://jsonapi.org/format/#content-negotiation-server-responsibilities) | Unrecognized profiles were removed from the representation but still increased range specificity, allowing an ignored `profile` range with quality zero to veto an acceptable base range. | `negotiation_test.go` | Fixed by calculating specificity after unsupported profiles are ignored. |
| Medium | [JSON:API media type](https://jsonapi.org/format/#media-type-parameters) | Extension/profile URI lists accepted tabs and Unicode whitespace even though only U+0020 is the defined separator. | `negotiation_test.go` | Fixed with exact ASCII-space tokenization and empty-token rejection. |
| Medium | [Cursor Pagination](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/) | Boundary links and metadata aliases did not fully express profile nullability and alias rules. | `cursor_page_test.go`, `cursor_meta_test.go` | Fixed with explicit link-state evidence and configurable `page` aliases. |
| Medium | Package security | Invalid UTF-8 was silently replaced during Go JSON decoding. | `codec_defense_test.go`, `resource_limits_test.go` | Fixed by rejecting invalid UTF-8 in the shared decode preflight. |
| Medium | Package confidentiality | Cursor/sort callback errors and Atomic panics could expose application-owned values through public error strings. | `error_contract_test.go`, `atomic_execute_test.go` | Fixed with redacted messages while retaining causes for `errors.Is` and `errors.As`. |
| Medium | Package confidentiality and availability | Extension/profile/cursor/sort errors exposed application text, and callback panics could cross the package boundary. | `go test ./... -run 'TestConfiguredCodec|TestCursorPaginationConvertsCallbackPanics'` | Fixed with redacted `CallbackError` values, preserved causes, and panic containment. |
| Medium | Package integrity | A profile validator could mutate the shallow document after core and extension validation, then return success and cause invalid or weakened output. | `go test ./... -run TestProfileValidatorCannotMutateValidatedDocument` | Fixed by fingerprinting the validated representation around each successful profile callback and rejecting mutation. |
| Medium | [Web Linking relation types](https://www.rfc-editor.org/rfc/rfc8288.html#section-3.3) | Registered relation validation accepted underscores even though the RFC token grammar excludes them, producing non-interoperable link metadata. | `go test ./... -run TestRegisteredLinkRelationRejectsUnderscore` | Fixed by aligning the registered-token grammar; absolute URI relation types remain supported. |
| Medium | [JSON:API member names](https://jsonapi.org/format/#document-member-names) | A constructed extension member containing invalid UTF-8 could register, be replacement-normalized during encoding, and then fail its own codec on decode. | `go test ./... -run 'FuzzMemberRegistry/810c9e553c1e191b'` | Fixed by rejecting invalid UTF-8 in constructed member names; minimized fuzz corpus retained. |
| Low | [HTTP semantics](https://www.rfc-editor.org/rfc/rfc9110.html#field.accept) | Floating-point parsing accepted values outside the HTTP `qvalue` grammar. | `negotiation_test.go` | Fixed with a grammar-specific parser. |
| Low | [Cursor Pagination](https://jsonapi.org/profiles/ethanresnick/cursor-pagination/#collection-sizes) | Integral JSON numbers written with a decimal point or exponent were rejected in collection-size metadata. | `cursor_meta_test.go` | Fixed with exact rational parsing and signed 64-bit bounds. |
| Low | Package correctness | Unknown core/Atomic validation contexts and negative expected-result counts silently weakened contextual checks. | `context_validation_test.go`, `atomic_test.go` | Fixed by rejecting invalid option states with stable validation violations. |
| Low | Package correctness | Registered member validators could run more than once during configured decoding. | `member_codec_defense_test.go` | Fixed; decode validators run once before core semantic validation. |

## Verification snapshot

The following local checks passed on 2026-07-14:

```text
go test ./...
go test -race ./...
go vet ./...
staticcheck 2025.1.1 ./...
golangci-lint 2.12.0 run
govulncheck 1.1.4 ./...
go test ./... -coverprofile=coverage.out
./scripts/check-coverage.sh coverage.out
go test ./... -run '^$' -bench . -benchtime=100ms
./scripts/check-docs.sh
go test ./... -run '^$' -fuzz <each configured target> -fuzztime=2s
```

Exact coverage contained no zero-count production statement. The nine fuzz
targets cover core and Atomic decode, constructed validation, member
registries, query and Cursor parsing, negotiation, Cursor metadata, and
constructed marshal/unmarshal round trips. `govulncheck` reported no known
reachable vulnerability.

## Residual and integration risks

- Callers must bound the HTTP body before allocating the `[]byte` passed to a
  codec. Package limits protect decoding, not transport buffering.
- Atomicity is only as strong as the supplied `AtomicTransaction`; a callback
  that commits before returning an error cannot be undone by this package.
- Extension/profile/cursor/sort callbacks are application code. The package
  contains panics and redacts public strings, but callbacks must still be
  bounded, concurrency-safe, and deterministic; retained causes remain
  sensitive diagnostics.
- URLs are validated as URI-reference data. Fetch policy, SSRF prevention,
  authorization, persistence, filtering, and cursor encoding belong to the
  application.
- Scheduled CI fuzzing provides continuing evidence; a short smoke run cannot
  prove the absence of all parser defects.

## Release recommendation

Prepare the dated `v1.0.0` changelog section, rerun all release gates on the
final commit, and let the configured Go 1.24/stable operating-system matrix
pass before creating the first tag. Because the public API is unreleased and
the audit included additive API and corrective wire-behavior changes, the
appropriate first stable version remains `v1.0.0` rather than a pre-existing
compatibility-line patch.
