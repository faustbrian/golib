# Security review packet

## Internal audit status

The current tree has executable evidence for:

- maintained Argon2id and bcrypt vectors;
- independent PHP 8.5.8 bcrypt and Argon2id fixtures;
- strict canonical parsing and pre-primitive resource rejection;
- match/mismatch, rehash, downgrade, entropy, cancellation, and admission paths;
- concurrent policy/admission use and shutdown race states;
- optimistic upgrade data and crash-safe persistence guidance;
- diagnostic and observation redaction;
- exact production statement coverage, fuzz, race, mutation, vulnerability,
  lint, API, documentation, and release gates.

## Independent specialist review

Independent cryptographic specialist review is a release blocker for v1 and
cannot be self-certified by implementation tests. The reviewer should assess:

1. Parameter selection against intended deployment hardware and threat model.
2. PHC/bcrypt grammar and Laravel interoperability.
3. Monotonic rehash and downgrade prevention.
4. Admission/resource ceilings under malicious valid hashes.
5. Error, formatting, observation, and timing surfaces.
6. CAS migration and crash/concurrent-login behavior.
7. Whether maintained primitive usage introduces any unsafe assumptions.

Record reviewer identity, scope, date, commit, findings, remediations, and final
disposition here before tagging v1. Do not replace this gate with coverage,
automated scanning, or an internal assertion of expertise.

## Review invariants

- No custom primitive, unsafe, cgo, assembly, or reversible storage.
- No secret-bearing diagnostics or high-cardinality observations.
- No attacker-selected primitive work above limits.
- No upgrade before match or unconditional stale overwrite.
- No claim of guaranteed erasure, cancellation, or side-channel immunity.
