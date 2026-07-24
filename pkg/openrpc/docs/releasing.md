# Releasing

1. Confirm the worktree is clean and the release commit is reviewed.
2. Run `make check-all` with the pinned minimum Go version.
3. Review `CHANGELOG.md`, supported versions, conformance matrices, dependency
   licenses, vulnerability output, API compatibility, and benchmark evidence.
4. Confirm every workflow uses immutable action commits and least-privilege
   permissions.
5. Create a signed semantic-version tag only through the release workflow.
6. Build source artifacts from the tag, generate checksums and provenance, and
   publish release notes from the reviewed changelog.
7. Verify the module proxy resolves the tag and a clean consumer can build it.

Do not release while coverage, mutation, conformance, vulnerability, docs, API,
or integration gates are waived. A failed optional NilAway advisory must remain
visible even when it is not blocking.
