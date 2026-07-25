# Hardening Goal: Distributed Scheduler

## Objective

Prove deterministic schedule calculation and safe distributed ownership under
clock anomalies, process death, backend failure, partitions, rolling deploys,
and hostile schedule definitions.

## Required Audits

- Exhaustively test cron boundaries, DST folds/gaps, timezone changes, leap
  years, month ends, delayed ticks, wall-clock jumps, and long downtime.
- Prove each missed-run policy with bounded catch-up and no unbounded replay.
- Run multiple scheduler replicas through acquisition, heartbeat, expiry,
  takeover, cancellation, completion, and shutdown races.
- Kill owners before/after dispatch and prove duplicates remain documented,
  observable, and controllable with idempotency.
- Test overlap skip, replacement, stale locks, manual unlock, fencing, and tasks
  that ignore cancellation.
- Inject PostgreSQL and Valkey outage, failover, latency, partial writes,
  reconnect, clock disagreement, and lost invalidation.
- Verify schedule updates during rolling deployment do not silently double-run or
  lose identity.
- Bound schedule count, catch-up, lease wait, conditions, history, event hooks,
  retries, and diagnostic output.

## Required Deliverables

- Time-calculation and timezone conformance corpus.
- Multi-replica state, lease, crash-point, and rolling-deploy matrices.
- PostgreSQL and Valkey failure-injection suites.
- Laravel behavior migration matrix and documented intentional divergences.
- Threat model, findings report, resource budgets, and benchmark baselines.
- Updated API, Kubernetes, recovery, security, FAQ, and `CHANGELOG.md`.

## Release Blockers

- Unbounded catch-up, duplicate current ownership, undetected stale owner, or
  overlap-policy violation.
- Incorrect DST/timezone behavior relative to the documented contract.
- Scheduler deadlock, race, panic, leaked goroutine, or unbounded wait.
- Claim that locking or Kubernetes provides exactly-once execution.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- Time, lease, crash, backend, overlap, and rolling-deployment suites pass.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- Every duplicate/missed-run possibility and recovery action is documented.
- No release blocker remains and `CHANGELOG.md` is current.
