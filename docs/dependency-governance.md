# Dependency Governance

## One Update Surface

The only dependency-update configuration is
[`/.github/dependabot.yml`](../.github/dependabot.yml). Its recursive Go-module
selection covers the root module and every tracked module below `pkg/`,
including adapters, fixtures, interoperability harnesses, examples, and
benchmarks. GitHub Actions are updated from the same root policy.

Go updates are grouped by dependency name across modules. This keeps one
shared dependency update attributable across all affected manifests instead of
opening divergent package-local pull requests. GitHub Actions updates are
grouped separately. The repository validator rejects a missing or changed
canonical root policy and every nested Dependabot configuration.

Dependabot proposes changes; it does not establish readiness. Every proposal
must pass changed-module selection and affected reverse-dependant gates. Major
updates and security-sensitive minor updates require explicit compatibility,
behavior, and migration review rather than automatic merging.

## New Direct Dependencies

A new direct dependency requires a review that records:

- the exact capability that the standard library and existing owned modules
  cannot provide correctly or maintainably;
- the module or adapter that owns the dependency and why consumers outside
  that boundary must or must not inherit it;
- license and notice obligations;
- maintenance activity, release cadence, ownership concentration, security
  history, transitive graph size, cgo or platform requirements, and network or
  generation behavior;
- replacement and removal cost, including persisted or wire compatibility;
- the smallest pinned version compatible with the repository's version policy;
- tests, interoperability evidence, and rollback needed for adoption.

Optional provider SDKs, broker clients, telemetry integrations, and heavy
format implementations belong in independently releasable adapter modules when
the core contract can remain useful without them.

## Update Review

Review an update against every module changed by its manifests and every
affected reverse dependant. Confirm API and wire compatibility, behavior and
default changes, security advisories, retractions, license or notice changes,
supported Go/platform changes, generated output, fixtures, benchmarks, and
service interoperability. A green compile is not sufficient.

Owned `v0.0.0` requirements are unpublished source-proxy plumbing and are not
external dependencies for Dependabot to publish. Independently released owned
modules move in dependency order through the release process.

## Detection And Response

Mandatory vulnerability, license, SBOM, secret, API, test, race, fuzz,
mutation, conformance, interoperability, and benchmark gates provide the
update evidence appropriate to each module. Scanner output that reports an
unreachable vulnerable dependency still requires an explicit disposition; it
is not deleted merely because no current call path is reachable.

For a compromised, retracted, or abandoned dependency, identify every affected
module and released version, stop release publication, constrain or replace the
dependency, preserve forensic evidence, run the complete affected contract,
and publish advisories and upgrade guidance where consumers were exposed.

Pinned specification corpora, generated sources, service images, repository
tools, and GitHub Actions are governed separately from ordinary `go.mod`
updates because checksum, provenance, license, and conformance-case review are
part of their contract.
