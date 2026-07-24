# Hardening evidence

The release gate treats the following suites as required evidence, not as
claims inferred from statement coverage.

| Risk | Evidence |
| --- | --- |
| Official and cross-language compatibility | `TestPinnedCrossLanguageVectors`, `TestOfficialValidCorpus`, RFC 9562 vectors, and maintained Go differential tests |
| Malformed input | `TestOfficialInvalidCorpus`; exhaustive single-byte UUID, ULID, KSUID, and NanoID mutation tests; parser fuzzing |
| Entropy | progressive short-read and premature-EOF tests, deterministic-source collision-domain tests, a 50,000-value random-family campaign, and a 63-symbol rejection-sampling distribution test |
| Monotonic state | same-bucket ordering, overflow, rollback, and 4,096-operation shared-generator race tests for UUIDv7, ULID, TypeID, and KSUID |
| Serialization | text, JSON, binary, SQL Scanner and Valuer, pgx UUID binary, PostgreSQL text codec, and generic typed-wrapper round trips |
| Immutability | fixed arrays, copied binary output, value receivers, and mutation regressions for returned byte representations |
| Privacy | exact leakage tests and `slog.LogValuer` redaction tests for every identifier and typed wrapper |
| Fuzz and mutation | parser targets for every family, a combined binary and JSON codec target, and production mutation gates |
| Performance and locality | comparative generation, parsing, formatting, sorting, and binary-search locality-proxy benchmarks |

The cross-language vector pins are in
`specification/vector-provenance.tsv`. Both official TypeID JSON corpus files
are vendored under `typeid/testdata/official`; the provenance gate verifies
their complete byte-level hashes before the test package embeds and executes
all 9 valid and 21 invalid cases. The TypeID corpus is shared by its Go,
JavaScript, Python, and Rust implementations. The KSUID fixture comes from the
Rust implementation's Segment-compatibility suite, while ULID and NanoID
fixtures come from the JavaScript reference implementations.

No supported generator has a node identifier, topology allocator, checksum, or
node-local sequence field. Node duplication and node-sequence exhaustion are
therefore not silently simulated. `TestDuplicatedGeneratorStateDefinesACollisionDomain`
proves that cloning deterministic clock and entropy state duplicates output;
independence of entropy state is a deployment requirement. Same-bucket
UUIDv7, ULID, TypeID, and KSUID counters fail with `ErrOverflow` instead of
wrapping.

PostgreSQL verification is serverless and exercises pgx's real registered
wire codecs: native UUID uses the 16-byte binary UUID representation, while
ULID, TypeID, KSUID, NanoID, and typed wrappers use canonical text. A database
constraint remains required. No running PostgreSQL service is assumed by the
test suite.
