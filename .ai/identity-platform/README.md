# Identity Platform Orchestration Entry Point

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Pinned Better Auth source prerequisite

The parity baseline requires actual Git objects, not only the checked-in
surface and leaf manifests. Before starting the orchestrator, the user MUST
obtain the repository once while online in a user-owned location, then keep it
available locally through terminal validation:

```sh
git clone --filter=blob:none https://github.com/better-auth/better-auth.git /absolute/path/to/better-auth
git -C /absolute/path/to/better-auth fetch origin b8077b74ef9a80a7757220b72834349bd8de05c0
export BETTER_AUTH_REPOSITORY=/absolute/path/to/better-auth
ruby .ai/identity-platform/generate_upstream_leaves.rb --check "$BETTER_AUTH_REPOSITORY"
```

This checkout is a persistent user-owned prerequisite, not a task-owned
disposable resource; the orchestrator MUST NOT create it, add it to the
task-owned resource registry, or delete it. The orchestrator MUST preserve this
environment variable through final gate capture and terminal validation. The
validator rejects a missing repository, noncanonical `origin`,
missing pinned commit, wrong object format, or mismatched source object. The
check reads only the local object database and therefore remains usable
offline after the one-time fetch.

## Give one goal to one orchestrator

Give exactly one coordinator agent exactly this prompt:

```text
Execute /Users/brian/Developer/go-libraries/.ai/identity-platform/ORCHESTRATOR_GOAL.md
to completion. Act only as coordinator. Use gpt-5.6-sol subagents with medium
reasoning for package implementation, maximize safe DAG parallelism, commit
coherent verified batches locally, do not push, and stop only at the completion
or blocker conditions defined by the goal.
```

The coordinator reads `ORCHESTRATOR_GOAL.md`, renders `WORKER_PROMPT.md`, and
creates one isolated worker for each eligible inventory unit. Humans MUST NOT
create, paste, or run per-package prompts. Individual goal files are inputs to
the coordinator-rendered worker prompt, not independent entry points; using
them directly omits common, parity, integration, and dependency requirements.
`GOAL_MANIFEST.json` pins every goal's exact bytes and its planning and
canonical package locations so unchanged lifecycle moves remain executable.

## Authority and ownership

The coordinator has write custody of `INVENTORY.md`, `EXECUTION_LEDGER.md`,
`DEPENDENCIES.md`, files directly under this directory, every goal file before
and after its move, root manifests, integration, and final end-state proof.
Custody is not product authority: the coordinator MUST NOT approve a change to
scope, behavior semantics, public API, parity disposition, protocol profile,
acceptance claims, acceptance artifacts, or a goal body. Such a change requires
the explicit user-authorization record defined by `PROGRAM.md` before its first
semantic byte changes. A worker owns only its assigned canonical package
directory minus coordinator-custody paths and reserved descendant roots, plus
package-local code, tests, fixtures, migrations, docs, examples, module files,
and changelog. The coordinator MUST reject cross-package or coordinator-custody
worker edits instead of resolving semantic ownership during a merge.

Execution also requires a platform trust document already pinned on the
recorded `main` base. Committed exact-byte captures record user authorization and
worker spawn/readiness/release/return sequence; repository-authored rows cannot
prove either actor. If that platform evidence is unavailable, affected work
remains blocked while independent read-only/preflight lanes continue.

The coordinator's complete read order, matching `ORCHESTRATOR_GOAL.md`
exactly, is:

