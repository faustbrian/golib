# Security policy

## Supported version

Security fixes are applied to the current unreleased line until the first
tagged release establishes a maintained version policy.

## Reporting

Report suspected vulnerabilities privately through the repository's GitHub
security advisory flow. Do not include credentials, production identifiers, or
customer data in an issue, fixture, event, snapshot, or benchmark artifact.

## Model

The package processes only numeric configuration, weights, contexts, and a
caller-supplied observer. Configuration and queues are bounded. It performs no
network, filesystem, process, environment, reflection, or unsafe operation.
Observer callbacks are untrusted: they run outside accounting locks and panics
are recovered. Callers remain responsible for releasing permits and bounding
the work protected by each permit.
