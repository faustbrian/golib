# Module Lifecycle

## Create

New public modules use `pkg/<name>` and
`github.com/faustbrian/golib/pkg/<name>`. Define ownership, non-goals, public
contract, dependency direction, lifecycle status, semantic tag prefix, gates,
license, changelog, security posture, documentation, and release proof. Run
`make manifests` and ensure changed selection includes the module.

## Rename

Rename directory, module path, imports, owned requirements, tags, API baselines,
docs, examples, generated files, provenance, and catalog entries atomically.
Because the previous modules had no consumers, the initial consolidation does
not retain misleading compatibility modules. Released paths require a separate
migration and deprecation plan.

## Split Or Merge

Require evidence that responsibilities or dependency costs justify the change.
Define which module owns each API and state transition, release in dependency
order, provide migration examples, and preserve protocol/persistence behavior.

## Retire

Deprecate first according to `DEPRECATION.md`, stop new dependants, publish a
final supported release, preserve security guidance and source provenance, and
mark the catalog lifecycle. Fixtures and harnesses must never be released.
