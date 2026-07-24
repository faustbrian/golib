# Repository standards

Suggested repository topics are: `go`, `golang`, `localization`, `bcp47`,
`internationalization`, `immutable`, `jsonb`, and `postgresql`.

Branching, staging, commit, and verification rules are defined in `AGENTS.md`.
Dependabot tracks Go modules and pinned Actions. Every blocking workflow has a
README badge. Repository settings should require the CI quality/lint and
PostgreSQL/mutation jobs appropriate to the release branch only after those
workflows are enabled by the owner.
