# JSON-RPC URL Contract Input-Digest Migration

The `pkg/jsonrpc` operational-assurance input changed from
`fab796f79ca30169073eef898518de6177b69225d5f21b5c13083f56956c1901`
to
`cd8dad00f351b4a344c5bdc0dc45373e7924576984fc6100519327bbd282b5d3`.

The reviewed change replaces obsolete standalone-repository URLs in the
package changelog and updates the documentation contract test to require the
monorepo-canonical history and release URLs. Production code, protocol tests,
transport behavior, public APIs, dependencies, and the JSON-RPC behavior
exercised by retained operational-assurance scenarios did not change.

The package documentation gate and documentation contract test passed with
the replacement input. The prior operational observations therefore remain
behaviorally identical. This record authorizes only the exact one-way digest
transition above; it does not relabel the original execution time, broaden the
original claim, or authorize a future input digest.
