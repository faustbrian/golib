# Verkle Tree Specification Decisions

This register records the package-owned profile and interoperability boundary
used by `verkle-tree`. It complements the normative
[pre-v1 profile](../specification/bandersnatch-ipa-256-v0.md), the
[profile-freeze decision](../specification/profile-freeze.md), and the bounded
[compatibility matrix](compatibility.md).

## VERKLE-DEC-001: Pre-v1 profile identity and interoperability boundary

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `verkle-tree` maintainers |
| Source | The package-owned profile is informed by the pinned [Ethereum Verkle EIPs](https://github.com/ethereum/EIPs/tree/c55786f4242e5324afd14c6bca890a369a771d7f), `ethereum/go-verkle`, `crate-crypto/rust-verkle`, and their recorded provenance. |
| Classification | Package profile and defensive interoperability policy |
| Issue | The reviewed Ethereum proposals are not a stable protocol profile, while the maintained reference implementations have different scope, maintenance status, APIs, and encodings. Treating those sources as one caller-composable or Ethereum-compatible profile would overstate the available evidence. |
| Credible interpretations | Freeze a stable Ethereum profile, expose independently configurable cryptographic components, or retain one immutable package-owned pre-v1 profile whose external claims are limited to pinned corpora. |
| Known peer behavior | The pinned Go implementation supplies a maintenance-mode tree and aggregate-proof corpus. The pinned Rust implementation independently supplies encoding, generator, commitment, proof, topology, tree-root, and transition corpora. Agreement is limited to those recorded artifacts. |
| Selected behavior | `verkletree-bandersnatch-ipa-256-v0` is the only implemented profile identity. Its width, key layout, curve and group, generator set, transcript, encodings, and state semantics are not caller-composable. It remains pre-v1 and makes no stable-v1, production-suitability, external-audit, or Ethereum-protocol compatibility claim. |
| Security and resource consequences | Rejecting unknown or inconsistent profiles before cryptographic work prevents cross-profile replay and attacker-selected component combinations. Existing profile limits bound supported key, value, proof, witness, and cryptographic work surfaces; this registration adds no new runtime input. |
| Compatibility and wire consequences | Canonical package bytes include the package profile identity and are compatible only with the exact documented package contract. Pinned corpus agreement does not create general Go, Rust, Ethereum, persistence, API, or wire compatibility. |
| Executable evidence | `TestProfileIsExactAndRejectsOtherValues`, `TestRustVerkleEncodingVectors`, `TestRustVerkleGeneratorSet`, `TestCommitmentEngineMatchesPinnedRustVectors`, `TestBuildMatchesPinnedRustTreeRoots`, and `TestStatelessUpdaterMatchesPinnedRustRebuiltTransitions` cover the selected identity and bounded cross-implementation artifacts. |
| Public surface | `Profile`, `BandersnatchIPA256V0`, roots, snapshots, proofs, witnesses, and their canonical encodings |
| Upstream record | Ethereum EIPs 4762 and 7748 are Draft and EIPs 6800 and 7612 are Stagnant at the pinned EIPs revision; neither reference implementation supplies a stable generic package contract. |
| Reconsider when | A deliberately versioned stable package profile or revision-pinned Ethereum profile has complete normative, independent interoperability, hostile-input, and release evidence. |

## Unresolved and excluded behavior

No known profile-identity or interoperability-boundary decision is unresolved.
This register does not close complete normative profile freeze, stable-v1,
production suitability, external audit, general reference-client compatibility,
or Ethereum protocol compatibility; those remain release blockers or explicit
non-claims until separately decided and evidenced.
