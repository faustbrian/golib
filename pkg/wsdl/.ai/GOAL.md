# Goal: Complete WSDL Description Toolkit

## Objective

Build `wsdl` as a complete open-source implementation of WSDL 1.1 and WSDL
2.0 document models, parsing, validation, resolution, deterministic generation,
composition, and inspection. It MUST use `xsd` for XML Schema and `wire`
for bounded XML/SOAP primitives without absorbing transport clients.

## Specification Scope

- Full WSDL 1.1 including messages, port types, operations, bindings, ports,
  services, imports, documentation, extensibility, SOAP 1.1/1.2 bindings, HTTP,
  and MIME bindings where claimed.
- Full WSDL 2.0 including interfaces, operations, faults, bindings, endpoints,
  services, message exchange patterns, imports/includes, and extensions.
- Pin W3C notes/recommendations, schemas, errata, adjuncts, examples, and
  interoperability fixtures with provenance.
- Maintain separate normative matrices and decision registers per version.

## Core Capabilities

- Complete typed models preserving absent/defaulted/extension distinctions and
  source locations.
- Bounded strict XML parsing, QName and namespace correctness, duplicate
  rejection, and deterministic serialization.
- Injected, disabled-by-default resolution for imports and referenced XSDs.
- Compile documents into immutable service/interface/operation/binding graphs.
- Validate operation styles, message parts, faults, headers, bodies, actions,
  encodings, endpoint addresses, extension requirements, and XSD references.
- Programmatic builders that validate before emitting documents.
- Deterministic composition and conflict reporting.
- Optional client/server code-generation model kept separate from core.
- Semantic comparison/diff with explicit compatibility caveats.

## Boundaries

- No HTTP client, SOAP call execution, credentials, retries, WS-Security,
  WS-Addressing runtime, service container, or generated vendor client in core.
- `wire` owns SOAP envelope and fault encoding.
- `xsd` owns schema compilation and instance validation.
- `http-client` owns transport; generated clients are optional consumers.
- WS-* specifications require separate explicit goals and support matrices.

## Security And Bounds

- Defend against entities, SSRF, file disclosure, redirect abuse, import bombs,
  QName confusion, recursive graphs, huge schemas, extension payloads, and
  diagnostic amplification.
- Bound bytes, depth, imports, schemas, operations, bindings, endpoints,
  extensions, output, and code-generation models.
- No hidden network, filesystem, global registry, or background work.

## Verification And Documentation

Require meaningful 100% production coverage, official schemas/examples,
SoapUI/.NET/Java interoperability fixtures where licensing permits, XSD
integration, parse/generate round trips, fuzzing, race, mutation, resolver
security, cancellation, and correctness-gated benchmarks.

Document versions, conformance, models, parsing, validation, resolution,
generation, SOAP bindings, HTTP/MIME bindings, XSD integration, extensions,
security, limits, migration, interoperability, performance, cookbook, FAQ,
compatibility, and changelog. CI/local gates follow ecosystem standards.

## Acceptance Criteria

- Every claimed WSDL 1.1/2.0 feature has executable evidence.
- Existing carrier WSDLs can parse, validate, inspect, and round-trip.
- WSDL and XSD responsibilities remain separate and dependency-safe.
- No external resource is loaded implicitly.
- Meaningful 100% coverage and every blocking gate pass.
