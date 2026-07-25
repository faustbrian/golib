# Quality Contract

## Coverage

`scripts/check-coverage.sh` instruments every production package in one module,
merges repeated blocks across package test binaries, and requires exact covered
and total statement counts to match per package. Missing packages, empty
profiles, zero executable evidence, rounded percentages, and aggregate-only
success fail.

Coverage tests must assert meaningful behavior: boundaries, errors, lifecycle,
cleanup, hostile inputs, state transitions, and concurrency where applicable.
Statement coverage is supplemented by mutation, fuzzing, properties, official
fixtures, and interoperability checks.

## Mutation

`scripts/check-mutation.sh` enables every supported Gremlins operator and
requires exactly 100% efficacy and mutant coverage. Reports must be non-empty
and every viable mutation must be `KILLED`.

`scripts/discover-mutation.sh` is the separate review command for enumerating
mutants and source-hashed zero-mutant packages. It does not write enforcement
evidence and MUST NOT be used as a mutation quality gate.

Equivalent or invalid mutants may be excluded only through a reviewed record
with exact location/identifier, transformation, rationale, evidence, reviewer,
review date, and expiry. Broad file/function exclusions and threshold reductions
are prohibited.

## API Compatibility

Modules with bespoke compatibility tooling keep using that stricter command.
Every other declared module uses `api/baseline.txt`, generated with the pinned
`apidiff` version through `make api-update MODULES=<module>`. The mandatory API
gate rejects missing baselines, tool failures, and incompatible exported API
changes. Baseline updates require review and the affected module changelog.

## Other Gates

Mandatory gates include formatting, tidy verification, vet, strict lint,
Staticcheck, isolated tests, race tests, fuzz smoke, vulnerability scanning,
secret scanning, license review, SBOM generation, API compatibility,
documentation, conformance, interoperability, benchmarks, and release proof.
NilAway remains advisory with visible no-regression findings.

Conformance proves declared specification behavior against pinned normative
fixtures or matrices. Interoperability proves behavior against independent
implementations. Each successful gate writes its module-attributable output
atomically before another gate begins.

## Evidence Checkpoints

`scripts/check-gates.txt` is the canonical ordered contract used by both the
aggregate module check and the repository runner. Each verification gate is
executed through `scripts/run-gate-with-evidence.sh`, which writes the complete
log before atomically publishing its JSON checkpoint. Failed and invalidated
results are persisted just as promptly as successful results.

Each checkpoint records the original execution revision, input fingerprint,
timestamps, environment identity, exit status, and log checksum. A commit hash
is traceability metadata, not a cache key. An existing successful checkpoint is
reused only when its module, gate, complete-input fingerprint, result, exit
status, and stored log checksum all validate. Reuse retains the original
execution revision and atomically records the current revalidation revision,
time, and count.

Mutation remains more narrowly content-addressed than the aggregate gates. Its
module fingerprint is assembled from the independently persisted package
fingerprints, including compiled source, tests, embedded data, conventional
fixture corpora, owned dependencies, policy, tool versions, and relevant
environment. Consequently, a history-only change or documentation edit cannot
force mutation execution when its actual inputs are unchanged.

Commands that intentionally modify the checkout (`format`, `tidy`, and
`api-update`) do not produce verification checkpoints. Their resulting files
must instead be proven by the corresponding non-mutating gate.
