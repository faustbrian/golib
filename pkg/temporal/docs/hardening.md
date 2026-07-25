# Temporal algebra hardening report

This report records the release evidence for the temporal algebra audit. The
commands are reproducible through the `Makefile`; dated results below were
captured on 2026-07-17 with Go 1.26.5 on Darwin/arm64.

## Bound and relation truth tables

The four bound modes form the complete endpoint-inclusion table:

| Bounds | Include start | Include end |
|---|---:|---:|
| `[)` | yes | no |
| `[]` | yes | yes |
| `()` | no | no |
| `(]` | no | yes |

For every non-empty pair, endpoint ordering selects exactly one relation. The
selection is independent of bounds; all 16 bound pairs are exercised for each
row by `TestAllenRelationsAcrossAllBounds`,
`TestDateAllenRelationsAcrossAllBounds`, and
`TestRelationConveniencePredicatesAcrossAllBounds`.

| Relation | Endpoint ordering | Converse |
|---|---|---|
| before | `a.end < b.start` | after |
| meets | `a.end = b.start` | met-by |
| overlaps | `a.start < b.start < a.end < b.end` | overlapped-by |
| starts | `a.start = b.start`, `a.end < b.end` | started-by |
| during | `b.start < a.start`, `a.end < b.end` | contains |
| finishes | `b.start < a.start`, `a.end = b.end` | finished-by |
| equals | equal starts and ends | equals |
| finished-by | `a.start < b.start`, equal ends | finishes |
| contains | `a.start < b.start`, `b.end < a.end` | during |
| started-by | equal starts, `b.end < a.end` | starts |
| overlapped-by | `b.start < a.start < b.end < a.end` | overlaps |
| met-by | `a.start = b.end` | meets |
| after | `a.start > b.end` | before |

Allen `meets` describes endpoint ordering. The convenience predicates also
classify represented membership at the shared endpoint. For a left interval
ending where the right interval starts, every cell abuts; `B` means borders
(both include the point), and `M` means meets without sharing it.

| Left \ Right | `[)` | `[]` | `()` | `(]` |
|---|---:|---:|---:|---:|
| `[)` | M | M | M | M |
| `[]` | B | B | M | M |
| `()` | M | M | M | M |
| `(]` | B | B | M | M |

Equal instant endpoints are a singleton only for `[]`; other modes are empty.
Discrete date periods additionally become empty when exclusions remove every
date. Reversed endpoints and Allen relations involving an empty period return
typed errors. Converse involution and the uniqueness of all 13 relation values
are separately asserted.

## Set-algebra property report

Deterministic property suites use 1,000 generated triples in each of the
instant, civil-date, and circular daily domains. Instant probes use half-hour
points, date probes use individual dates, and daily probes cover every half
hour. The circular complement suite additionally exhausts every unequal pair
of hourly endpoints under all four bounds (2,208 interval/bound cases).

| Operation | Executable property |
|---|---|
| normalization | stable canonical order, no empty member, no mergeable pair |
| union | exact `A or B` membership; commutative, associative, idempotent |
| intersection | exact `A and B` membership; commutative and associative |
| subtraction/difference | exact `A and not B` membership |
| conservation | `(A intersect B) union (A minus B) = A` |
| complement | `A union complement(A) = FullDay`; intersection is empty |
| complement involution | `complement(complement(A)) = A` |
| gap | exact missing membership with inverted adjacent boundary inclusion |
| immutability | constructor inputs and returned slices cannot alias storage |

Fragment-producing operations check `OutputPeriods` before append and return no
partial value. Inputs are copied before normalization, and all public slice
accessors return copies. The maximum fragment count is therefore bounded even
for adversarial alternating inputs.

## Iteration, parsing, and resource budgets

| Resource | Hard maximum and zero-value default |
|---|---:|
| parser input | 64 KiB |
| fractional precision | 9 digits |
| error text | 1 KiB |
| formatted output | 64 KiB |
| input periods | 100,000 |
| output periods | 100,000 |
| split/iteration steps | 1,000,000 |
| parser nesting | 8 |

Zero limits resolve to these finite defaults; callers may only lower them.
Splits and steps reject zero or negative progress, validate count before
emission, and return no partial result. Fixed `time.Duration` arithmetic uses
checked nanoseconds. Civil movement is delegated to `calendar` with a
reference date and explicit DST resolution. Constructors strip monotonic clock
metadata while preserving the represented instant and location.

Fuzz smoke covers instant/date/daily notation, fixed durations, local times,
instant splitting, daily normalization, scalar and collection JSON, and SQL
range scanning. Seeds include invalid UTF-8, trailing data, precision overflow,
oversized input, arithmetic extremes, zero/negative steps, and malformed bound
syntax.

## Persistence and compatibility evidence

PostgreSQL 18.3 integration passed for `tstzrange`, `daterange`, and their
multiranges. The adapter rejects unbounded and empty core mappings, preserves
all four instant bounds, classifies SQL `NULL` separately, rejects
sub-microsecond writes, and documents discrete-range canonicalization.

The PHP differential fixture passed against
`faustbrian/temporal@469603239dbe700739c29b4c532a90382b6cbedf`. The complete
public non-chart inventory contains 412 sorted symbols and is regenerated from
the pinned PHP source. All 412 have an exact generated behavior-coverage entry:
285 are supported and 127 deliberately diverge. Each entry names its contract,
Go test or divergence evidence, and migration guidance; unknown PHP types fail
generation rather than receiving a default classification. Behavior groups are
explained in [compatibility.md](compatibility.md), with migration guidance in
[migration.md](migration.md).

`Period\Chart` remains deferred. Its types, configuration, renderers, labels,
outputs, errors, and byte-exact fixture requirements are inventoried in the
compatibility matrix. Neither the README nor release documentation claims full
PHP-package compatibility while that gap remains. The roadmap preserves an
optional `temporalchart` extension seam over immutable public values.

## Gate evidence

The following current-tree commands passed:

- `make check`: format, vet, lint, static analysis, tests, exact 100.0%
  statement coverage, race, fuzz smoke, workflow lint, docs, and API diff;
- `make nilaway`: advisory and clean;
- `make vuln`: no known vulnerabilities;
- `make bench`: relation and notation hot paths reported zero allocations;
- `make php-compat PHP_TEMPORAL_SOURCE=<pinned checkout>`: exact fixture match;
- `go test -tags=integration ./postgres`: PostgreSQL 18.3 integration;
- `make mutation`: no surviving mutant in any configured scope.

Mutation detail:

| Scope | Killed | Lived | Not covered | Timed out | Mutant coverage |
|---|---:|---:|---:|---:|---:|
| all production packages | 1,068 | 0 | 100 | 0 | 91.44% |
| instant | 138 | 0 | 44 | 0 | 75.82% |
| dateperiod | 159 | 0 | 28 | 0 | 85.03% |
| timeofday | 425 | 0 | 21 | 0 | 95.29% |
| notation | 173 | 0 | 0 | 0 | 100.00% |
| postgres | 78 | 0 | 4 | 0 | 95.12% |

All covered mutants were killed. Uncovered entries are Gremlins instrumentation
limits for constants, switch conditions, and defensive impossible-state
branches already reached by the 100% statement suite. The bounded iteration
fuzz/property checks also kill every mutation of daily loop progress and
termination; no mutant timed out.
