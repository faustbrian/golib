# Security policy

Report vulnerabilities privately to the repository owner. Do not include
credentials, production payloads, customer identifiers, or arbitrary resource
labels in reports.

Bulkheads are process-local availability controls, not authorization or
distributed coordination. Applications must bound resource identity, partition
count, queue size, wait, execution context, fan-out, retries, hedges, and
maximum replica capacity.
