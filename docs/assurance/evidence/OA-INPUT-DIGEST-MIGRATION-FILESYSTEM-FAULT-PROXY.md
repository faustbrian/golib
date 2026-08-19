# Filesystem Fault-Proxy Input-Digest Migration

Observed at `2026-08-19T19:58:01Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The public `pkg/filesystem/fstest` TCP fault proxy now applies configured
latency after a source chunk arrives. Idle time before a transfer therefore
cannot consume a fault that is intended to delay that transfer. The FTP
configuration tests also identify each rejected field by its required error
instead of allowing a later network failure to satisfy the assertion.

The retained `pkg/service/integration/reference-external` scenario imports the
filesystem core and S3 adapter, but it does not import `filesystem/fstest` or
the FTP adapter. Its exercised production code, public composition, runtime
configuration, dependencies, and loopback S3 boundary are unchanged. The
operational-assurance input digest therefore moves from
`82815a93a983db65cfdced24a72071fad65ad98ff6261880d3c841d8419a39fa` to
`dda6f289d0d163624d931b3ff0d1591a1890a45ca703d68245418709ba7d25a7`.

## Behavioral Proof

The fault-proxy regression failed repeatedly against the prior read-side
latency implementation and passed repeatedly after latency moved to the
forwarding writer. Exact filesystem mutation verification killed all viable
mutants, including 138 of 138 FTP mutants, and the current strict module
evidence passes tests, race detection, exact statement coverage, mutation,
fuzzing, lint, static analysis, security, documentation, API, interoperability,
and benchmark gates. Release and clean-consumer evidence is rerun separately
because the public testing package changed.

## Claim Boundary

This evidence authorizes only the exact one-way digest transition above for
the retained `OA-REFERENCE-EXTERNAL` observation. It does not relabel that
scenario's execution time, claim that it exercised the fault proxy or FTP
adapter, or replace current filesystem release and consumer verification.
