# Operations, troubleshooting, and release verdict

Monitor counts and latency by fixed operation/outcome/reason. Alert on replay
store failures, policy rejection changes, terminal delivery rates, exhausted
retries, and sustained body/header limits. Never label metrics with tenant,
event, endpoint, key, or raw status outside the bounded status field.

Common failures:

- `invalid webhook timestamp`: verify clock synchronization and sender units;
- `invalid webhook signature`: compare exact raw bytes, target host/path/query,
  algorithm, key ID, and vector output without printing secrets;
- replay rejection: check namespace, TTL, event-ID uniqueness, and atomic store;
- policy rejection: inspect the configured resolver and prefixes privately;
- one attempt only: an idempotency key is required for retries;
- queue/outbox repeats: ensure only the durable layer owns retry scheduling.

Release is blocked by any red `make check`, `make safety`, or
`make interoperability` gate, canonical drift, coverage below 100%, race/fuzz
failure, unresolved high/medium finding, or unsupported provider claim. The
release workflow verifies the tag matches `v<module version intent>`, runs all
gates, and publishes source artifacts through GitHub Releases.

Fuzzing defaults to four workers so results do not depend on host CPU count.
Override `FUZZWORKERS` only when the runner has a deliberate resource budget.

Current residual risks: HMAC security depends on operator-generated secrets;
durability depends on the supplied replay/queue/outbox stores; explicit SSRF
allow prefixes weaken defaults; and `http-client` has no published API.
