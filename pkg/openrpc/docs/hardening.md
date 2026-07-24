# Hardening

- Set parser and JSON limits below defaults when the deployment contract is
  smaller.
- Keep unknown-field rejection enabled for untrusted current-version input.
- Run meta-schema and semantic validation; use resolved validation only with an
  explicit store and allowlist.
- Prefer in-memory or embedded stores. For HTTP, keep HTTPS-only exact hosts,
  private-address rejection, redirect limits, compression rejection, timeout,
  and streamed byte limits.
- Treat conditional compatibility findings as review-required.
- Scope discovery filtering and caching to the caller's authorization model.
- Export telemetry only from `observe` events, never from raw payloads or
  errors containing caller state.
- Run race, fuzz, mutation, vulnerability, conformance, and coverage gates on
  every release candidate.

The package uses no `unsafe`, cgo, `go:linkname`, hidden goroutines, or mutable
process-global registries.
