# OA OpenSearch Security Matrix Evidence

Observed through `2026-08-13T12:03:19Z` on Darwin/arm64 with Go 1.26.5 and
Docker Engine 29.6.2. The campaign used task-owned disposable Go caches,
random per-run credentials, copied test CA material, and uniquely labelled
Docker resources. All disposable resources and credential material were
removed after the successful run.

The pinned security matrix ran OpenSearch 2.19.6 at image digest
`sha256:8690b204fe914c60ca76d451ac73bc0481e034d32d3779944c8caca56a2b003f`
and OpenSearch 3.8.0 at image digest
`sha256:bcc1797519726ceb6d651d4a3e60b7c30da91793914a8dfe75fd441d4f641509`.
The exercised script had SHA-256
`17b1134a227032edc694c1d8a7904cf6b122f2d7dd8fb34792ac6bf9d6a177fb`.

Both engine versions passed the same observable security contracts:

- peer-verified TLS succeeded with the pinned test CA and failed closed with
  an untrusted root;
- a runtime role could read and write only its authorized tenant alias;
- cross-tenant reads, cross-tenant writes, cluster health, and security
  administration returned HTTP 403 for the runtime principal;
- operator credentials rotated without reconstructing the adapter;
- changing the resolved dial target moved the client to a distinct secured
  cluster while retaining TLS verification and expected version checks;
- operator health and capacity access remained available after rotation;
- replica-induced degraded health was detected and returned to the baseline
  after recovery; and
- both secured nodes stayed within the campaign's CPU, memory, PID, and file
  descriptor limits.

This is real local TLS, least-privilege, tenant-isolation, credential-rotation,
DNS-change, and recovery evidence. It does not prove production PKI, managed
OpenSearch IAM, live secret distribution, production DNS, or an incident
response drill. The associated operational-assurance scenarios therefore
remain pending.
