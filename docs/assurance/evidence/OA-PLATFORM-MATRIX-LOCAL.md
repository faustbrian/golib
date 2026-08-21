# OA-PLATFORM-MATRIX Local Evidence

Observed at `2026-08-13T10:29:28Z` on Docker Engine `29.6.2` running
`linux/arm64`, with BuildKit `0.31.2` and Go `1.26.5`.

## Executed Proof

- A fresh task-owned BuildKit builder produced `CGO_ENABLED=0` scratch images
  for `linux/amd64` and `linux/arm64` from the pinned Go builder digest.
- Each image ran as UID and GID `65532` with a read-only root filesystem,
  every Linux capability dropped, `no-new-privileges`, a 64 MiB memory limit,
  a 0.25 CPU limit, a 64-process limit, and a 256-descriptor limit.
- A bounded `/tmp` tmpfs accepted, persisted, and removed an application file
  while the remainder of the image stayed read-only.
- Liveness, startup, readiness, and business listeners responded on both
  architectures. The runtime reported the expected Linux architecture and
  unprivileged identity.
- The service resolved `host.docker.internal`, trusted an explicitly mounted
  private CA, completed an HTTPS dependency request, and rejected readiness
  until that dependency succeeded.
- `SIGTERM` closed admission, drained the process through the public `service`
  lifecycle, and produced the documented exit status `143` before Docker's
  five-second kill deadline.
- The canonical module check and a separate race-enabled module execution
  passed. The disposable containers, images, builder, fixture, and Go caches
  were removed after the run.
- The complete matrix also passed from a task-owned extraction of the committed
  Git archive, without `.git`, sibling checkouts, dirty files, warm Go caches,
  ambient credentials, or developer-specific paths.

## Claim Boundary

This proves the local ECS-compatible container shape and emulated Linux
architecture matrix. It does not prove native Graviton execution, live ECS
task lifecycle, task IAM credentials, ECS DNS or network policy, production
certificate roots, bit-for-bit artifact reproducibility, deployment, load,
soak, or production readiness. `OA-PLATFORM-MATRIX` therefore remains pending.
