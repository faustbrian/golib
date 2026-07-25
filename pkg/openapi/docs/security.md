# Security and trust-boundary model

This document describes the implemented security model reviewed by the
2026-07-22 release audit. It is a bounded engineering claim, not a guarantee
against every future input, dependency defect, or caller misconfiguration.

The labels below keep different kinds of evidence separate:

- **Specification requirement** is imposed by OpenAPI or an incorporated
  standard.
- **Observed fact** is directly established by current source and executable
  tests.
- **Package policy** is a deliberate restriction or guarantee chosen by this
  module.
- **Inference** is a security conclusion drawn from the preceding evidence.

## Protected properties

The protected properties are process availability; bounded CPU, memory,
goroutine, file, socket, and output use; the integrity of parsed semantics and
pinned conformance artifacts; filesystem and network authority; confidentiality
of paths, credentials, document contents, and resolver details; deterministic
results; and the absence of mutable cross-call aliasing.

Attackers may control JSON, YAML, Schema Objects, references, runtime
expressions, parameter values, media payloads, public API descriptions, reader
and writer failures, remote HTTP responses, and files beneath a caller-granted
root. Callbacks and explicitly supplied resolvers are caller code and therefore
belong to the caller's trust domain.

## Trust boundaries

| Boundary | Authority granted | Implemented control | Evidence |
| --- | --- | --- | --- |
| JSON and YAML readers | Bytes and reader behavior only | Strict single-document parsing, duplicate-key and invalid UTF-8 rejection, exact numbers, JSON-equivalent YAML, configurable independent limits, and context-aware reads | `parse` tests, fuzzers, race tests, and mutation gate |
| Semantic values and models | Caller-owned immutable data | Constructor copies, caller-owned returned slices, ordered exact semantics, and bounded high-level traversal | `jsonvalue`, `model`, generated model, composition, diff, validation, and serialization tests |
| File resolver | Only explicitly listed existing directories | Canonical roots opened with `os.OpenRoot`, post-symlink containment, accepted extensions, regular-file checks, cumulative document and byte limits, close lifecycle, and redacted access failures | `reference/file_resolver*_test.go` and resolver fuzzing |
| HTTP resolver | Exact schemes, hosts, ports, and optional CIDRs | No environment proxy, no transparent decompression, DNS address approval and pinned dialing, special-purpose address denial, redirect revalidation, credential stripping, bounded headers, bodies, documents, redirects, addresses, concurrency, and duration | `reference/http_resolver*_test.go` and resolver fuzzing |
| External references | Only the caller's explicit `Resolver` | URI and JSON Pointer validation, bounded graph traversal, cycle termination, cache identity, cancellation, and redacted resolver failures | `reference` tests, fuzzers, race tests, and mutation gate |
| JSON Schema compiler | Only explicitly configured dialect and resource loaders | Dialect separation, bounded traversal, single-flight construction, cancellable waiters, and redacted loader and compilation failures | `jsonschema` conformance, concurrency, fuzz, race, and mutation evidence |
| Writers and generated evidence | Caller-supplied writer or repository-local destination | Byte, node, and depth limits; deterministic ordering; atomic temporary-file replacement; bounded, single-value internal decoders | `serialize` and internal command tests and fuzzers |
| Official artifacts | Repository checkout only | Pinned source revision or retrieval date, SPDX license, HTTPS license source, SHA-256, regular-file and symlink checks, and offline verification | `specification/manifest.json` and `make provenance` |

## Parser policy

**Specification requirement:** OpenAPI descriptions use JSON or YAML with the
JSON data model and string mapping keys. Version-specific OpenAPI and JSON
Schema rules are applied only after syntax parsing.

**Package policy:** Ambiguous syntax is rejected. This includes concatenated or
multi-document input, duplicate keys, non-string YAML keys, anchors, aliases,
merge keys, custom tags, non-JSON number forms, invalid UTF-8, and unpaired
JSON surrogate escapes. YAML is not a general-purpose YAML loader.

**Observed fact:** Input bytes are read through the caller context under
`parse.Limits`. JSON checks cancellation during token traversal. YAML checks it
while the underlying decoder consumes syntax and at both document boundaries,
then during semantic traversal. Parser diagnostics do not include source text
or member names.

