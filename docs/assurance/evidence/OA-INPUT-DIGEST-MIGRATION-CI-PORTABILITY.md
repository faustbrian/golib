# CI Portability Input-Digest Migration

Observed at `2026-08-19T13:05:06Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/queue-control-plane` CLI regression suite now distinguishes its
required-environment validation from downstream client failures, killing both
logical-condition mutants in that boundary. The `pkg/scheduler` fuzz-target
discovery script now uses POSIX `grep` instead of an undeclared ripgrep
dependency. These changes alter tests and verification scripts only.

The CloudEvents Golib adapter changed only because its operational-assurance
fingerprint includes the affected transitive verification closure. Its source,
public API, runtime behavior, dependencies, and retained HTTP-reference
scenario are unchanged.

The authorized one-way transitions are:

- `pkg/cloudevents/adapters/golib`:
  `c19328fd84e93e42ea00dcfb5765606d6cb9fc8f70bff06d604e4857afe299d4`
  to `ee1d0884ecc7f783e99af0c2a09dcf12499d361b995610423ee38edb350b19c3`;
- `pkg/queue-control-plane`:
  `328b8a172a5080fc031d842df0e42ad1b1edcabbaa9165cb118548679b1382ea`
  to `7b6dccf1b89cbdf3f8023dcf9f81183847f698904a1f2cf36d4bfa46d1ecd554`;
- `pkg/scheduler`:
  `c844d3beadc4e4e679b0f637772c1519af52b15c171034c7f99a01c950c52ed3`
  to `aa7a96ea2d5ca54bc07b100b8b5f07c5c357db57f86e47d9303b84d0158e0505`.

## Behavioral Proof

The focused queue CLI test passed and mutation testing killed all six viable
mutants in `cmd/queue-control`; every unchanged package checkpoint was reused
by exact content identity. The scheduler fuzz gate completed every discovered
target without ripgrep. Existing retained operational evidence was not
relabelled or re-executed.

## Claim Boundary

This evidence authorizes only the three exact transitions above. It does not
change an operational scenario's observation time, environment, scope,
accepted risks, or release verdict.
