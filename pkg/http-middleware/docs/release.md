# Release process

1. Run `make check-all` on a clean tree with Go 1.26.5.
2. Confirm architecture restrictions, 100.0% production coverage, all declared
   mutants killed, fuzz smoke, race, real HTTP integration, docs, API
   compatibility, lint, Staticcheck, and vulnerability checks.
3. Review `go list -m all`, `NOTICE`, third-party licenses, threat model,
   benchmark output, and changelog.
4. Update `api/baseline.txt` only for intentional reviewed API changes.
5. Create a signed annotated semantic-version tag.
6. Let the release workflow repeat all gates, build a source archive and
   checksum, sign their Sigstore provenance attestation, and publish them.
   Never bypass failed verification.

Repository topics should include `go`, `golang`, `http`, `middleware`,
`net-http`, `security`, and `observability`.
