# Hardening evidence

| Requirement | Authoritative local evidence |
| --- | --- |
| Gregorian and ISO correctness | Exhaustive years 1–9999 differential test |
| Clamp/reject/overflow and negatives | Arithmetic matrix plus 19 killed mutations |
| Gap/fold and timezone drift | Transition corpus and `make timezone` |
| Immutable bounded business rules | Property/hostile-input tests and race suite |
| Canonical hostile parsing | Fuzz targets and exact coverage |
| PostgreSQL finite/infinity round trip | Native pgx codec and live integration tests |
| Concurrency | Shared calendar, location, codec, and corpus tests under race |
| Production statement coverage | `make coverage` reports exactly 100.0% |
| Performance and allocations | `make benchmark` plus allocation-budget tests |
| Vulnerabilities and analyzers | vet, Staticcheck, golangci-lint, govulncheck |

## Local release command matrix

| Command | Scope | Latest local result |
| --- | --- | --- |
| `make tidy-check format-check vet staticcheck lint` | module and static quality | passed, zero findings |
| `make test` | unit, properties, fixtures, examples | passed |
| `make race` | all packages and immutable concurrent use | passed |
| `make coverage` | all runtime packages | exactly 100.0% |
| `make fuzz FUZZ_TIME=2s` | nine hostile-input targets | passed |
| `make mutation` | 19 high-risk semantic mutants | 19/19 killed |
| `make timezone` | DST, aliases, unusual offsets, ranges | passed |
| `make integration` | PostgreSQL date/infinity under race | passed on pinned PostgreSQL 18 |
| `make benchmark` | parse, arithmetic, ISO, business, pgx, timezone | passed |
| `make provenance docs api-compat` | governance and publication artifacts | passed |
| `make vuln workflows` | vulnerability and workflow validation | passed |
| `make nilaway` | advisory nil analysis | passed with zero findings |

See the [Gregorian/ISO vectors](gregorian-iso-vectors.md) and
[timezone corpus](timezone-corpus.md) for the human-reviewable truth tables.

## Requirement audit

### Gregorian and arithmetic

| Requirement | Executable evidence |
| --- | --- |
| Years 1-9999, leap rules, month lengths, ordinals, weekdays, and ISO weeks | `TestExhaustiveSupportedGregorianCalendar` constructs and differentially checks every supported date |
| Quarter and semester membership and boundaries | The same exhaustive test checks every supported year and month |
| Clamp, reject, overflow, and signed movement | `TestArithmeticPolicyMatrix`, `TestDateArithmeticPolicies`, and `TestNamedSubtractionAndComponentDifference` |
| Policy-permitted add/subtract inverses | `TestMonthArithmeticInverseWhenDayIsPreserved` plus `FuzzDateArithmeticNeverPanics` |
| Minimum, maximum, zero, native integer overflow, and unsupported policy states | `TestDateDayArithmeticRejectsSupportedRangeOverflow`, `TestDateAndPeriodEdgeContracts`, and `TestTypedPeriodEdgeContracts` |
| Mutation sensitivity | `make mutation` changes leap, parser, clamp, reject, overflow, multiplication, negative movement, both date/month range edges, business admission/counting/search, and timezone gap/fold logic |

### Timezone and DST

| Requirement | Executable or reviewable evidence |
| --- | --- |
| Gaps, folds, aliases, unusual and second-level offsets, historical transitions, and date-line changes | `calendartest.TransitionVectors`, `TestFixturesAndTransitionCorpus`, and the [timezone corpus](timezone-corpus.md) |
| Explicit reject/earlier/later/offset ambiguity policies | `TestResolveRejectsGapAndSelectsFoldOccurrence` and `TestResolutionFailureContracts` |
| Missing, malformed, oversized, and hostile zone identifiers | `TestLoadLocationBoundsIANAIdentifiers` and `FuzzLoadLocation` |
| Standard-library differential behavior | `TestTimezoneConversionsDifferentialAgainstStandardLibrary` checks every month from 1900-2030 across nine zone identities |
| Next-day exclusive day boundaries | `TestInstantRoundTripAndExclusiveDayRange`, `TestInclusiveDatesBecomeExclusiveInstantPeriod`, and [exclusive ranges](exclusive-ranges.md) |
| Persisted local values under tzdata change | [Timezone guidance](timezone.md), [versioning](versioning.md), and [operations](operations.md) require zone, policy, local value, and tzdata/application version |

