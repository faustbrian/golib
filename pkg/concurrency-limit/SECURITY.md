# Security

Report vulnerabilities privately to the repository maintainers. Do not include
credentials, customer identifiers, payloads, or production traces in reports.

The limiter retains only bounded numeric aggregates and configured partition
keys. It never retains operation results, errors, contexts, credentials, or
request bodies. Partition membership and priority authorization must be decided
before calling this package. Treat observer implementations as trusted local
code and keep emitted metrics free of caller-controlled high-cardinality
labels.

The module has no network, storage, unsafe, cgo, reflection-based wiring, or
background-worker dependency. Supply-chain verification is performed by the
repository vulnerability, license, SBOM, secret, and clean-consumer gates.
