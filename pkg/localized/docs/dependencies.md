# Dependencies

Core depends on `international/locale` for bounded BCP 47 identity,
canonicalization, parent fallback, and registry provenance. It uses `x/text`
privately for language matching and Unicode normalization. PostgreSQL and
optional wire/config/query packages introduce pgx, wire, config, and
api-query. The HTTP client adapter adds http-client. Versions and
checksums are pinned in `go.mod` and `go.sum`.

Tool commands use explicit versions for govulncheck, NilAway, and Gremlins.
GitHub Actions use full commit SHAs with human-readable version comments.

The pinned `international`, `validation`, and `api-query` commits may
be resolved locally through an ignored Go workspace while awaiting publication;
`go.mod` contains no checkout-relative replacements. Hosted verification runs
only after those pins are published. `make dependency-revisions` rejects pin
drift and tests archived clean commits rather than sibling working trees. See
[compatibility](compatibility.md).