### Business calendars

| Requirement | Executable or reviewable evidence |
| --- | --- |
| Arbitrary or full weekends, empty calendars, closures, overlaps, observance, and revisions | `TestCalendarBusinessDayCalculations`, `TestCalendarIsImmutableAndPreservesOverlappingHolidays`, and `TestEveryObservanceAndLimitBranch` |
| Bounded forward, backward, add, and count searches | `TestBusinessSearchAndResourceBounds`, `TestCalendarEveryCalculationBranch`, and the caller-supplied search limits |
| Caller inputs and returned values remain immutable | `TestCalendarIsImmutableAndPreservesOverlappingHolidays`, defensive metadata copies, and `TestCalendarConcurrentReads` |
| Hostile holiday names, metadata, provenance, and configuration | `TestHolidayAndCalendarRejectHostileData`, `FuzzHolidayData`, and `FuzzCalendarConfiguration` |
| Dataset provenance, checksums, deterministic generation, and compatibility classification | No dataset ships; [holiday datasets](holiday-datasets.md) and the [business report](business.md#provenance-revision-and-compatibility-report) define the blocking contract for any future dataset |

### Parsing, persistence, and resources

| Requirement | Executable evidence |
| --- | --- |
| ISO/custom parsers, invalid UTF-8, impossible values, trailing data, and huge years | `FuzzParseDate`, `FuzzTypedCalendarParsers`, strict parser tests, and the ten-byte parser limit |
| Timezone names and holiday metadata | Timezone and business fuzz targets plus named byte/count limits |
| JSON, text, config, validation, and canonical wire forms | Date encoding tests and the calendarconfig, calendarvalidation, and calendarwire package suites |
| SQL, pgx, finite date, NULL, and explicit infinity policy | PostgreSQL unit/fuzz tests and `TestPostgreSQLDateRoundTrip` against a live server |
| Parser, year, holiday, search, output, and allocation budgets | Constants and caller limits plus all package-local `Test*AllocationBudget*` tests |
| Shared calendars, locations, codecs, and generated corpus metadata | Concurrent tests in business, timezone, calendarwire, postgres, and calendartest under `go test -race ./...` |
| Performance evidence | Parse, arithmetic, ISO, business, timezone, and pgx benchmarks documented in [performance](performance.md) |

## Deliverable index

| Deliverable | Artifact |
| --- | --- |
| Gregorian/ISO truth tables and arithmetic policy matrix | [Gregorian and ISO vectors](gregorian-iso-vectors.md) and [arithmetic](arithmetic.md) |
| Timezone transitions, ambiguity, and tzdata compatibility corpus | [Timezone corpus](timezone-corpus.md) and [timezone guidance](timezone.md) |
| Business provenance, revision, and resource report | [Business calendars](business.md) and [holiday datasets](holiday-datasets.md) |
| Mutation, fuzz, race, PostgreSQL, and benchmark evidence | This document, repository scripts, and [performance](performance.md) |
| API, Carbon migration, timezone, business, security, and FAQ documentation | [API](api.md), [Carbon migration](carbon-migration.md), [timezone](timezone.md), [business](business.md), [security](security.md), and [FAQ](faq.md) |
| Current release notes | [Changelog](../CHANGELOG.md) |

## Release-blocker disposition

The local blocking matrix rejects invalid dates, wrong ISO results, arithmetic
policy drift, DST misresolution, unbounded business searches, caller-data
mutation, persistence corruption, races, panics, and resource-limit bypasses.
No holiday dataset is bundled, day ranges use next-day exclusive instants, and
coverage and curated mutation gates are exact blockers. Hosted CI for the exact
release revision remains a separate mandatory gate; local success does not
substitute for a green hosted run.

`calendartest` contains failure-reporting helpers whose failing branches cannot
be executed without intentionally failing a test. It is excluded from the
production denominator; all runtime packages remain included.
