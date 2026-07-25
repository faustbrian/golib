# openapi

`openapi` is a version-aware OpenAPI document toolkit for Go. It provides
immutable lossless semantic values, bounded JSON and YAML parsing, generated
typed views for Swagger 2.0 and OpenAPI 3.0 through 3.2, normative validation,
OpenAPI JSON Schema dialects, explicit reference resolution, composition,
conversion, compatibility diffing, and bounded deterministic serialization.

The 2026-07-22 audit reconciled reviewed implementation, test, and
documentation links for all 2,381 extracted normative requirements. Production
statement coverage is 100%, and the adversarial, mutation, interoperability,
performance, security, and supply-chain gates are executable release evidence.
Those results are necessary gates, not proof by aggregate score. See
[`specification/conformance/evidence.tsv`](specification/conformance/evidence.tsv)
for clause-level status and [`docs/audit-report.md`](docs/audit-report.md) for
the evidence-backed audit outcome.

## Supported document lines

The root decoder selects exact declared versions and returns the corresponding
immutable typed view:

- Swagger 2.0;
- OpenAPI 3.0.0 through 3.0.4;
- OpenAPI 3.1.0 through 3.1.2; and
- OpenAPI 3.2.0.

Future patch or minor versions are rejected rather than interpreted as the
latest known version. Unknown fields and `x-` extensions remain in the lossless
raw semantic value. Typed field accessors distinguish absent, explicit `null`,
valid, and invalid values.

Patch conversion within a supported minor line changes only the declared
version marker. Cross-minor conversion is explicit and loss-aware. The
specification's minor-version compatibility requirements constrain future
specification authors; they are not treated as permission to guess the meaning
of an unsupported future version.

## Parse and validate

Parsing requires an explicit context and limits. It performs no implicit I/O.
Every limit must be positive, and `MaxBytes` must be below `math.MaxInt64` so
the parser can reserve a non-overflowing byte for limit detection.
Strict YAML input accepts only JSON-compatible scalar tags and string mapping
keys; custom tags, non-string keys, anchors, aliases, and merge keys are
rejected with stable bounded errors.
Strict JSON rejects unpaired UTF-16 surrogate escapes instead of silently
replacing them, so member names and string values retain interoperable Unicode
identity.
OpenAPI 3.x URI and URL fields accept valid relative references unless their
field-specific rule requires an absolute value; Swagger 2.0 retains its legacy
absolute-URL requirements.
Current URI handling follows RFC 3986. Applications that need historical URL
interoperability should additionally apply RFC 1738's scheme and character
rules rather than assuming every RFC 3986 identifier is accepted by older
tooling.

The package does not emulate undefined historical behavior. Ambiguous inputs
are rejected or reported, and implementation-defined choices are documented
at their APIs. Parsers consume exactly one complete JSON or YAML value. A
reference resolver returns a complete semantic resource root, after which a
pointer or anchor is resolved; there is no fragment-only parsing mode that can
miss base-URI-changing keywords. Arbitrary JSON or YAML root values can be
parsed for caller-defined embedding formats, but extraction and the embedded
Object type remain the embedding format's responsibility.

```go
limits := parse.DefaultLimits()
document, err := openapi.ParseYAML(ctx, reader, limits)
if err != nil {
	return err
}

validator := validate.NewValidator()
report, err := validator.Document(ctx, document)
if err != nil {
	return err
}
for _, diagnostic := range report.Diagnostics() {
	fmt.Printf("%s %s: %s\n",
		diagnostic.Severity,
		diagnostic.InstanceLocation,
		diagnostic.Code,
	)
}
```

The default validator uses the canonical JSON Schema engine's conservative
resource limits for official data-model validation. For a reviewed large
description, construct an isolated cache with
`validate.NewValidatorWithDocumentSchemaLimits`, starting from the canonical
engine's defaults and increasing only the measured limiting dimension.

Runtime values can be checked independently with explicit direction semantics:

```go
report, err := validate.SchemaInstance(
	ctx,
	document,
	schema,
	value,
	validate.InstanceOptions{Direction: validate.DirectionRequest},
)
```

Schema instance validation applies the selected OpenAPI Schema Object dialect,
rejects Swagger `readOnly` properties in requests while permitting them in
responses, and validates Swagger discriminator values against the named schema
and its `allOf` descendants. For OpenAPI 3.x, read-only request values and
write-only response values produce warnings, and `required` applies only in
the relevant direction. Direction traversal is bounded by `MaxNodes` and
performs no implicit reference I/O. A root internal Schema Object reference
is resolved from the supplied document before compilation, including boolean
Schema Objects in OpenAPI 3.1 and later.

