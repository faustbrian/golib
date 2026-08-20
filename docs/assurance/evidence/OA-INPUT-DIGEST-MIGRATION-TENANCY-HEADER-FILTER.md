# Tenancy Header-Filter Test Evidence Migration

Observed at `2026-08-20T09:54:56Z` on `darwin/arm64`.

The `pkg/tenancy` operational-assurance fingerprint changed from
`1cfef5d97283443123bcfc34656d61100b1c1625150171edf998ec2adf929f92`
to
`178aecce19f50fba4dda270fb6a2fa89c632c679694c3b3610985b2dccc5221e`.

The only operational-assurance input change was a deterministic assertion in
`pkg/tenancy/http/http_test.go`: an unrelated header whose value resembles a
valid tenant identifier must be ignored when the canonical tenant header is
also present. The file changed from SHA-256
`7dd1efa6fcf7784d855ec686efca1655412a7707118dbf5d8bbcbeeb91ef06fb` to
`6ea6c49e1f6a02df64520f5261c682bea638ded6a2cd3b7f083b1085c210e323`.
No tenancy production source, fixture, manifest, dependency, service version,
or runtime configuration changed.

The assertion detects a loop-control mutation that would incorrectly accept
an unrelated header based on map iteration order. Fresh mutation campaigns
killed all 253 viable mutants in the root package, all 28 in `http`, all 39 in
`jsonrpc`, and all 140 in `postgres`, with no survivors, uncovered mutants,
timeouts, nonviable mutants, or skips.

A task-owned clean clone at the pre-test revision reproduced the original
fingerprint exactly under the captured operational-assurance environment. The
current tree produced the replacement fingerprint. The clone and disposable
Go caches were removed immediately after the comparison.

This evidence authorizes only the exact one-way digest migration above. It does
not authorize migration across future production, test, dependency, tool,
service, environment, or orchestration changes.
