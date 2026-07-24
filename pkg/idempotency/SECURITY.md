# Security policy

## Supported versions

The project is pre-v1. Only the latest released version receives security
fixes. Unreleased commits on `main` are development snapshots and are not a
supported production channel.

## Reporting a vulnerability

Report vulnerabilities privately through the repository's GitHub Security
Advisories page. Do not open a public issue with exploit details, production
keys, request payloads, database records, or provider delivery data.

Include the affected version, adapter or integration, deployment assumptions,
reproduction steps, impact, and any proposed mitigation. You should receive an
initial acknowledgement within seven days. Disclosure timing will be
coordinated after a fix and supported release are available.

## Security model

The package does not provide authentication, authorization, payload signature
verification, encryption, secret storage, or exactly-once execution. Callers
must authenticate tenants and callers before constructing keys, verify webhook
signatures before deduplication, bound untrusted payloads, protect datastore
credentials, and apply fencing or application invariants at side-effect commit.

Idempotency keys, results, and metadata may contain sensitive identifiers. Hash
keys before logging, avoid raw values in metric labels, restrict datastore
access, encrypt transport and storage, and choose the shortest useful retention.

Raw logical key fields, tenant and caller identifiers, fingerprints, owner and
fencing tokens, payloads, replay results, and metadata must not be emitted to
logs, metric labels, or traces. The supplied observers expose bounded semantic
fields only. When correlation is necessary, use `NewHMACKeyHasher` with a
separately managed secret and keep its digest out of metric labels.

See the [threat model](docs/threat-model.md) for assets, trust boundaries,
failure threats, and residual obligations. Resource ceilings are listed in the
[resource budgets](docs/resource-budgets.md).