For OpenAPI 3.2 sequential media, `validate.SequentialMediaInstance` accepts
already-decoded ordered items. It applies `schema` to the complete array and
`itemSchema` independently to each item; both fields may be used together.
The item count and direction-sensitive traversal are bounded by `MaxNodes`.
`media.ParseServerSentEvents` performs bounded WHATWG `text/event-stream`
parsing before validation: it handles UTF-8 replacement and a leading BOM,
all three line endings, comments, multiline data, literal field names,
persistent event IDs, numeric retry values, ignored fields, and incomplete
events at end of input. It returns ordered JSON objects suitable for the
sequential-media validator.
For unencoded binary streams, `validate.BinaryMediaLength` compares a
caller-supplied octet count with the Schema Object `maxLength` using exact,
unbounded integer arithmetic. This permits incremental transports to enforce
the limit without retaining the payload.
`validate.BinaryMediaLengthWithOptions` follows every reachable `$ref` and
`allOf` branch under explicit node, depth, retrieval-URI, and resolver policy,
including legal schema cycles and authorized external schemas. OpenAPI 3.0
Reference Object siblings remain ignored while 3.1 and 3.2 schema siblings
remain active.
For mixed multipart payloads, `validate.MultipartBinaryMediaLengths` partitions
named `properties` and positional `prefixItems`, `items`, and `itemSchema`
subschemas under the same bounded reference policy, then applies raw binary
`maxLength` constraints to each caller-supplied part length.
OpenAPI 3.2 positional multipart encodings are rejected unless `itemSchema` or
an inline, internal, or explicitly resolved external array-applicable `schema`
is present.
`media.PositionalEncodings` then maps `prefixEncoding` entries to matching
item positions, ignores unused prefix entries for shorter instances, and uses
`itemEncoding` for every remaining item under an explicit item-count bound.
Encoding fields outside their version-specific media-type contexts produce
non-fatal ignored-field warnings rather than invalidating the description,
including reusable Media Type Objects evaluated at each content-map use.
For name-value media, `media.NamedEncodingValues` takes caller-resolved Schema
Object properties and maps Encoding Objects to bounded instance values. It
ignores unknown encoding names, expands top-level object array properties into
repeated values with the same name, and preserves nested arrays inside a
top-level array as single values.
Each result retains the encoding key as its form parameter name. For
`application/x-www-form-urlencoded`,
`media.SerializeNamedEncodingValues` composes this mapping with Encoding
Object defaults and overrides to produce a bounded body while preserving
repeated parameter names. For
`multipart/form-data`, `media.MultipartFormDataDisposition` produces a bounded
`form-data; name="..."` header value, escaping quoted-string characters and
percent-encoding non-ASCII or control octets without permitting header
injection.
Inline and reusable OpenAPI 3.2 positional `multipart/form-data` encodings must
also declare a case-insensitive `Content-Disposition` entry in their Encoding
Object headers; the formatter supplies its required `form-data` name value.
OpenAPI 3.2 named Encoding Objects are also supported for non-`form-data`
multipart media types when their keys match resolved schema properties.
`media.SelectEncodingContentType` applies the clarified OpenAPI 3.1 and 3.2
type defaults and
requires the application to choose a concrete match whenever `contentType`
contains multiple or wildcard alternatives. It does not inspect or sniff the
payload, so untrusted bytes cannot silently select their own representation.
`media.EncodingHeaders` exposes bounded multipart header descriptors while
always removing `Content-Type` case-insensitively; for non-multipart media it
returns no headers because the entire field is ignored.
`media.ApplyEncoding` combines content-type selection, multipart headers, and
optional RFC6570-style serialization in one bounded result. It retains the
complete Encoding Object and each nested named, prefix, and item encoding value
so the same operation can be applied recursively after child-value mapping.
`media.SerializeEncodingForVersion` applies explicit OpenAPI 3.x `style`,
`explode`, and `allowReserved` fields with patch-correct query-style defaults
and no leading `?`. OpenAPI 3.0 applies them only to form-urlencoded content
and reports them as ignored for every other media type; OpenAPI 3.1 and 3.2
also apply them to multipart form-data. `media.SerializeEncoding` is the
OpenAPI 3.2 convenience form.
Form-urlencoded results use WHATWG space-as-plus encoding. Multipart form-data
results contain no URI percent-encoding, and `allowReserved` has no effect.
The function reports when these fields are absent or ignored so callers can
apply content-type serialization instead.
The named and positional mapping functions accept the identically shaped
fields on an Encoding Object as well as a Media Type Object. This provides the
required first nested encoding level; callers can deliberately compose further
levels under the same explicit value bounds. OpenAPI 3.1.1, 3.1.2, and 3.2
documents receive an interoperability warning when an encoded property uses
the non-recommended `contentMediaType` keyword.
After a caller serializes complex values to strings, `media.FormURLEncode`
produces the final bounded `application/x-www-form-urlencoded` body using the
WHATWG space-as-plus and percent-encode rules. It preserves input order and
repeated field names.
`media.MultipartFormDataEncode` writes bounded multipart bodies with a
caller-owned boundary, deterministic part ordering, repeated names, explicit
content types, and validated additional headers. It never sniffs content.
`media.MultipartMixedEncode` applies the same bounded named-part convention to
`multipart/mixed`, using `Content-Disposition: form-data` with a `name`
parameter as optionally permitted by OpenAPI 3.0 and 3.1.
Authors using RFC 6570-based `multipart/form-data` serialization should review
RFC 7578's character-set and encoding guidance before selecting that form.
Encoding content-type selection deliberately ignores a Schema Object's
`contentMediaType`, including when the two values disagree.
For OpenAPI 3.2 non-JSON serialization, `media.SelectSerializationDataType`
uses caller-supplied validated runtime data whenever available. Without runtime
data, it performs bounded, cancellation-aware schema inspection through only
`$ref` and `allOf`, with optional explicit external resolution. A schema without
`type` reports `any`; incompatible or genuinely ambiguous type constraints
return stable errors. The selector deliberately does not infer a choice from
`oneOf`, `anyOf`, or `$dynamicRef`, so ambiguous applicators cannot silently
select a serialization strategy.

The package exposes specification data and serialized forms. OpenAPI 3.2's
optional application form, including class hierarchies or other information
beyond the specification, remains an explicit application-owned enrichment so
the core model does not invent semantics.

