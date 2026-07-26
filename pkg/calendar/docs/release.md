# Release process

1. Use a clean tree and the pinned Go 1.26.5 toolchain.
2. Update [CHANGELOG.md](../CHANGELOG.md) and version guidance.
3. Run `make check-all`; inspect advisory NilAway output.
4. Run the hosted matrix for OS/timezone and PostgreSQL 14–18.
5. Compare the generated API baseline and benchmark history.
6. Create a signed semantic version tag and let the release workflow publish
   source archives and checksums.

No release proceeds with a surviving curated mutation, coverage below 100.0%,
an unexplained timezone corpus change, missing provenance, or a skipped blocking
gate.
