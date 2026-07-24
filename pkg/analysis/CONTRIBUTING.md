# Contributing

## Propose one semantic policy

Start with a short design note containing the stable rule ID, owner, category,
severity, rationale, remediation, configuration shape, exact diagnostic
location, and proof needed to report. Compare the proposed semantics with every
tool in [the conflict matrix](docs/governance.md). A different message or
package-specific severity does not justify duplicating a mature analyzer.

Describe likely false positives before implementation. State what the analyzer
will deliberately accept when evidence is incomplete. Prefer missing a finding
to guessing about execution, ownership, identity, or data flow.

## Develop test first

Add `analysistest` fixtures that fail because the intended diagnostic is absent.
Every diagnostic needs rejected and accepted cases, a near miss, aliases,
generics when applicable, build-tag behavior, generated-code behavior, and
multi-package evidence. Add internal tests for malformed or otherwise
unreachable analysis states.

Implement with syntax, `types.Info`, facts, or bounded control/data flow. Do not
identify APIs by textual spelling. Resolve the declared object so aliases, dot
imports, methods, and generic instantiations have the same semantics.

Suggested fixes are appropriate only when the transformed program preserves
meaning without guessing. A rule is complete without a fix.

## Integrate a rule

Export `Rule` and `Analyzer` from one cohesive analyzer package. Configurable
rules also expose typed `Options` and `New`; invalid, duplicate, overlapping, or
unbounded policy is rejected in `New`. Register the rule in the raw
multichecker, configured driver, policy registry, shared fuzz harness, aggregate
benchmark, mutation script, rule catalog, configuration example, and command
tests.

Run `make compatibility-update` only after reviewing an intentional public or
rule-contract change and documenting its migration impact. Never refresh a
baseline merely to make the check green.

## Prove precision and cost

Run the focused analyzer tests during development, then:

```sh
make check
make race
make benchmark
make nilaway
```

`make check` includes formatting, docs, compatibility, reproducible builds,
vet, tests, meaningful 100% production statement coverage, vettool execution,
Staticcheck, strict golangci-lint, govulncheck, actionlint, fuzz smoke, and
mutation testing. The mutation runner covers shared configuration, driver,
governance, and every shipped analyzer package; new diagnostic decisions need
zero surviving and zero uncovered mutants. Record benchmark changes and justify
allocation-budget increases with measured analyzer work.

Evaluate the complete owned repository corpus before recommending blocking
status. Classify every finding and retain stable expected advisories. A local
fixture suite is not corpus precision evidence.