`validate.Validator` owns its compiled official-schema cache, is safe for
concurrent use, and performs no background work. Package-level validation
functions create isolated one-shot validators. `DocumentWithOptions` supports
deterministic fail-fast behavior plus independent document node, nesting-depth,
reference, and diagnostic count limits. Document traversal rejects wide values
before copying child collections beyond the remaining node or depth budget.
Structural findings come from pinned official schemas. Internal reference
targets are checked with bounded source-order traversal.
Parameter-list identity checks resolve inline, internal, and explicitly
authorized external Parameter and Path Item references before comparing the
location and name. Duplicate path-level and operation-level parameters are
reported at their use sites.
The official schemas are supporting evidence only: independently evaluated
prose-derived diagnostics remain authoritative when schema evaluation passes.
External OpenAPI references remain unresolved unless `ReferenceResolver` and
the document's `ReferenceResourceURI` are explicitly supplied.
When a retrieval URI is supplied, OpenAPI entry documents not named
`openapi.json` or `openapi.yaml` produce an interoperability warning.
When supplied, one call-local cache is shared by reference target checks and
path-parameter semantics, so external parameter names and required flags are
validated without duplicate fetches.
Operation identifier uniqueness also includes operations reached through
authorized external Path Item and Callback references.
Prose-derived passes currently add stable findings for path field names and
templates, parameters, headers, request bodies, response-code keys,
deprecated operations, parameters, headers, OpenAPI 3.0 schemas, and OpenAPI
3.2 security schemes,
and, where required by the selected patch revision, warnings for exact status
codes absent from the pinned IANA HTTP Status Code Registry. They also cover
media-type encodings, example
value-source and singular/named-field exclusivity, Schema and XML Object
constraints, Swagger required read-only properties, ignored standard header
parameters, which are excluded from every semantic consumer, component names,
metadata URIs and email addresses, ignored
Content-Type Header Objects, and encoding headers outside multipart media
types. Encoding names are matched against properties collected through bounded
Schema Object composition and authorized external references. Multipart request
media types through OpenAPI 3.1 require a Schema
Object. OpenAPI 3.1 and 3.2 Encoding Objects
also report contentType values overridden by explicit serialization fields,
while OpenAPI 3.0 reports serialization fields outside form-urlencoded media,
and GET, HEAD, or DELETE request bodies report undefined method semantics,
while OpenAPI 3.0 semantic consumers exclude request bodies from methods that
do not define body semantics,
root document surfaces and dialect URIs, servers, security connections,
scheme identifiers, URL references, and URI-like OpenAPI 3.2 security
component names. HTTP authentication scheme names absent from the pinned IANA
registry produce recommendation diagnostics. Operation IDs outside the
portable ASCII identifier convention `[A-Za-z_][A-Za-z0-9_]*` likewise produce
interoperability warnings. Revisions that discourage Schema Object
serialization for cookie parameters recommend `text/plain` content instead.
Validation also covers operation IDs and Link
Object target parameters. Ambiguous unqualified Link parameter
names are rejected, while location-qualified names take precedence. Validation
follows authorized external Path Item and Callback references when checking
Link runtime expressions against their parent operation parameters. Validation
also covers tags,
external-documentation URLs, and Swagger host, base-path, scheme, media-type,
operation-summary length, file-upload, payload-parameter declarations, and
allowed parameter, item, and response-header types. Swagger response example
media types are checked against effective root or operation `produces`
declarations for inline, internal, and authorized external responses.
Compilable response examples are checked against their response schemas,
including responses reached through authorized external references. File
response schemas recommend an effective `produces` declaration, and Swagger
XML metadata is restricted to direct property schemas. OpenAPI 3.x parameter,
header, and media-type examples with compilable inline, root internal, or
explicitly resolved external schemas produce warnings when their values do not
satisfy those schemas.
Form-urlencoded media examples across every supported OpenAPI 3.x revision
also reject a leading query delimiter. Other serialization formats and
external Example Object references can be checked through an explicitly
configured `validate.MediaTypeExampleCodecResolver`. The selected codec sees
the complete Media Type Object, encodes data-form examples, and decodes
serialized or retrieved examples before schema and semantic comparison. Codec
output and retrieved input share the configured example byte bound; validation
performs no implicit retrieval.

For OpenAPI 3.2 JSON media, `serializedValue` strings are parsed with bounded
duplicate-key-safe JSON rules, checked against their associated schema, and
compared semantically with a paired `dataValue` when present. Object member
order and equivalent JSON number spellings do not create false mismatches.
OpenAPI 3.2 `externalValue` fields are syntax-checked as URI references without
performing implicit retrieval. For every OpenAPI 3.x revision, callers can opt
into bounded JSON example retrieval through `validate.ExternalExampleResolver`;
relative identifiers resolve against the explicit document retrieval URI.
Retrieved JSON examples are parsed with the same strict rules and checked
against their associated schema. OpenAPI 3.2 examples are also compared with a
paired `dataValue`. Explicit media-type codecs apply the same checks to
non-JSON representations.
OpenAPI 3.2 `value` and shorthand `example` usage produces an interoperability
warning for parameter, header, and non-JSON media targets. JSON and `+json`
media remain safe, as does `text/plain` with a resolved string schema.
After any application-owned resolver retrieves a non-JSON external example,
`media.ExternalExampleText` applies explicit byte bounds and defaults an
otherwise undetermined character set to UTF-8. Explicit UTF-8 and US-ASCII are
supported; other character sets require an application transcoder.

## Typed views and lossless values

Rich-text fields are exposed as opaque source strings. CommonMark syntax and
extensions are preserved byte-for-byte through the JSON semantic model, but
this package does not render rich text or define an extension dialect.

The selected root value can be type-asserted to its version package:

```go
document, err := openapi.ParseJSON(ctx, reader, parse.DefaultLimits())
if err != nil {
	return err
}

oas, ok := document.(oas32.Document)
if !ok {
	return fmt.Errorf("expected OpenAPI 3.2")
}
info, ok := oas.Info().Value()
if !ok {
	return fmt.Errorf("missing info")
}
title, ok := info.Title().Value()
if !ok {
	return fmt.Errorf("missing title")
}
fmt.Println(title)
```

Every generated object exposes `Raw() jsonvalue.Value`. Raw values retain exact
number spelling and source member order, copy caller-owned slices on
construction, and return defensive copies for arrays and objects.

## OpenAPI Schema Objects

The `jsonschema` package uses `json-schema` as its canonical evaluation
engine. The pinned Swagger 2.0 and OpenAPI 3.0 Schema Object definitions and
OpenAPI 3.1 and 3.2 vocabulary meta-schemas are registered without global
mutable state. Swagger 2.0 and OpenAPI 3.0 schemas retain their Draft 4 numeric
behavior and enforce typed defaults. OpenAPI 3.0 additionally enforces array
item declarations and translates `nullable` at every Schema Object position
before instance evaluation. Its schema documentation fields, including
discriminator, read/write direction, XML metadata, external documentation,
examples, and deprecation, are accepted without narrowing the Schema Object.
OpenAPI 3.0 `pattern` values are compiled with ECMAScript regular-expression
syntax rather than Go's RE2 syntax.

