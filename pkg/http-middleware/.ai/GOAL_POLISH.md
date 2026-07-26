# Goal: Add Bounded Request Decompression And Comparative Evidence

## Objective

Add the missing request-decompression capability and fair middleware comparison
evidence without changing the original completed goal files.

## Request Decompression

- Add an independently importable `decompress` package.
- Support gzip initially; isolate additional content-coding dependencies.
- Parse `Content-Encoding` as an ordered coding stack and reject malformed,
  duplicated, unsupported, or excessive stacks deterministically.
- Enforce separate encoded-byte, decoded-byte, expansion-ratio, coding-depth,
  decoder-memory, and processing-time limits.
- Prevent decompression bombs, concatenated-stream bypasses, truncated-stream
  ambiguity, checksum bypasses, and retained-buffer growth.
- Preserve cancellation and close every decoder and request body on success,
  rejection, timeout, cancellation, panic, and partial downstream reads.
- Define exact `Content-Encoding` and `Content-Length` rewrite behavior.
- Document ordering with encoded `bodylimit`, content validation,
  authentication, exact-byte signatures, JSON-RPC, JSON:API, and application
  decoding.
- Never silently decompress a signed payload whose signature covers transport
  bytes.

## Comparative Benchmarks

- Compare equivalent behavior with direct `net/http`, chi, Echo, Gin, Alice,
  Negroni, and Gorilla middleware only after shared correctness tests pass.
- Separate chain overhead from individual middleware behavior and full-stack
  framework cost.
- Keep Fiber/fasthttp in a separately labeled architecture track including
  adapters, request-context semantics, and transport costs.
- Store pinned versions, fixtures, raw output, allocation data, profiles, and
  statistical analysis.

## Required Evidence

- Meaningful 100% coverage of new production code.
- Fuzz, mutation, race, leak, real-listener, streaming, and hostile compression
  tests.
- Security and resource-budget updates.
- Updated API, ordering, adoption, security, performance, FAQ,
  troubleshooting, and changelog documentation.

## Completion Criteria

- Compressed requests cannot bypass limits or leak resources.
- Middleware order and signature semantics are explicit and tested.
- Comparative claims are correctness-gated and transport differences are not
  hidden.

