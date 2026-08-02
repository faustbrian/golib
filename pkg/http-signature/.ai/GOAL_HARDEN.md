# Goal Harden: `http-signature`

## Mission

Prove complete RFC conformance and resist parser differentials,
canonicalization ambiguity, insufficient coverage, replay, downgrade, key
confusion, proxy manipulation, body substitution, and resource exhaustion.

## Required Audit

1. Refresh the normative RFC 9421/RFC 9530 matrix, errata, registries, vectors,
   and independent implementation inventory.
2. Inventory every parser, serializer, component extractor, canonicalizer,
   profile, algorithm, key resolver, replay store, middleware, and error path.
3. Differentially test Structured Fields and signature bases across independent
   implementations and every normative example.
4. Test duplicate labels, duplicate components, reordered parameters, combined
   fields, non-ASCII values, trailers, malformed binary fields, query
   ambiguity, authority reconstruction, and transformed HTTP versions.
5. Prove profiles reject missing mandatory components, unsafe algorithms,
   wrong key types, stale signatures, untrusted origin data, and downgrade.
6. Verify request-response binding, multiple signatures, negotiation, digest
   coverage, streaming, compression, retries, redirects, and body ownership.
7. Exercise nonce races, resolver/cache races, rotation, revocation, resolver
   outage, clock skew, cancellation, process shutdown, and unknown outcomes.
8. Audit constant-time operations, cryptographic APIs, randomness, key
   lifetimes, zeroization claims, secret redaction, and diagnostic safety.
9. Run parser and canonicalizer fuzzing, race, leak, stress, soak, malformed
   network fixtures, and strict allocation/input limits.
10. Verify legacy adapters cannot contaminate the RFC 9421 acceptance path.

## Required Evidence

- complete normative conformance matrix with no undocumented gap;
- RFC, errata, and independent interoperability vectors;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- race, fuzz, leak, stress, soak, and fault-injection results;
- security review covering algorithms, keys, profiles, replay, and proxies;
- equivalent benchmarks against maintained Go implementations where available;
- compiled docs, examples, migration guidance, and clean-consumer proof.

Any interoperability exception MUST identify the peer, exact divergence,
specification analysis, security impact, and chosen behavior.
