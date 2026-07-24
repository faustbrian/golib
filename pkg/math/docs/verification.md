# Verification

`make check` runs formatting, module tidiness, vet, architecture policy, tests,
race detection, exact production coverage, fuzz smoke, mutation checks,
examples, consumer compatibility, benchmarks, docs, API compatibility, lint,
static analysis, and vulnerability analysis. `make check-all` additionally
runs advisory NilAway.

The test suite includes algebraic laws, direct `math/big` comparisons,
independent `apd` and `shopspring/decimal` comparisons, applicable General
Decimal Arithmetic vectors, serialization round trips, alias checks, fuzz
targets, and concurrent use. Official vector provenance is recorded under
`specification/gda`.

The GDA harness executes and exactly accounts for 3,547 applicable vectors and
1,542 explicit skips across addition, subtraction, multiplication, division,
quantization, and rounding files. Non-finite values, unsupported conditions,
and non-extended operand pre-rounding are outside the finite extended-decimal
contract. Any accounting drift fails the suite.

Mutation testing uses Gremlins v0.6.0 over every production package and requires
both 100% test efficacy and 100% mutant coverage. Provenance checks verify every
vendored vector checksum before the release gate proceeds.
