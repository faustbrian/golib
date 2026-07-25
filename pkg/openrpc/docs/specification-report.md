# Specification and interoperability report

This report records the release audit performed on 2026-07-20. The source of
truth is OpenRPC 1.4.1 at commit
`3a13c7a8bad248e6edd2d48339cd1c06b57f8f22`; `specification/manifest.json`
pins every copied input and its SHA-256 digest.

## Supported versions

| Input | Verdict | Reason |
| --- | --- | --- |
| Canonical `1.4.0` and any canonical non-negative `1.4` patch | accepted | Patch releases share the inventoried 1.4 feature set |
| Earlier `1.x`, future minor or major lines | rejected | Their semantics have not been inventoried |
| Prerelease, build metadata, leading-zero, or malformed values | rejected | The supported field is a canonical release version |

`SupportedVersions` reports only `1.4.x`. Strict and preserving parsing share
this version boundary, so preserving mode cannot reinterpret a future feature
line as 1.4.

## Requirement and object audit

The generated normative matrix contains 49 statements: 41 have direct
implementation and executable evidence, and eight are explicitly
not-applicable because this module has no implicit file naming, renderer, or
JSON-RPC request runtime. The object-field matrix contains all 75 fields from
the pinned meta-schema. Every row records shape, requiredness, nullability,
default, extension and unknown-field behavior, implementation, and executable
evidence. The conformance gate rejects missing, duplicate, stale, or
undisposed rows.

## Meta-schema alignment

The exact upstream schema and its `meta.json-schema.tools` companion are
embedded and verified by the manifest before use. Validation is offline. The
companion's dialect URI is normalized to Draft 7, which is the dialect required
by OpenRPC 1.4. One upstream contradiction is corrected during compilation:
the generic absolute-URI format on Server Object `url` is removed because the
normative text permits relative URLs and server-variable templates. Dedicated
semantic validation still checks the expression and its variable bindings.

The structural meta-schema runs through the pinned
`santhosh-tekuri/jsonschema` implementation. The separately implemented typed
parser and semantic validator provide the differential layer; acceptance is
not inferred from either layer alone.

## Corpus verdicts

The official examples repository is pinned independently at commit
`dce69463ba9a3ca2232506b734606fa97f25dd45`. Its eight service descriptions
span empty, link, metrics, named-parameter, expanded, example-rich, petstore,
and arithmetic APIs. Every source document declares an older or otherwise
unsupported feature line. Both the independent meta-schema validator and the
typed parser reject each document specifically at `openrpc`; the fixtures are
not silently relabeled as 1.4.1.

The complete current-version fixture exercises every OpenRPC object and schema
placement. It passes strict parsing, the pinned meta-schema, semantic
validation, canonical determinism, preserving round trips, removal tests, and
explicit-null tests. Boolean and object Draft 7 schemas additionally have
focused composition, recursion, format, annotation, and hostile-input tests.

The public ecosystem reviewed on 2026-07-20 still predominantly publishes
older OpenRPC feature lines, including the current official example corpus.
Those inputs are useful rejection evidence but cannot prove 1.4 acceptance.
New external 1.4 documents must be pinned with source, license, digest, and
expected verdict before joining the blocking corpus.

## Commands

```sh
make conformance
go test ./parse ./validate -count=1
make integration
```

Together these gates verify generated matrices, official-corpus verdicts,
current-version structural and semantic validation, and `rpc.discover`
interoperability with `jsonrpc`.
