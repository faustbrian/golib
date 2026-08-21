# OA OpenSearch Rolling Recovery Evidence

Observed at `2026-08-13T11:16:19Z` on Darwin/arm64 with Go 1.26.5 and
Docker Engine 29.6.2. The campaign used task-owned disposable Go caches and
Docker resources labelled with a unique run identity; all resources were
removed when the campaign completed.

The pinned two-node campaign ran OpenSearch 2.19.6 at image digest
`sha256:8690b204fe914c60ca76d451ac73bc0481e034d32d3779944c8caca56a2b003f`
and OpenSearch 3.8.0 at image digest
`sha256:bcc1797519726ceb6d651d4a3e60b7c30da91793914a8dfe75fd441d4f641509`.
The exercised script had SHA-256
`f189b14ddc2c5ad163a5b2d1bd65944e82d154cf3c394b4327d5d5608a4c79d1`.

The campaign passed all of these observable contracts:

- both old-version nodes served the seeded fixture and rotated requests;
- loss and recovery of one endpoint stayed within the failover budget;
- complete cluster outage returned bounded failures rather than hanging;
- recovery reconciled an unknown write outcome before fixture verification;
- replacing the first node with 3.8.0 preserved data and allowed old/new
  mixed-version traffic and conformance checks;
- replacing the second node completed the rolling upgrade while preserving
  fixture data and endpoint rotation; and
- both final nodes remained within the campaign's CPU, memory, PID, and file
  descriptor limits.

This is real local OpenSearch failure, recovery, mixed-version, and rolling
upgrade evidence. It does not prove managed OpenSearch failover, production
network behavior, snapshot restore, a full source-of-truth rebuild, or an ECS
deployment. The associated operational-assurance scenarios therefore remain
pending.
