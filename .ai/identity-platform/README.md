# Identity Platform Orchestration Entry Point

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Give one goal to one orchestrator

Do not manually distribute the package goals. Give one coordinator agent this
exact prompt:

```text
Execute /Users/brian/Developer/go-libraries/.ai/identity-platform/ORCHESTRATOR_GOAL.md
to completion. Act only as coordinator. Use gpt-5.6-sol subagents with medium
reasoning for package implementation, maximize safe DAG parallelism, commit
coherent verified batches locally, do not push, and stop only at the completion
or blocker conditions defined by the goal.
```

The coordinator reads `ORCHESTRATOR_GOAL.md`, renders `WORKER_PROMPT.md`, and
creates one isolated worker for each eligible inventory unit. Humans MUST NOT
copy individual goal files into ad-hoc prompts because that omits common,
parity, integration, and dependency requirements.

## Authority and ownership

The coordinator alone owns `INVENTORY.md`, `DEPENDENCIES.md`, files directly
under this directory, planning-goal moves, root manifests, integration, and
final end-state proof. A worker owns only its assigned canonical package
directory and package-local code, tests, fixtures, migrations, docs, examples,
module files, and changelog. The coordinator MUST reject cross-package worker
edits instead of resolving semantic ownership during a merge.

Read order is: `ORCHESTRATOR_GOAL.md`, `PROGRAM.md`,
`COMMON_REQUIREMENTS.md`, `END_STATE.md`, `BETTER_AUTH_PARITY.md`,
`DEPENDENCIES.md`, `INVENTORY.md`, `WORKER_PROMPT.md`, then the assigned goal.

## Computed execution waves

These waves are the longest-path depths of the authoritative `Requires` DAG.
Units in one wave MAY execute concurrently as soon as their own prerequisites
are `verified`; a wave is not a barrier.

### Wave 0

- `identity`
- `identity/delivery`
- `webauthn`

### Wave 1

- `identity/postgres`
- `identity/session`
- `identity/risk`
- `identity/email`
- `identity/apikey`
- `identity/i18n`
- `organization`

### Wave 2

- `identity/session/postgres`
- `identity/session/valkey`
- `identity/risk/postgres`
- `identity/risk/valkey`
- `identity/risk/captcha`
- `identity/risk/hibp`
- `identity/password`
- `identity/magiclink`
- `identity/otp`
- `identity/anonymous`
- `passkey`
- `identity/oauth`
- `identity/impersonation`
- `organization/postgres`
- `sso`
- `scim`
- `oauth-server`

### Wave 3

- `identity/risk/captcha/recaptcha`
- `identity/risk/captcha/turnstile`
- `identity/risk/captcha/hcaptcha`
- `identity/risk/captcha/captchafox`
- `identity/username`
- `identity/phone`
- `identity/mfa`
- `identity/oauth/providers`
- `identity/oauth/proxy`
- `sso/oidc`
- `sso/oauth2`
- `sso/saml`
- `sso/postgres`
- `scim/postgres`
- `scim/organization`
- `oauth-server/oidc`
- `oauth-server/device`
- `oauth-server/postgres`

### Wave 4

- `identity/oauth/onetap`

### Wave 5

- `identity/http`

### Wave 6

- `identity/identitytest`

## Readiness and completion

Only dependency-free units begin `ready`. The coordinator changes a proposed
unit to `ready` only after all its prerequisites are integrated and `verified`.
Implementation on a worker branch is not verification. Program completion
requires all 48 units verified, every in-scope parity row proved, every
`END_STATE.md` journey passing without undocumented application glue, and all
final repository gates current.
