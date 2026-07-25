# Migration notes

## From direct `encoding/json`

1. replace ad hoc response maps with `Document` and `ResourceObject`;
2. choose explicit primary/relationship data constructors;
3. map domain values to attributes and relationships;
4. replace `json.Marshal`/`json.Unmarshal` at protocol boundaries with the
   package codec;
5. map typed errors to your HTTP error policy;
6. add canonical fixtures before switching production responses.

Expect stricter behavior: duplicate members, unknown members, invalid links,
identity conflicts, and compound-document linkage errors will no longer pass
silently.

The package boundary also rejects malformed UTF-8 and applies the documented
`DefaultDecodeLimits`. Keep the defaults for ordinary endpoints, lower them for
small request contracts, or call `UnmarshalWithLimits` when a justified larger
document is required. Continue limiting the HTTP body before reading it; a
decoder limit cannot recover memory already allocated by the transport layer.

`ParseQuery` and `NewNegotiator` likewise apply `DefaultQueryLimits` and
`DefaultNegotiationLimits`. Use their `WithLimits` variants for endpoint-level
policies. Accept quality values are parsed using the HTTP grammar, so forms
that generic floating-point parsers accept—such as exponents or more than
three decimal places—are rejected.

Links are also validated in their JSON:API object scope. Top-level links use
`self`, `related`, `describedby`, or pagination names; resource links use
`self`; relationship links use `self`, `related`, or pagination names; and
error links use `about` or `type`. Register namespaced extension members with
`LinksObjectMemberScope` instead of using arbitrary core-looking names.

URI-reference values must already use RFC 3986 wire encoding. For example,
emit `page%5Bafter%5D` rather than raw brackets and `%20` rather than a raw
space. Use `net/url` construction helpers before passing a URL to `URI`,
`ObjectLink`, Atomic `href`, extension registration, or negotiation setup.
Constructed `describedby` link graphs must also be acyclic and remain within
`DefaultMaxNestingDepth`; validation now rejects graphs that previously risked
recursive stack exhaustion.

Atomic transaction callback panics are returned as `AtomicExecutionError`
wrapping `AtomicPanicError`; they are no longer re-panicked by the package.
Inspect causes with `errors.Is`/`errors.As`, and do not depend on internal cause
text being included in the public error string.

Extension/profile validators and cursor/sort hooks likewise return a redacted
`CallbackError` instead of exposing callback text or allowing a panic to cross
the package boundary. Use `errors.Is`/`errors.As`, `CallbackPhase`, and
`CallbackPanicValue` for explicit diagnostics. Applications should not expose
those inspectable causes or panic values in client responses.
Profile validators must be observational. A validator that mutates its
document argument and returns nil now produces a redacted `CallbackError`
instead of allowing the changed document through the codec.

## From reflection/tag-based JSON:API libraries

Create a small adapter per resource type rather than porting tags mechanically.
Audit:

- resource type and ID formatting;
- absent versus null versus empty collections;
- relationship linkage and links;
- included-resource deduplication;
- attribute naming and number representation;
- error pointer construction;
- query and media negotiation previously handled by middleware.

Explicit adapters are more code than tags but make protocol compatibility
visible and testable.

## Wire-format comparison

Do not compare only decoded domain values. Capture and compare:

- member presence;
- null versus omitted values;
- relationship linkage;
- included identities;
- link string versus object forms;
- exact numbers;
- extension/profile media type parameters;
- deterministic bytes where clients or caches depend on them.

## Incremental adoption

Use the package first at one endpoint boundary. Keep the old mapping behind a
feature flag or shadow comparison, build golden fixtures from accepted cases,
and monitor rejection codes. Expand only after mismatches are classified as an
old bug, a new bug, or an intentional compatibility change.

## Future release migrations

Breaking changes before v1 and all migrations after v1 are recorded in
`CHANGELOG.md`. A stable release will not remove or redefine exported APIs or
wire behavior without a new major version.
