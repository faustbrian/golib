# OA OpenSearch Version Matrix Evidence

Observed through `2026-08-13T11:54:28Z` on Darwin/arm64 with Go 1.26.5 and
Docker Engine 29.6.2. The campaign used task-owned disposable Go caches,
benchmark output, snapshot directories, and uniquely labelled Docker
resources. All disposable resources were removed after the successful run.

The pinned matrix ran OpenSearch 2.19.6 at image digest
`sha256:8690b204fe914c60ca76d451ac73bc0481e034d32d3779944c8caca56a2b003f`
and OpenSearch 3.8.0 at image digest
`sha256:bcc1797519726ceb6d651d4a3e60b7c30da91793914a8dfe75fd441d4f641509`.
The exercised script had SHA-256
`3d47bb7ff616c7a0b244edabd81a490dcc01a6673d810b59c5181bda3425402a`.

Both engine versions passed the same real-backend contracts:

- shared search semantics and generated-DSL differential checks;
- bounded load under one CPU, 1 GiB memory, 512 PIDs, and 1,024 file
  descriptors;
- snapshot creation, deletion, restore, and restored-content verification;
- durable stale-write rejection after backend delete-version garbage
  collection;
- partial-shard diagnostics, partial bulk outcomes, point-in-time expiry,
  cluster-block recovery, malformed response rejection, and ambiguous applied
  write reconciliation;
- mixed application protocol-version operation;
- rebuild, reconciliation, rollback, cleanup safety, and concurrent-application
  behavior; and
- constrained real-network benchmarks against direct use of the official
  OpenSearch client with equivalent fixtures and operations.

The benchmark campaign executed ten samples of twenty operations for each
equivalent indexing, query, bulk, offset-pagination, and cursor-pagination
track on both versions. Benchmark evidence validation and cross-version
`benchstat` comparison passed; the aggregate allocation geomean changed by
1.25% from 2.19.6 to 3.8.0. The benchmark is comparative evidence, not a
production capacity claim.

This is real local OpenSearch interoperability, recovery, compatibility, and
bounded-resource evidence. It does not prove a multi-hour soak, managed
OpenSearch failover, prolonged overload or network partition behavior,
storage exhaustion, a production-sized application-owned live source-of-truth
rebuild, or ECS deployment. The associated
operational-assurance scenarios therefore remain pending.
