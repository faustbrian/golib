# Hardening evidence

The hardening contract is executable. This table maps each risk class to the
current proof surface; no row is satisfied by statement coverage alone.

| Risk | Executable evidence |
| --- | --- |
| Integer correctness | `integer/differential_test.go`, edge tests, algebra laws |
| Rational correctness | `rational/differential_test.go`, normalization and rounding tests |
| Decimal correctness | `decimal/differential_test.go`, 3,547 exactly accounted GDA vectors |
| Binary-float correctness | `bigfloat/differential_test.go`, accuracy and negative-zero tests |
| Immutability and aliasing | constructor/accessor tests and `encoding/aliasing_test.go` |
| Concurrent reuse | `race_test.go` under `go test -race ./...` |
| Hostile parsing | all six fuzz targets under `scripts/check-fuzz.sh` |
| Canonical encoding | text/JSON tests, binary round trips, canonical-decoder fuzzing |
| Resource bounds | `resource_test.go`, allocation budgets, bounded benchmarks |
| Leak freedom | package-level `goleak` test mains and no-goroutine architecture rule |
| Branch sensitivity | full Gremlins run with 100% efficacy and mutant coverage |
| Interoperability | `apd`, `shopspring/decimal`, `math/big`, and consumer tests |
| API separation | architecture rule forbidding cross-family production interfaces |

## Requirement matrix

| Requirement | Proof |
| --- | --- |
| Signs, zeros, boundaries, and huge magnitudes | family edge suites, retained fuzz corpora, and `resource_test.go` |
| Negative zero | `bigfloat` sign-bit arithmetic, conversion, text, and binary-codec tests |
| Coprime, non-coprime, and repeating fractions | rational normalization, decimal-expansion, and exact-division tests |
| Exact and inexact conversions | integer narrowing, rational expansion, float accuracy, and condition tests |
| Algebraic laws | `laws_test.go` proves exact addition laws and finite-precision counterexamples |
| Differential arithmetic | integer, rational, and float suites compare `math/big`; decimal arithmetic and quantization compare `apd` and Shopspring |
| Decimal vectors | ten pinned GDA files execute 3,547 applicable cases and account for 1,542 unsupported cases |
| Branch mutation | `make mutation` requires 100% efficacy and 100% mutator coverage |
| Constructor and accessor aliasing | every `math/big` constructor/accessor has defensive-copy tests |
| Encoder and decoder aliasing | `encoding/aliasing_test.go` mutates every returned and supplied buffer |
| Shared-value concurrency | `race_test.go` reuses every value and context across arithmetic, conversion, and encoding |
| Text and JSON hostility | family fuzzers cover signs, bases, exponents, separators, Unicode, huge digits, JSON kinds, and trailing tokens |
| Binary hostility | the binary fuzzer accepts only byte-for-byte canonical payloads under decoder limits |
| Precision-loss rejection | JSON always emits strings; narrowing and infinite conversions return explicit errors |
| Work and output bounds | operand preflights, decimal scaling guards, bounded random attempts, allocation budgets, and benchmarks |
| Cancellation and error identity | `resource_test.go` distinguishes context, limit, domain, and arithmetic failures |
| Memory and goroutine retention | allocation budgets, decoder-buffer overwrite tests, `goleak`, and the no-goroutine rule |
| Consumer interoperability | `money` and `measurement` compile and pass against the current API |

`make check-all FUZZ_TIME=5s BENCH_TIME=50ms` is the local release proof. A
release additionally requires a successful run of
`.github/workflows/math-ci.yml` for the exact commit being released; local
parity is not a substitute for that hosted result.

The package has no SQL adapter. Database numeric conversion remains a consumer
policy because database scale, nullability, and driver representations are not
numeric identity. Therefore there is no local SQL aliasing surface to audit.

There are no caches, background workers, mutable numeric globals, unsafe code,
cgo, or ambient random sources. All expensive public operations either operate
within already-bounded values or accept explicit limits; cancellable operations
return context errors without wrapping them as arithmetic or limit failures.
