# Troubleshooting

## `unknown-member` while decoding

The JSON pointer identifies a member not permitted in that JSON:API-defined
object. Check spelling and scope. For extension semantics, register the exact
namespaced member and decode through `Codec`; otherwise use attributes or meta.

## `duplicate-member` on apparently valid JSON

JSON parsers often silently keep the last duplicate key. This package rejects
duplicates before normal decoding. Inspect the raw payload at the reported
pointer rather than a previously decoded map.

## Validation says an included resource is not linked

Every included resource must be reachable through relationship linkage from
primary data, subject to the sparse-fieldset exception. Add the missing
identifier linkage or remove the unrelated included resource.

## A create or update document has the wrong identity requirements

Use `UnmarshalWith`/`MarshalWith` with `CreateRequest` or `UpdateRequest`.
Generic validation cannot infer endpoint intent from bytes alone.

## Content type returns 415

Confirm:

- the media type is exactly `application/vnd.api+json`;
- only `ext` and `profile` parameters are present;
- each parameter is a quoted, space-separated list of absolute URIs;
- every requested extension is registered with the negotiator.

## Accept negotiation returns 406

Every JSON:API candidate was invalid, quality zero, or requested an unsupported
extension. Add a plain JSON:API or wildcard candidate, or configure the
required extension.

## Query parser returns `unknown-parameter`

Register implementation-specific family names with `NewQueryParser` or an
extension namespace in its second argument. Lowercase unregistered names are
reserved and remain rejected.

## Cursor range requests fail

Both `page[after]` and `page[before]` form a range request. Enable
`AllowRange`, configure a finite `MaxSize`, and implement range semantics; or
return the profile's range-not-supported error.

## Cursor page validation requires null links

The profile requires both `prev` and `next` members. Use `NullLink()` when a
direction has no page; omitting the member is different from explicit null.
Set `HasPrevious` and `HasNext` to the result of the required boundary checks.
For an `after` request the previous link may be speculative, and for a
`before` request the next link may be speculative, as the profile permits.

## Atomic execution rolled back but returns two causes

`AtomicExecutionError` preserves the operation/commit failure and a rollback
failure. Inspect `Cause`, `RollbackCause`, and `Phase`; `errors.Is` and
`errors.As` can match either cause.

## Coverage prints 100.0 but CI fails

The display rounds. CI checks the raw profile for zero-count production
statements as well as the reported total. Run:

```sh
go test ./... -coverprofile=coverage.out
awk -F'[: ,]+' '$NF == 0 { print }' coverage.out
```

Any output identifies a statement that still lacks execution.

## A fuzz command matches multiple targets

Anchor the name:

```sh
go test ./... -run '^$' -fuzz '^FuzzUnmarshal$' -fuzztime=30s
```

## A callback failure hides the original message

This is intentional. Extension/profile validators and cursor/sort hooks return
a redacted `CallbackError` so application values and panic text do not enter a
client-facing error accidentally. Use `errors.Is`, `errors.As`,
`CallbackPhase`, and `CallbackPanicValue` in trusted diagnostics only.
