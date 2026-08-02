# Comparison provenance

## Netflix Gradient2 reference

- Repository: `https://github.com/Netflix/concurrency-limits`
- Revision: `78a74b9878d38c4c048b0304ce12a162ab7b7222`
- Source: `concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/Gradient2Limit.java`
- Source SHA-256: `06ab7b29b503a3809ae2fde51ba5953ab71dff5530c978b65314481df2bd556d`
- License: Apache-2.0

The Go reference in `internal/netflix/gradient2.go` retains the warm-up
arithmetic mean, exponential long-window average, drift correction,
application-limited guard, RTT tolerance, gradient bound, queue allowance,
smoothing, and absolute min/max clamp. It deliberately
does not port logging, metrics, builders, or Java synchronization and MUST NOT
be used as JVM runtime evidence.

To update it, pin and review a new upstream revision, recompute the source
checksum, translate the equation changes, run the exact local-versus-reference
trace test, regenerate the checked-in report, and review semantic differences
against the other candidates before replacing this record.

## Go implementations

- Failsafe-Go: `github.com/failsafe-go/failsafe-go` v0.9.6, MIT.
- Platinum: `github.com/platinummonkey/go-concurrency-limits` v1.0.0,
  Apache-2.0.

Their module content checksums and transitive dependency checksums are pinned in
`go.sum`; their public APIs are invoked directly rather than copied.
