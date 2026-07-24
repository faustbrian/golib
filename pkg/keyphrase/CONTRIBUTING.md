# Contributing

Security and compatibility take priority over convenience. Open an issue before
changing public APIs, generation distributions, word lists, normalization,
entropy claims, or BIP-39 behavior. Never include real secret material in an
issue, fixture, benchmark, or log.

Run `make release-check` before requesting review. New behavior requires a
failing test first, exact error and resource semantics, documentation, fuzz or
property coverage where relevant, and mutation-sensitive assertions. Embedded
list changes require provenance, license, source checksum, transformed
checksum, compatibility notes, and explicit security review.

Commits use Conventional Commits with a prose body. Every line must be at most
72 characters.
