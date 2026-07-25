# Goal: Secure the Software Supply Chain

## Mission

Make source, dependencies, fixtures, CI automation, tools, and releases in
`golib` reproducible, auditable, minimally privileged, and resistant to
dependency or workflow compromise.

## Dependency Governance

- Inventory every direct, indirect, tool, test, fixture, and action
  dependency by module.
- Require a documented purpose, license, maintenance status, security history,
  and replacement cost for new direct dependencies.
- Prefer the standard library when it is correct and maintainable.
- Keep optional integrations isolated so core consumers do not inherit their
  dependency graphs.
- Pin reproducible tool and workflow versions.
- Verify `go.mod`, `go.sum`, module proxies, checksums, and vendored or embedded
  upstream assets.
- Detect stale, retracted, vulnerable, abandoned, or unexpectedly replaced
  dependencies.
- Use one root dependency-update system and prevent duplicate bot conflicts.

## Upstream Fixtures And Specifications

Official specifications, schemas, test suites, corpora, timezone data, and
other vendored evidence MUST record source URL, revision, checksum, license,
local modifications, and update procedure. Upstream updates MUST not silently
remove cases or weaken conformance.

## Workflow Security

- Use least-privilege workflow permissions.
- Pin third-party actions according to the documented policy.
- Review action provenance and maintenance.
- Prevent untrusted pull requests from accessing secrets or privileged
  runners.
- Avoid script injection through branch names, paths, matrix values, release
  notes, and issue content.
- Separate build, test, release, and administrative permissions.
- Validate workflows locally with maintained tooling.
- Require explicit approval for publishing, tagging, repository archival, or
  protection changes.

## Release Integrity

- Release only from reviewed, clean commits.
- Generate dependency manifests, checksums, SBOMs, and provenance where they
  add consumer value.
- Sign or attest released binaries and artifacts.
- Verify module tags map to the intended directory and commit.
- Rebuild released commands in a clean environment and compare expected
  outputs where reproducible builds are practical.
- Test published modules through normal Go resolution with `GOWORK=off`.
- Never publish fixture modules or local replacement directives.

## Licensing And Notices

Verify every module and imported asset has an accurate compatible license.
Preserve Apache NOTICE obligations, third-party license text, copyright
attribution, and generated-code provenance. A root license MUST not silently
relicense differently licensed modules.

## Incident Response

Document how to revoke compromised releases, rotate credentials, replace
actions or dependencies, publish advisories, and communicate affected module
versions without deleting evidence or rewriting history.

## Completion Criteria

This goal is complete when dependencies and assets have provenance, workflows
are least-privileged and injection-resistant, releases are traceable and
verifiable, licenses are accurate, and no unreviewed supply-chain exception
remains.
