# Security

## Trust boundary

Keys, values, compact paths, encoded nodes, proof nodes, storage responses, and
fixture updates are untrusted. Stores may omit, corrupt, substitute, replay, or
partially persist nodes. Contexts, explicit byte/count/depth/hash/read/write
limits, and checked arithmetic bound work before conversion or allocation.

## Threats

The implementation must defend against profile or double-hash confusion; RLP
and compact-path malleability; the 31/32-byte reference boundary; proof
substitution, truncation, reordering, duplication, and surplus nodes; cycles
and path amplification; corrupt storage; stale or partial root publication;
unsafe pruning across shared roots; preimage disclosure; integer and allocation
overflow; CPU, memory, storage-read, and disk amplification; slice aliasing and
concurrent mutation; secret leakage in errors; and compromised dependencies or
fixtures.

Errors expose bounded metadata and typed causes, never complete keys, values,
nodes, proofs, preimages, or credentials.

## Proof limitation

A valid proof establishes a key/value, absence, or exact range-completeness
claim under the supplied root. It does not establish that the root is
canonical, finalized, recent, or authorized.