`jsonschema.RecognizeAnnotatedEnum` optionally recognizes `oneOf` or `anyOf`
arrays whose object alternatives contain a `const` and only the `title` and
`description` annotations. It preserves every `const` as a lossless JSON value,
preserves annotation absence, and applies a caller-configurable case limit.
Alternatives with other keywords are deliberately left to ordinary JSON Schema
evaluation instead of being classified as annotated enum cases.

`jsonschema.NeedsExplicitDialect` makes the interoperability recommendation
for standalone Schema Objects and objects extracted from incomplete OpenAPI
documents machine-checkable. It reports a missing `$schema`, validates an
explicit declaration as an absolute fragment-free URI, and reports that a
boolean schema needs its dialect carried out of band. Schema Objects embedded
in a complete OpenAPI document inherit that document's dialect selection.

```go
compiler, err := jsonschema.NewCompilerForDocument(document)
if err != nil {
	return err
}
compiled, err := compiler.Compile(ctx, schemaObject.Raw())
if err != nil {
	return err
}
result, err := compiled.Validate(ctx, instanceJSON)
```

Compiler traversal defaults to one million semantic nodes and 256 nesting
levels. `jsonschema.WithTraversalLimits` selects stricter positive bounds;
wide or deep schemas are rejected before child collections are copied.
Each compiler owns its dialect engine cache. Concurrent first use of the same
pinned or default dialect shares one load and compilation attempt; failed
attempts are not retained.

Callers compiling embedded Schema Objects with relative references can pass
`jsonschema.WithBaseURI` with the document's absolute retrieval URI. The
compiler uses the nearest schema `$id` when present, otherwise that retrieval
URI; OpenAPI 3.2 `$self` overrides or resolves against the supplied URI.

When `jsonSchemaDialect` or a Schema Object `$schema` selects an external
dialect, the caller must provide an explicit resource loader. No network or
file loading is enabled by default. OpenAPI 3.1 documents use the OpenAPI 3.1
base dialect by default, while OpenAPI 3.2 documents use the pinned OpenAPI 3.2
dialect. A root `jsonSchemaDialect` or resource-root `$schema` overrides that
default. Standalone Swagger 2.0 and OpenAPI 3.0 schema resources returned by a
loader are validated against the same pinned subset before compilation, and
OpenAPI 3.0 `nullable` translation applies across the resource boundary.
Compiler errors preserve `errors.Is` classification but do not echo dialect,
base-URI, vocabulary, keyword, or resource-loader text.

## References

The `reference` package separates requested, retrieval, and canonical resource
identity. Internal JSON Pointers and anchors require no resolver. A reference
that leaves the current resource fails with
`reference.ErrExternalResolutionDisabled` unless the caller supplies a
resolver.

`reference.ParseObject` validates the required `$ref` URI-reference string
without performing I/O and preserves every sibling field for later
version-specific processing.

Descriptions split across multiple documents are supported through explicit
resolvers. Retrieval always produces a complete resource before its fragment
is evaluated. A `$ref`-shaped value inside Schema Object example data is
preserved losslessly and may be resolved explicitly by an application, but it
is not reclassified automatically as an OpenAPI Reference Object.

OpenAPI 3.2 `$self` establishes the canonical base URI. The separately supplied
retrieval URI remains an accepted alias for that same in-memory resource, but
descriptions should not rely on the alias because other tools are not required
to support it when `$self` is present.

```go
target, err := reference.Resolve(
	ctx,
	reference.Resource{
		RetrievalURI: documentURI,
		Root:         document.Raw(),
	},
	"#/components/schemas/Pet",
	nil,
	reference.DefaultLimits(),
)
```

Resolution failures do not echo base or requested resource identifiers or
anchor names.
Failures returned by an explicit resolver remain classifiable with `errors.Is`
alongside `reference.ErrResourceAccess`, while their messages are withheld.

Consecutive Reference Objects can be followed with `reference.ResolveChain`.
The returned immutable chain preserves every target's resource identity and
reports legal cycles without recursing indefinitely. `MaxReferenceDepth`
bounds acyclic chains independently from in-resource pointer traversal.
Pointer token depth is rejected before token allocation or escape decoding
beyond the configured traversal budget.
Anchor, reference-inventory, bundling, and dereferencing traversal check child
cardinality against remaining node and depth budgets before copying composite
children.
`reference.Scan` provides a bounded, source-ordered inventory of every `$ref`
member without resolving it or performing I/O. `reference.ResolveAll` connects
that inventory to immutable target chains under one explicit resolver policy.
`reference.ScanFiltered` lets typed consumers select semantic reference
positions before interpreting their values, so reference-shaped application
data does not trigger resolution or invalidate the rest of an inventory.

`reference.BundleComponents` localizes external reference graphs into
version-appropriate OpenAPI component or Swagger reusable-object registries.
It resolves only through the supplied resolver, deduplicates targets,
preserves cycles as internal references, rewrites references back to the base
resource, and appends imported values in deterministic discovery order.
Existing names are never overwritten; collisions receive bounded `_bundled`
names and every rewrite retains resource and pointer provenance.

Registry targets retain their declared type and name. Arbitrary pointer,
anchor, and root targets are also supported when the reference's source
position unambiguously determines the reusable-object registry; derived names
are sanitized and collision-safe. Bundling rejects mismatched registries and
locations where neither side establishes the target type. It remains distinct
from full dereferencing and does not inline Schema Object references. `$ref`
members inside examples, Schema annotation values, Link request bodies, and
extensions remain ordinary application data and never trigger resolution.

`reference.DereferenceObjects` instead replaces OpenAPI Reference Objects with
arbitrary internal or explicitly resolved external object targets. Traversal,
depth, and reference counts are independently bounded, external resources are
cached for one call, and every replacement records source and target
provenance. It rejects direct Reference Object cycles, non-object targets, and
incompatible target types. OpenAPI 3.1 and 3.2 `summary` and `description`
siblings override only target object types that define those fields; prohibited
extra fields are ignored. Ambiguous Path Item siblings and malformed overlays
are rejected. Schema Object `$ref` keywords stay intact because inlining them
can change JSON Schema base URI and dynamic-scope semantics.

