# Frequently asked questions

## Why are rules advisory by default?

Fixture precision is not corpus precision. Blocking promotion requires clean,
classified results across every owned repository plus migration and performance
evidence.

## Why does the suite not report lost cancellation or broad interfaces?

`go vet` `lostcancel` and golangci-lint `interfacebloat` already own those
semantics. The suite adds only concrete organization-policy gaps and records
canonical authority in its conflict matrix.

## Why does vettool ignore my YAML policy?

The standard vettool protocol has no repository configuration channel. Use the
standalone `check -config` command for governed analysis.

## Can I suppress a whole file or package?

No. Suppress the exact diagnostic location with the exact rule ID and a reason,
or add a reviewed exact policy exception when the contract is configuration
owned.

## Does generated.go count as generated code?

Not by filename. Exclusion requires explicit policy and the recognized Go
generated header before the package declaration.

## Does a clean run prove the program is safe?

No. Keep the compiler, vet, Staticcheck, golangci-lint, gosec, govulncheck,
CodeQL, race tests, fuzzing, NilAway, and human review in their documented roles.