1. repository `AGENTS.md`;
2. `README.md`;
3. `PROGRAM.md`;
4. `COMMON_REQUIREMENTS.md`;
5. `END_STATE.md`;
6. `END_STATE_ACCEPTANCE.json`;
7. `ACCEPTANCE_ARTIFACTS.json`;
8. `REFERENCE_PROFILE.md`;
9. `BETTER_AUTH_PARITY.md`;
10. `PARITY_DISPOSITIONS.json`;
11. `API_OPERATIONS.md`;
12. `OPERATION_SEMANTICS.json`;
13. `PUBLIC_CONTRACTS.json`;
14. `public_contracts.rb`;
15. `UPSTREAM_DISPOSITIONS.md`;
16. `UPSTREAM_SURFACE.json`;
17. `PROTOCOL_BASELINES.md`;
18. `PROTOCOL_CONFORMANCE_MANIFEST.json`;
19. `SECURITY_EVENTS.md`;
20. `TRANSACTION_CONTRACT.md`;
21. `LIFECYCLE_CASCADES.md`;
22. `LIFECYCLE_CONSUMERS.md`;
23. `REFERENCE_CONFIGURATION.md`;
24. `CONFIGURATION_CATALOGS.json`;
25. `VERIFICATION_APPLICABILITY.json`;
26. `PREFLIGHT_EVIDENCE.md`;
27. `DEPENDENCIES.md`;
28. `INVENTORY.md`;
29. `EXECUTION_LEDGER.md`;
30. `WORKER_PROMPT.md`;
31. `GOAL_MANIFEST.json`;
32. the exact goal assigned to that worker.

## Computed execution waves

These waves are the longest-path depths of the authoritative `Requires` DAG.
Units in one wave MAY execute concurrently as soon as their own prerequisites
are `verified`; a wave is not a barrier.

### Wave 0

- `primitive/authentication-identity-contracts`
- `primitive/authorization-identity-contracts`
- `primitive/capability-identity-contracts`
- `primitive/identifier-identity-contracts`
- `primitive/password-secret-contracts`
- `identity/delivery`

### Wave 1

- `identity`

### Wave 2

- `identity/postgres`
- `identity/session`
- `identity/risk`
- `webauthn`
- `identity/i18n`

### Wave 3

- `primitive/capability-postgres-identity-contracts`
- `identity/session/postgres`
- `identity/session/valkey`
- `identity/delivery/postgres`
- `identity/risk/postgres`
- `identity/risk/valkey`
- `identity/risk/captcha`
- `identity/risk/hibp`
- `identity/otp`
- `identity/anonymous`
- `webauthn/postgres`
- `passkey`
- `identity/oauth`
- `identity/impersonation`
- `organization`
- `oauth-server`

### Wave 4

- `identity/risk/captcha/recaptcha`
- `identity/risk/captcha/turnstile`
- `identity/risk/captcha/hcaptcha`
- `identity/risk/captcha/captchafox`
- `identity/password`
- `identity/email`
- `identity/otp/postgres`
- `identity/anonymous/postgres`
- `identity/mfa`
- `passkey/postgres`
- `identity/oauth/postgres`
- `identity/oauth/providers`
- `identity/oauth/proxy`
- `identity/impersonation/postgres`
- `organization/postgres`
- `identity/apikey`
- `sso`
- `scim`
- `oauth-server/oidc`
- `oauth-server/device`

### Wave 5

- `identity/password/postgres`
- `identity/username`
- `identity/magiclink`
- `identity/phone`
- `identity/mfa/postgres`
- `identity/oauth/onetap`
- `identity/apikey/postgres`
- `identity/apikey/valkey`
- `sso/domain-verification`
- `sso/oidc`
- `sso/oauth2`
- `sso/saml`
- `sso/postgres`
- `scim/organization`
- `oauth-server/postgres`

### Wave 6

- `scim/postgres`
- `identity/http`

### Wave 7

- `identity/reference`

### Wave 8

- `identity/identitytest`

## Readiness and completion

Only dependency-free units begin `ready`. The coordinator changes a proposed
unit to `ready` only after all its prerequisites are integrated and `verified`.
Implementation on a worker branch is not verification. Program success is only
the single exhaustive completion predicate in `PROGRAM.md`; partial summaries
or a green structural validator do not weaken it. A non-success stop is
permitted only by the blocked predicate in `ORCHESTRATOR_GOAL.md` after every
independent progress lane is exhausted.