Resolvers are policy boundaries. A network or file adapter must enforce its
own scheme, host, path, redirect, byte, duration, concurrency, and credential
policy. `reference.NewFileResolver` is an optional, deny-by-default local-file
adapter. It requires explicit directory roots, resolves symlinks before its
root-containment check, accepts only `file:` URIs ending in `.json`, `.yaml`,
or `.yml`, and enforces caller-selected byte, document, and parser limits. Its
document budget is cumulative, so callers should construct one resolver per
bounded operation and call `Close` afterward. Root handles use Go's
traversal-resistant filesystem API, so path or symlink swaps cannot escape the
opened directory tree on supported platforms. Filesystem operation failures
return `reference.ErrResourceAccess` without exposing resource paths or
underlying operating-system messages.

`reference.NewHTTPResolver` is the corresponding deny-by-default network
adapter. Exact schemes, IDNA-normalized hosts, ports, and any private-address
exceptions must be explicit. Its owned transport disables environment proxies
and transparent decompression, pins each connection to addresses checked after
DNS resolution, rejects a host if any returned address is unauthorized, checks
every redirect, strips credential headers, and bounds addresses, headers,
redirects, bytes, documents, concurrency, parsing, and duration. Queries,
userinfo, fragments, wildcard hosts, compressed responses, and unknown formats
are rejected. IANA special-purpose IPv4 and IPv6 ranges are denied unless an
exact caller-selected CIDR exception authorizes them. Construct one resolver
per bounded operation and call
`CloseIdleConnections` afterward. No remote access occurs unless this resolver
is explicitly passed to a reference operation. An explicit HTTP `Content-Type`
is authoritative:
unsupported media types are rejected even when the resource path has a JSON or
YAML extension. Extensions select a parser only when `Content-Type` is absent.
Request-construction, DNS, dial, and transport failures return
`reference.ErrResourceAccess` without exposing the requested path or the
underlying transport message. Context cancellation remains directly
classifiable.

## Implicit connections

The `implicit` package resolves caller-owned, already-parsed documents without
performing I/O. `ResolveComponent` and `ResolveTag` use the entry document by
default, including Security Scheme names referenced from another document; an
explicit option can select current-document behavior when compatibility
requires it. `ResolveOperationID` considers operations from every supplied
document, including paths, webhooks, inline callbacks, and component callbacks,
and rejects ambiguous identifiers. All lookups retain the source document URI
and JSON Pointer and enforce independent document, name, operation, and nesting
limits.

When a URI-based alternative exists, authors should prefer it for portable
multi-document descriptions. `operationRef` is validated as the explicit
alternative to Link Object `operationId`, and discriminator mapping values
retain whether they are explicit URI references or implicit schema names.

## Serialization

`serialize.JSON` and `serialize.YAML` accept any immutable source exposing
`Raw() jsonvalue.Value`. Both enforce byte, nesting-depth, semantic-node, and
cancellation limits. They reject wide values before copying child collections
beyond the remaining semantic-node or depth budget.

```go
options := serialize.DefaultOptions()
options.Mode = serialize.Canonical
if err := serialize.JSON(ctx, writer, document, options); err != nil {
	return err
}
```

Preserving mode retains semantic member order and exact numbers. It cannot
restore comments, YAML anchors, aliases, or scalar styles because the current
semantic parser does not retain them. Canonical mode sorts object and mapping
keys by UTF-8 byte order recursively. That ordering is a package policy for
reproducible output, not an OpenAPI canonicalization standard.

Direct `jsonvalue.Value.MarshalJSON` calls also enforce conservative fixed
defaults. Use `MarshalJSONWithLimits` when the caller needs tighter byte,
nesting-depth, or semantic-node bounds; invalid or zero limits are rejected.

## XML null handling

`xmlvalue.PlanNull` maps a null element to an empty element carrying
`xsi:nil="true"` and maps a null attribute to omission. Because an omitted XML
attribute cannot distinguish null from a missing property, attribute properties
should be optional instead of nullable for interoperable descriptions.
`xmlvalue.RestoreOmittedAttribute` performs the reverse decision against an
already-compiled property Schema Object: it adds a lossless JSON null only when
that schema accepts null, otherwise it leaves the property absent. The helper
performs no schema loading or XML I/O and preserves existing object order.

## Runtime expressions

`expression.Parse` implements the pinned runtime-expression ABNF, including
HTTP header tokens and body JSON Pointers. `expression.ParseTemplate` splits
callback and link strings into immutable literal and dynamic parts. Parsed
expressions evaluate against typed caller-owned request and response values
without mutating them, preserving referenced JSON types. Template evaluation
uses deterministic JSON text for non-string values and bounds rendered output.

## Server URL expansion

`server.Expand` substitutes Server Object variable defaults and explicit caller
overrides without mutating inputs or applying implicit escaping. It rejects
malformed templates, missing declarations, unused overrides, invalid UTF-8,
and configurable output or variable limits. Callers remain responsible for any
percent-encoding required by their transport context.
`server.ResolveReference` then resolves relative API URLs against an expanded
absolute or relative Server Object URL using RFC 3986. Server queries and
fragments are rejected, absolute references remain absolute, and both input
and output sizes are bounded without performing network access.

## Semantic operation diff

