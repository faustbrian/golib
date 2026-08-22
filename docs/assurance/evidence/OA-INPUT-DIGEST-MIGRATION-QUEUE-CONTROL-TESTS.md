# Queue Control Test Input-Digest Migration

Observed at `2026-08-22T18:17:21Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

Queue control-plane tests now distinguish both valid drain targets, preserve
caller-supplied command metadata while proving missing-value defaults, and
reject every non-terminal structured dispatch outcome. The change modifies
tests and changelog entries only; production source, public APIs, dependencies,
fixtures, runtime configuration, and operational behavior are unchanged.

The authorized one-way transition is:

- `pkg/queue-control-plane`:
  `d53a2100e31e208117316d25782e77c5b41c8c66d346f6a0a4513f766ffbc075`
  to `c97ab01c9f0baa7e435b966b9b94306324f7ae1ada6eb98f05918c61a2adf3f5`.

## Behavioral Proof

The focused queue control-plane package tests passed under Go 1.26.6. The
control package mutation campaign killed all `62/62` viable mutants, including
the seven branches that survived the preceding CI campaign.

The retained `OA-REFERENCE-HTTP` observation cannot change because this
transition contains no production or reference-service change.

## Claim Boundary

This evidence authorizes only the exact transition above. It does not change
an operational scenario's observation time, environment, scope, accepted
risks, or release verdict, and it does not replace the current CI module gate.