**Inference:** YAML alias expansion, parser differentials, and diagnostic
reflection cannot be used to smuggle alternative OpenAPI semantics through the
document parser. The caller still needs a deadline appropriate to its service.

## Network and filesystem policy

**Package policy:** Core parsing and validation never load a remote or local
resource implicitly. A nil resolver means no external retrieval. File and HTTP
resolvers grant no authority with their default options until the caller adds
roots or hosts.

**Observed fact:** HTTP identifiers reject user information, query strings, and
fragments. Redirects are re-authorized and lose `Authorization`, `Cookie`, and
`Proxy-Authorization`. Environment proxies and transparent decompression are
disabled. Responses with unsupported explicit media types or non-identity
content encodings are rejected. File identifiers must be absolute `file` URIs
inside a root after symlink evaluation.

**Inference:** A description cannot independently turn a `$ref` into ambient
SSRF or filesystem access. Explicitly allowing a private CIDR or broad root is
a transfer of authority by the caller and must be treated like any other
security-sensitive configuration.

The supplied HTTP policy makes an authorization decision on resolved addresses
and dials the approved address. It does not claim that an explicitly allowed
remote service is honest. Content authenticity, TLS trust-root policy, service
identity beyond normal HTTPS verification, and application credentials remain
caller responsibilities.

## Resource and lifecycle policy

**Observed fact:** Independent limits exist for input and output bytes, syntax
tokens, semantic values, scalar size, object and array width, depth, references,
documents, redirects, addresses, concurrency, diagnostics, and operation-
specific graph work. Limits are checked before wide semantic copies on audited
paths. File handles and HTTP bodies are closed, temporary output is replaced
atomically, and resolver document budgets are cumulative.

**Package policy:** Zero-valued option fields use documented defaults only where
the API explicitly says so. Invalid negative or overflow-prone limits are
rejected. Callers should lower defaults to their expected document size and use
deadlines for every untrusted operation.

**Inference:** The implemented guards bound known amplification axes; aggregate
process safety still depends on callers bounding the number of simultaneous
top-level operations and the lifetime of objects they retain.

## Concurrency and ownership

**Observed fact:** Parsed semantic values and generated models are immutable.
Collection accessors return outer slices owned by the caller. Resolver counters,
compiler construction, and validation caches are synchronized. Callbacks are
not deliberately invoked while package locks are held. Race tests exercise all
production packages.

**Package policy:** Immutability of a generic collection does not recursively
freeze an arbitrary caller-defined element type. Callers must not mutate data
behind their own pointers concurrently unless that type documents safety.

## Content and secret handling

**Package policy:** Descriptions, examples, defaults, extensions, Markdown, and
other free-form strings are data. The module preserves them; it does not mark
them safe HTML, shell, source code, filesystem paths, or templates. Downstream
renderers and generators must apply contextual escaping.

**Observed fact:** The module emits no telemetry. Resolver access errors redact
paths, URLs, host details, anchor text, and underlying transport messages while
preserving stable error classification. Parser `Error()` strings are bounded
and content-free. A caller can deliberately inspect wrapped reader errors with
`errors.Unwrap`; those causes belong to the caller's reader trust boundary and
should not be logged blindly.

## Supply chain and update procedure

The complete selected Go build list, including graph-only modules, is recorded
in [`dependencies.tsv`](dependencies.tsv). `make dependencies` rejects version
drift, verifies module checksums, confirms a tidy graph, and runs the inventory
audit. `make license` scans built packages, and `make vuln` analyzes reachable
vulnerabilities. Official evidence updates follow
[`specification/README.md`](../specification/README.md) and must preserve exact
source bytes.

Security-sensitive updates must add a failing regression, retain explicit
limits and error classification, run the relevant fuzz and race targets, and
pass mutation testing before the release gates.

## Non-goals and residual responsibilities

The package does not authenticate API traffic, execute runtime expressions,
sanitize rich text, manage application secrets, establish a sandbox for caller
callbacks, or make an explicitly trusted remote server safe. Optional resolver
authority is disabled until configured.

Interoperability observations, fuzz campaigns, performance budgets, and
cross-platform tests are separate executable evidence linked from
[`audit-report.md`](audit-report.md). None may be inferred from this threat
model, aggregate coverage, official-schema success, or self-round trips.