`diff.Operations` compares paths, top-level webhooks, and their dialect-defined
operations, including OpenAPI 3.2 additional operations. For common operations
it also compares operation identifiers, request-body presence and requiredness,
effective path and operation parameters, accepted request media types,
parameter requiredness and effective serialization settings, parameter schema
and content changes, parameter defaults and examples, response keys, produced
response media types, and schemas on common request and response media types.
Response headers are compared
case-insensitively, including resolved local Header Object references. Swagger
response schemas, headers, and media-type examples are compared directly.
Effective operation security is compared after root inheritance and operation
overrides; requirement and scope
ordering is semantic, and an empty requirement object is treated as an
anonymous alternative. Effective OpenAPI 3 server contracts follow root, path,
and operation precedence. Server URL additions and removals, default-server
ordering, and variable changes are reported separately. OpenAPI callback names,
runtime-expression keys, callback operation presence, and changed unresolved
external references are compared. Callback extensions are reported separately
instead of being treated as runtime expressions.
Operations shared by callback Path Items receive the same parameter, request,
response, security, server, tag, and nested-callback comparison as top-level
operations.
Response link names and resolved local contract changes are also compared,
with changed unresolved external Link Object references remaining unknown.
Encoding Object changes on common media types are breaking unless malformed
input makes their impact unknown; example changes remain explicitly
conditional. Swagger scheme sets,
host, and effective basePath are compared at the document endpoint boundary,
while an absent host or scheme list remains document-relative and therefore
conditional when made explicit. Header parameter identities are
case-insensitive and operation parameters override matching path-level
parameters. Absent style, explode, allowReserved, and Swagger collectionFormat
values are compared using their specification defaults. Swagger
`allowEmptyValue`, defaults, enums, and every primitive validation facet are
included in parameter comparison. Swagger consumes and
produces sets follow operation-over-root inheritance; adding or removing an
explicit media type is classified separately, while making a relative set
explicit remains conditional. Document Tag Object contracts and operation tag
sets are compared without treating ordering or duplicate operation tags as
changes. Extension changes on document, path, operation, parameter, body,
response, media type, callback, link, header, security-scheme, and tag surfaces
are reported independently. Their default classification is conditional;
`ExtensionClassification` supplies an explicit caller policy. The immutable
report retains escaped JSON Pointers and emits additive, compatible,
conditionally compatible, breaking, or unknown classifications in stable
source order.

Operation identifiers and response-set changes remain explicitly conditional;
changed unresolved external Reference Objects remain unknown. Callers may set
separate `LeftResourceURI`, `RightResourceURI`, `LeftResolver`, and
`RightResolver` options to authorize and compare external reference targets
without conflating the two revisions. Resolver failures are returned to
the caller. Comparison is bounded by change count, semantic node count,
document depth, and reference-chain depth, supports patch versions within one
specification dialect, and rejects cross-dialect comparisons. Input and
resolved-reference traversal reject wide values before copying child
collections beyond the remaining semantic-node or depth budget.

The diff does not infer compatibility from source text. Adding effective
authentication is breaking, removing it is compatible, and changing required
schemes or scopes is conditional. Security-scheme registries are compared for
additions, removals, inline changes, and changed unresolved references across
Swagger and OpenAPI 3. Definition changes remain conditional and unresolved
reference changes remain unknown because authentication compatibility depends
on deployment policy. Parameter schema and content changes remain unknown
unless equal semantics can be established; general request/response schema
subsumption is not guessed. Internal Reference Object chains are resolved for
path items, parameters, request bodies, responses, callbacks, links, and
request or response schemas while report pointers retain the usage location.
OpenAPI 3.1 and 3.2 Reference Object summary and description overrides are
applied during comparison. Cyclic, invalid, and unauthorized external
references remain explicitly unknown when their source identity changes.
Schema presence and the
boolean-schema extremes are classified with distinct request and response
direction; other schema changes remain unknown.

## Operation filtering

`compose.FilterOperations` creates a new immutable document while retaining
only operations accepted by an explicit caller predicate. It traverses every
operation-bearing surface defined by the selected dialect: paths, webhooks,
component path items, and callbacks, including OpenAPI 3.2 additional
operations. The result retains source ordering, unknown fields, extensions,
and immutable provenance for each removed operation.

Filtering is bounded by operation count and nesting depth, observes
cancellation throughout traversal, and never mutates the source. Reference
targets are not resolved implicitly; local path-item siblings are filtered,
while referenced callback contents require an explicit resolution or bundling
step.

## Document merge

`compose.Merge` combines exact-version documents without mutating them. Path,
webhook, Swagger reusable-object, and OpenAPI component registries are unioned
in stable document and member order. Equivalent objects may use different
source member order; exact number spelling and array order remain significant.

Every non-equivalent collision is rejected by default with its escaped target
pointer and both source document indexes. A caller may explicitly retain the
existing value or use the incoming value through a conflict resolver. The
result records immutable source and target provenance for accepted decisions.
Independent document, entry, semantic-node, and nesting-depth limits bound
work without recursively walking arbitrary input values. Merge rejects wide
values before copying child collections that exceed the remaining semantic-node
or nesting-depth budget.

For OpenAPI 3.x component collisions, an explicit `RenameIncoming` decision
selects the first available deterministic name or a caller-selected name.
Merge validates the target name, rejects secondary collisions, and rewrites
exact and nested internal JSON Pointer references throughout that incoming
document. External references remain unchanged. Source provenance continues
to point to the original component name.

Merge currently requires the same exact specification revision. Legacy
Swagger definitions and non-component collisions cannot be renamed; callers
must reject, retain, or replace them explicitly.

## Version conversion

`convert.To` performs bounded, immutable, directional migrations. Patch
versions within an OpenAPI line preserve every value and replace only the root
version marker. Swagger 2.0 documents can be upgraded to OpenAPI 3.0, 3.1, or
3.2. The migration translates hosts and schemes to servers, definitions and
security definitions to components, body and form parameters to request
bodies, reusable parameters and responses, media types, response headers,
collection formats, schema references, file payloads, and discriminators.
Unsupported source combinations, inferred media types, external references,
and discarded reference siblings produce structured diagnostics.
`MaxDocumentNodes` bounds transformed Swagger objects independently from
`MaxSchemaNodes`; the root-member limit is enforced before copying wide
document roots.

OpenAPI 3.0 documents can be upgraded directly to 3.1 or 3.2. The converter
recursively translates `nullable` and boolean exclusive bounds at every Schema
Object location while leaving schema-shaped extensions untouched. Ignored
siblings of 3.0 Reference Objects are removed with loss diagnostics, and
external references require manual review because their target document cannot
be changed locally. Schema traversal is bounded by `MaxSchemaNodes`.

OpenAPI 3.1 documents can be upgraded to 3.2; when the source omits
`jsonSchemaDialect`, the converter writes the 3.1 base dialect explicitly so
the target's 3.2 default does not change Schema Object meaning.

