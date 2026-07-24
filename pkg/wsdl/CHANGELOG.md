# Changelog

## Unreleased

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Check temporary interoperability fixture cleanup and normalize XML Schema
  extension detection to satisfy the canonical strict lint contract.
- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.
- Separate W3C fixture conformance from Apache Woden interoperability so both
  results remain attributable.

- Normalize standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Refresh the canonical XSD checksum after its API compatibility tooling was
  standardized.
- Add bounded WSDL 1.1 and WSDL 2.0 parsing and deterministic serialization.
- Add SOAP, HTTP, MIME, extension, validation, and XML Schema integration.
- Preserve the WSDL 2.0 `wsdlx:safe` operation property through parsing,
  compilation, generation, serialization, and semantic comparison.
- Add typed WSDL 2.0 `wrpc:signature` support and XSD-backed RPC style
  validation.
- Add XSD-backed WSDL 2.0 IRI and multipart operation-style validation.
- Add explicit resolution, immutable compilation, builders, composition,
  semantic diff, and a separate bounded code-generation model.
- Add provenance matrices, security documentation, fuzzing, benchmarks,
  coverage, mutation, race, and CI gates.
- Add provenance-pinned accepted W3C WSDL 2.0 interoperability fixtures.
- Add licensed SoapUI, `dotnet-svcutil`, Apache CXF, and DHL WSDL 1.1
  interoperability fixtures with executable round-trip evidence.
- Preserve descendant-local namespaces in embedded schemas and reuse canonical
  schema prefixes during deterministic WSDL serialization.
- Add a pinned Apache Woden Java gate for the W3C WSDL 2.0 fixture corpus.
- Run the Woden gate in a digest-pinned Eclipse Temurin container instead of
  depending on an untracked host Java installation.
