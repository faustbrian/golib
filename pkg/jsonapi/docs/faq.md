# FAQ

## Is this an HTTP framework?

No. It models, validates, parses, and serializes JSON:API protocol values. Use
it with `net/http` or any router.

## Does it map my structs with tags?

No. Mapping is explicit by design. This avoids hidden resource identity,
relationship, omission, and compatibility behavior.

## Does it implement filtering?

It parses and preserves the `filter` parameter family. Each application owns
its filter vocabulary, authorization, and storage mapping because JSON:API
does not define a universal filtering strategy.

## Why are page and filter values not strongly typed?

Core JSON:API reserves those families but leaves semantics to implementations.
The official Cursor profile provides a typed parser for its page semantics.

## Why does decoding reject an extra member?

JSON:API-defined objects cannot contain arbitrary members. Register a genuine
namespaced extension member with `Codec`, move application data into
attributes/meta, or correct the payload. Members beginning with `@` are the
forward-compatible exception and are ignored.

## How do I distinguish omitted, null, one, and many data?

Use `nil`, `NullData`, `ResourceData`, and `ResourceCollection` for primary
data; use `nil`, `NullRelationship`, `ToOne`, and `ToMany` for linkage.

## Are numbers converted to float64?

No. Arbitrary decoded values use `json.Number`, preserving large integer and
decimal wire values across canonical round trips.

## Is output deterministic?

Yes for the same semantic document. Output ordering and presence behavior are
tested as compatibility surface. JSON object member order is not semantically
significant, but stable bytes are useful for caches and fixtures.

## Does Cursor Pagination query my database?

No. It validates profile policy and response structure. Your adapter owns
cursor encoding, unique ordering, keyset predicates, and page existence.

## Does Atomic Operations guarantee my storage is atomic?

Only when the supplied `AtomicTransaction` genuinely owns one atomic storage
transaction. The package guarantees call ordering, rollback attempts, commit
handling, and positional results around that adapter.

## Can I use custom profiles?

Yes. Register a `ProfileDefinition` with an absolute URI and optional
document-level semantic validator.

## Is the API stable?

The repository is pre-v1 until `v1.0.0` is tagged. See the
[compatibility policy](compatibility.md).
