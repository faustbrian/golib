# Policy format and compatibility

The portable policy envelope is human-readable JSON with the format identifier
`authorization.policy/v1`. Format compatibility is governed by semantic
versioning: incompatible interpretation changes require a new format identifier
and an explicit migration path.

```json
{
  "format": "authorization.policy/v1",
  "revision": 7,
  "algorithm": "deny-overrides",
  "policies": [
    {
      "id": "documents",
      "revision": 3,
      "model": "acl",
      "priority": 10,
      "metadata": {
        "owner": "security"
      },
      "document": {
        "entries": []
      }
    }
  ]
}
```

The envelope records one monotonic snapshot revision, a combining algorithm,
and stable policy records. Each record carries its own revision, model,
priority, optional UTC activation window, review metadata, and a model document.

`policy.Decode` rejects unknown envelope fields, trailing JSON values, unknown
format versions, unsupported algorithms or models, duplicate policy IDs,
invalid revisions, malformed activation windows, and non-object documents.
`policy.Encode` validates before producing deterministic indented JSON ending in
a newline. Decode and encode reject manifests above the 16 MiB default before
an oversized policy can reach activation.

The envelope does not deserialize Go evaluators by itself. `policy.Compiler`
uses an explicit model decoder registry to validate each `document` payload and
construct an immutable snapshot. Registries are copied at compiler construction.
The compiler independently bounds policy count, each document, and aggregate
document bytes; defaults are 1,000 policies, 1 MiB per document, and 16 MiB in
aggregate.

```go
compiler, err := policy.NewCompiler(map[policy.Model]policy.Decoder{
    policy.ModelACL:  acl.Decoder{},
    policy.ModelRBAC: rbac.Decoder{},
    policy.ModelABAC: abac.Decoder{},
})
if err != nil {
    return err
}

snapshot, err := compiler.Compile(manifest)
```

A model decoder must validate its document into typed ACL, RBAC, ABAC, or
application-specific definitions before calling their constructors. Missing
decoders, oversized documents, decoder errors, and nil evaluators all fail
compilation. An object that passes envelope validation but fails model
validation is never activated.

Built-in ACL, RBAC, and ABAC documents use model document version `1`. They
reject unknown fields and trailing JSON, express effects as `"allow"` or
`"deny"`, and pass decoded definitions through the same bounded constructors
as in-memory configuration. Each decoder and encoder rejects a model document
above 1 MiB. Their public `Document` values can be validated and encoded with
each model package's `EncodeDocument` function.

```json
{
  "version": 1,
  "entries": [
    {
      "id": "document-reader",
      "subject_kind": "user",
      "subject_id": "user-1",
      "action": "read",
      "resource_type": "document",
      "effect": "allow"
    }
  ]
}
```

RBAC documents contain `roles`, `permissions`, and `assignments` arrays. Both
models accept optional `limits` and `global_inheritance`; global inheritance
remains disabled unless the document explicitly enables it.

ABAC documents contain `rules` and `named_conditions`. Conditions use a closed
operator set matching the typed Go constructors: equality, existence, null,
boolean composition, comparisons, membership, set and string operations, and
CIDR membership. Attribute values are tagged with an explicit kind and never
coerced between strings, booleans, integers, floats, timestamps, IP addresses,
or string sets.

## v1 compatibility record

The following envelope fields are part of the v1 compatibility contract:

- `format`, `revision`, `algorithm`, and `policies` at the root;
- `id`, `revision`, `model`, and `document` on every policy record; and
- optional `priority`, `active_from`, `active_until`, and `metadata` fields.

Unknown fields are rejected rather than ignored. Implementations may add new
model-document versions behind model-specific version fields, but changing the
meaning or type of an envelope field requires a new envelope format.

`policy.Repository` is the storage-neutral contract for loading and updating
manifests with an expected revision. Durable adapters must provide atomic
updates, monotonic revisions, and optimistic conflict errors without relying on
cache invalidation for correctness.
