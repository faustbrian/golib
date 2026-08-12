# Identity Platform Orchestration Entry Point

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

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

The coordinator alone owns `INVENTORY.md`, `EXECUTION_LEDGER.md`,
`DEPENDENCIES.md`, files directly
under this directory, planning-goal moves, root manifests, integration, and
final end-state proof. A worker owns only its assigned canonical package
directory and package-local code, tests, fixtures, migrations, docs, examples,
module files, and changelog. The coordinator MUST reject cross-package worker
edits instead of resolving semantic ownership during a merge.

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
- `webauthn`

### Wave 2

- `identity/postgres`
- `identity/session`
- `identity/risk`
- `identity/i18n`

### Wave 3

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
- `identity/phone`
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
- `identity/mfa/postgres`
- `identity/oauth/onetap`
- `identity/apikey/postgres`
- `identity/apikey/valkey`
- `sso/domain-verification`
- `sso/oidc`
- `sso/oauth2`
- `sso/saml`
- `scim/organization`
- `oauth-server/postgres`

### Wave 6

- `sso/postgres`
- `scim/postgres`
- `identity/http`

### Wave 7

- `identity/reference`

### Wave 8

- `identity/identitytest`

## Readiness and completion

Only dependency-free units begin `ready`. The coordinator changes a proposed
unit to `ready` only after all its prerequisites are integrated and `verified`.
Implementation on a worker branch is not verification. Program completion
requires all 61 identity-platform units and all six primitive-extension
prerequisite units verified (67 schedulable units total), every in-scope parity row proved, every
`END_STATE.md` journey passing without undocumented application glue, and all
final repository gates current.