OpenAPI 3.1 documents can also be downgraded to OpenAPI 3.0. Boolean schemas,
type unions, `const`, examples, and numeric exclusive bounds are translated;
unsupported JSON Schema keywords and 3.1-only document fields are removed
with exact loss diagnostics.

OpenAPI 3.2 documents can be downgraded to OpenAPI 3.1 or chained through to
3.0. The 3.2 Schema Object dialect is made explicit for 3.1 targets. Media Type
Object components are inlined at known uses, and 3.2-only operations, fields,
encodings, examples, and reusable registries produce exact loss diagnostics.

OpenAPI 3.0, 3.1, and 3.2 documents can be downgraded to Swagger 2.0. Newer
documents first pass through the same loss-aware OpenAPI downgrade stages.
Simple absolute server URLs become host, base path, and schemes; component
schemas become definitions; request body and response media types become
consumes and produces; body and non-body parameters, reusable request bodies,
parameters, responses, basic and API-key security schemes, response schemas,
OAuth 2 flows, response headers and examples, and internal references are
translated. Concepts without a Swagger representation produce loss
diagnostics.
Multipart and form-urlencoded object bodies become Swagger `formData`
parameters, including required fields, binary files, arrays, and reusable
request bodies inlined at operation uses.
Collisions between reusable parameters and request bodies use a deterministic
request-body rename, rewritten references, and a manual-review diagnostic.

```go
target, err := openapi.ParseVersion("3.2.0")
if err != nil {
	return err
}
result, err := convert.To(ctx, document, target, convert.DefaultOptions())
if err != nil {
	return err
}
for _, diagnostic := range result.Diagnostics() {
	fmt.Printf("%s: %s\n", diagnostic.Kind, diagnostic.Message)
}
```

Every upgrade ending at OpenAPI 3.2 returns a structured manual-review
diagnostic. The result retains the immutable source document for audit
provenance.

## Parameter serialization

`parameter.Encode` serializes caller-owned `jsonvalue.Value` inputs for every
OpenAPI 3.x path, query, header, and cookie style. Options require an exact
specification version and reject undefined style, location, explode, and
`allowReserved` combinations. OpenAPI 3.2's `cookie` style follows Cookie
header delimiters without applying implicit escaping. `parameter.Decode`
reverses those styles using an explicit scalar, array, or object shape and an
explicit policy wherever empty and undefined values serialize identically.
`parameter.OptionsFor` derives options from a Parameter Object and applies the
location-specific `style` and `explode` defaults before encoding or decoding.
`parameter.OptionsForResolvedSchema` accepts a caller-resolved Schema Object so
`allowEmptyValue` is ignored when a referenced shape has undefined
serialization for the selected style.
OpenAPI 3.2 parameter examples are checked in both directions: `dataValue` is
encoded, `serializedValue` is decoded and compared with paired data, and an
explicit `ExternalExampleResolver` can retrieve bounded `externalValue` text
for the same decoding and comparison. Parameters using `content` instead of
`schema` use the explicit media-type codec boundary described above.
For APIs that choose `spaceDelimited`, `pipeDelimited`, or `deepObject`,
`parameter.EscapeAmbiguousDelimiters` defines an optional reversible
pre-serialization convention: literal delimiters and percent signs become
percent triplets that the standard codec encodes a second time. After normal
decoding, `parameter.UnescapeAmbiguousDelimiters` reverses only those triplets.
This keeps structural delimiters distinguishable without leaving RFC 3986
delimiters unencoded; both helpers are byte-bounded and reject other styles.
The codec constructs each defined serialization directly; it does not use an
RFC 6570 template engine or apply a second template-expansion pass to already
encoded values. This avoids double percent-encoding reserved characters in
base64 and other caller-supplied data.
For `allowEmptyValue: true`, 3.0.0 through 3.0.3 and 3.1.0 decode a zero-length
query value as an empty string; the clarified 3.0.4, 3.1.1, 3.1.2, and 3.2.0
texts decode it as unused. Validation warns wherever the field is deprecated.
OpenAPI 3.1.2 and 3.2 header values, plus 3.2 `cookie`-style values, pass
through without percent-encoding, percent-decoding, quoting, or escaping.
`parameter.RecommendHeaderEncoding` checks a bounded representative Header
Object value and recommends `content` with `text/plain` when it contains
semicolon parameters or characters that are not URI-safe; otherwise it permits
the `schema` strategy. Malformed header values and line breaks are rejected.
Both directions enforce configurable byte and item limits; zero-valued limits
use conservative defaults of 1 MiB and 10,000 top-level collection items.
Decoders enforce the item limit while splitting input, before allocating or
percent-decoding an over-budget collection.
`parameter.EncodeSwagger20` and `parameter.DecodeSwagger20` cover primitive
values and all five Swagger 2.0 array collection formats across path, query,
header, and form-data locations, including `multi`'s location restrictions.

## Discriminator selection

`discriminator.Select` derives an explicit, implicit, or OpenAPI 3.2 default
schema-selection hint from a Discriminator Object and payload. Bare ambiguous
targets are treated as schema names, while URI-shaped targets such as `./Cat`
remain URI references. Selection is bounded, performs no I/O, and never
changes the independent Schema Object validation result.
`validate.SchemaInstance` adds OpenAPI semantic checks for local discriminator
targets: implicit component names, explicit mappings, and transitive named
`allOf` descendants are accepted, while an unknown local discriminating value
produces a stable diagnostic. Explicit URI targets remain caller-resolved
selection hints and do not trigger hidden resource loading.
When `oneOf` or `anyOf` is adjacent to a discriminator, validation requires
mapping targets and every known transitive `allOf` descendant to appear in an
adjacent alternative list. A parent-only discriminator continues to use its
inheriting schemas as alternatives without redundant lists.

## Response selection

