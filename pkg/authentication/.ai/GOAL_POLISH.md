# Goal: Polish Authentication Package Boundaries

## Objective

Verify and document the intended repository, package, and nested-module
boundaries after the original authentication implementation and hardening work.
Do not modify the historical `GOAL.md` or `GOAL_HARDEN.md`.

## Required Work

- Keep Basic, opaque bearer, API-key, `authhttp`, and test support as ordinary
  root-module subpackages because they are cohesive and standard-library-first.
- Keep JWT/JWK, OIDC, and OpenTelemetry in optional nested Go modules so their
  dependency graphs and compatibility are opt-in.
- Add `authotel` to current package maps and adoption documentation where the
  implemented API supports it.
- State that JWT is an authentication-token capability while JWX/JOSE is the
  underlying JWK, JWS, JWE, and JWT standards machinery.
- Do not create an owned JWX/JOSE implementation without a separate full-spec
  cryptographic project, security review, and interoperability mandate.
- Keep OIDC validation separate from outbound OAuth2 token acquisition.
- Keep HTTP extraction as an adapter rather than an independent identity
  system.
- Require a material dependency, compatibility, or release boundary before
  adding another nested module; require an independently coherent product
  before adding another repository.

## Verification

- Prove root-only consumers do not acquire JWT, OIDC, JOSE, or OpenTelemetry
  dependencies.
- Test every nested module independently with `GOWORK=off`.
- Verify nested-module tags, compatibility, docs, examples, and changelogs.
- Update architecture, package map, installation, FAQ, and migration docs.

## Completion Criteria

- Package and module boundaries are explicit, dependency-isolated, tested, and
  understandable without source inspection.
- No duplicate authentication repository or local JOSE implementation is
  introduced without a separately approved goal.

