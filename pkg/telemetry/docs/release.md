# Release process

## Preconditions

- `CHANGELOG.md` contains every user-visible change.
- Compatibility, upgrade, security, and operations docs match behavior.
- `make check`, `make race`, `make fuzz`, `make benchmark`, lint, and
  `govulncheck` pass.
- The Go/OpenTelemetry matrix and all GitHub Actions gates are green.
- No cardinality, privacy, race, leak, deadlock, timeout, or unbounded-resource
  blocker remains.

## Versioning

Use semantic versioning. Before v1, call out configuration/API changes clearly.
After v1, changing exported types, defaults, metric names/units/attributes,
resource identity, propagation policy, or error behavior is a compatibility
change.

## Tagging

Create a signed `v*` tag only from a verified commit. The release workflow
reruns all gates and creates GitHub release notes from the verified tag. Never
use a tag to bypass a failing branch check.

## Post-release

Build the service and worker examples against the tag, confirm module proxy
availability, inspect release notes and documentation links, and monitor early
adopters for exporter failures, memory, cardinality, and compatibility issues.

Move released entries from `Unreleased` to a dated version and create a new
empty `Unreleased` section in the same release change.