`response.Select` chooses a response definition for a concrete HTTP status.
An exact three-digit key takes precedence over its uppercase wildcard range,
and `default` is the final fallback. Selection is immutable, validates the
status range, and performs no reference resolution or I/O.
`response.Headers` retains bounded Header and Reference Object descriptors in
source order while removing `Content-Type` case-insensitively.
`response.SetCookieValues` validates and copies bounded, pre-encoded cookie
field values. Each returned element is emitted independently, without a field
name or colon, and no percent, base64, or other escaping is applied by the
toolkit.
`response.LinksetHeaderValue` converts a bounded ASCII
`application/linkset` document to an HTTP field value by replacing document
newlines, including CRLF, so the result can be emitted by Header Object
consumers without field-line injection.
`media.ValidateLinksetJSON` checks the bounded RFC 9264 JSON model, including
contexts, relation arrays, URI-reference targets, and standard, extension, and
internationalized target attributes. `media.SerializeLinkset` transcodes that
same model to deterministic `application/linkset` text, repeats shared anchors
for each emitted link, and RFC 8187-encodes internationalized attributes. Both
operations are local-only and enforce independent link-count and byte bounds.
OpenAPI 3.2 document validation also statically checks `application/linkset`
and `application/linkset+json` schemas for the required root `linkset` array,
context objects, relation arrays, and required string `href` targets through
bounded internal or explicitly resolved external Schema Object references.

## Security requirements

`security.Satisfied` evaluates caller-owned credentials against a Security
Requirement array without performing authentication itself. Alternatives use
OR semantics, while every scheme and required scope within one alternative
must be satisfied. Empty arrays and empty requirement objects permit anonymous
access, and independent limits bound alternatives, schemes, and scopes.

Document validation resolves Security Scheme component references through the
same explicit resolver policy used for other OpenAPI references. Resolved local
or external schemes drive OAuth scope checks and version-specific role-list
rules. OpenAPI 3.2 Security Requirement URI names are likewise type-checked
when internal or when an external resolver is authorized; no retrieval occurs
without that option.

## Conformance status and limitations

The claimed document lines are Swagger 2.0, OpenAPI 3.0.0 through 3.0.4,
OpenAPI 3.1.0 through 3.1.2, and OpenAPI 3.2.0. The normative ledger has no
unimplemented or unresolved row. Accepted errata and interpretation decisions
are explicit in [`docs/specification-decisions.md`](docs/specification-decisions.md).

Deliberate boundaries remain:

- YAML is the documented JSON-equivalent subset; aliases, merge keys, custom
  tags, and non-string keys are rejected rather than assigned loader-specific
  semantics.
- External files and networks are unavailable until a caller supplies an
  explicitly authorized resolver. Rich text and caller callbacks remain in the
  caller's trust domain.
- Cross-minor conversion reports every retained, removed, and impossible
  construct; it does not promise lossless conversion where the target language
  cannot represent the source.
- OpenAPI Overlay and Arazzo are separate specifications and outside this
  module's claim. The module exposes no end-user CLI or source-code generator;
  repository-local generators maintain audited typed views and evidence only.
- Symlink-containment tests skip on a Windows host only when that host denies
  the test process permission to create a symlink. Linux and macOS run the same
  containment assertions, and Windows still runs all other package tests.
- Finite fuzz and differential runs establish reproducible observations, not a
  proof that no future input or independent implementation can expose another
  defect. Scheduled campaigns preserve that continuing detection role.

## Development gates

Run the complete local gate with:

```sh
make check
```

It verifies formatting, module tidiness, `go vet`, tests, race tests,
specification extraction, model generation, and generated-file drift. Pinned
specification revisions and checksums are recorded in
[`specification/manifest.json`](specification/manifest.json).
Interpretations and accepted errata are recorded in
[`docs/specification-decisions.md`](docs/specification-decisions.md).
The implemented trust boundaries, attacker model, controls, and residual caller
responsibilities are recorded in [`docs/security.md`](docs/security.md).

Run `make coverage` for the required complete statement-coverage report and
`make fuzz`
for short local fuzz campaigns across parsers, Schema Object compilation and
evaluation, validation, serialization, server-sent events, the mutation-report
CLI decoder, references, component bundling, runtime expressions, parameter
decoders, operation filtering, semantic operation diffing, reference
dereferencing, and forward and lossy version conversion.
`FUZZ_TIME` controls each fuzz target's duration. The scheduled CI job runs the
same targets for five minutes each.
Coverage fails below 100% for any production package or for the repository in
aggregate. `MIN_PACKAGE_COVERAGE` and `MIN_TOTAL_COVERAGE` cannot lower those
release floors.

`make provenance` verifies every artifact listed in
[`specification/manifest.json`](specification/manifest.json) against its pinned
SHA-256 checksum. It rejects missing, changed, duplicate, escaping, symlinked,
or non-regular artifact paths and performs no network access. `make
conformance` includes this offline provenance check.

`make api` compares the current exported module API with `HEAD^` and fails on
source-incompatible changes. Set `API_BASE_REF` to a release tag or target
branch when checking a wider change set. The pinned checker compares exported
Go declarations; behavioral compatibility remains covered by tests and the
semantic diff package.

`make mutation MUTATION_PATH=./security` runs the pinned mutation engine for
one package and independently rejects every lived, uncovered, timed-out,
skipped, or otherwise unresolved mutant. The default path is `./security`.
Set `MUTATION_INTEGRATION=true` for command packages. The scheduled CI matrix
shards every production package, isolates the root package from recursive
subpackages, and requires strict 100% efficacy and mutator coverage in every
shard.

Run `make benchmark` for allocation-reporting comparisons of representative
100-path parsing, warm validation, canonical JSON serialization, internal
reference resolution, external component bundling, semantic operation
diffing, operation filtering, document merging, and OpenAPI 3.0-to-3.1 and
3.1-to-3.2 conversion.
Benchmarks perform no internet access. Explicit resolver cases use one temporary
local file and an in-process loopback HTTP server.
The reproducible capture method, workload definitions, allocation budgets,
limitations, and raw evidence are documented in
[`docs/performance.md`](docs/performance.md).

Run `make interoperability` for the pinned, isolated-module comparison with
independent OpenAPI implementations. Its complete dependency graph and
checksums remain outside the core module. The exact versions, fixture policy,
classified differences, update procedure, and observed matrix are documented
in [`docs/interoperability.md`](docs/interoperability.md).
