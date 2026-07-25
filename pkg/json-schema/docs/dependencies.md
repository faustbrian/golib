# Dependency review

## `golang.org/x/net/idna`

- Purpose: IDNA2008, UTS #46 mapping, registration validation, bidi rules,
  contextual rules, Punycode canonicalization, and DNS-length enforcement for
  `hostname`, `idn-hostname`, `email`, URI, and IRI formats.
- Necessity: the standard library does not provide equivalent IDNA processing;
  a local table implementation would duplicate security-sensitive Unicode
  standards and update work.
- Ownership and maintenance: maintained by the Go project.
- License: BSD 3-Clause; recorded in `NOTICE`.
- Security: covered by dependency review, `govulncheck`, hostile official IDN
  fixtures, and the package's format budgets.
- Replacement: prefer a standard-library implementation if one becomes
  available with equivalent conformance; otherwise update deliberately after
  reviewing Unicode and behavior changes.

`golang.org/x/text` is a transitive requirement of `x/net/idna` under the same
Go project maintenance and BSD license model.

## `github.com/dlclark/regexp2/v2`

- Purpose: execute schema patterns with ECMAScript lookaround,
  backreferences, character classes, and Unicode behavior unavailable in
  Go's RE2 engine.
- Necessity: JSON Schema defines its pattern syntax in terms of ECMA-262;
  translating only the RE2-compatible subset creates observable validation
  divergences.
- Ownership and maintenance: an independently maintained pure-Go project,
  pinned by `go.mod` and reviewed on update.
- License: MIT; recorded in `NOTICE`.
- Security: every compiled expression has a caller-configurable backtracking
  stack bound and match timeout. Pattern count and bytes are bounded before
  compilation.
- Replacement: prefer a standard-library ECMAScript engine if one becomes
  available with equivalent Unicode and resource-limit behavior.

No dependency may add implicit network work or a mutable behavior registry.

## Automated review

`make dependencies` verifies module content, tidiness, and the complete build
graph. `make license` checks dependency licenses with a pinned `go-licenses`,
and `make secrets` scans the package tree with a pinned, redacting Gitleaks.
`make workflows` validates the owned workflows and rejects mutable action
references, missing top-level permission declarations, and
`pull_request_target`. Pull requests also run GitHub's dependency-review
action at an immutable commit. `make supply-chain` runs the dependency,
license, and secret gates together.
