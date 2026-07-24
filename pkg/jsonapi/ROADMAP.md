# Roadmap

## First stable release

- complete and publish open-source licensing;
- keep the JSON:API 1.1, Atomic Operations, and Cursor Pagination matrices
  fully verified;
- complete CI, security, documentation, and tagged-release automation;
- publish benchmark baselines and compatibility promises;
- resolve any API feedback found during real service adoption;
- tag `v1.0.0` only when no known compliance gap remains.

## After core stabilization

Potential work is evaluated independently and is not promised for `v1`:

- OpenAPI or JSON Schema helpers;
- optional client helpers;
- router middleware packages outside the transport-neutral core;
- opt-in code generation for explicit resource mappings;
- additional registered extensions and profiles.

The core will not add ORM integration, framework controllers, authentication
policy, or a project-specific filter language.
