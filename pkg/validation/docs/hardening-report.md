# Hardening report

Date: 2026-07-17. Environment: Go 1.26.5, Darwin arm64.

## Evidence matrix

| Claim | Local evidence |
| --- | --- |
| All production statements covered | `make coverage`: 100.0% every package and total |
| Concurrent plans and async execution are race-free | `go test -race ./...` |
| Standard-rule, composition, resource, reflection, and projection defects are detected | `make mutation`: 90/90 killed |
| Hostile paths, Unicode, compiled shapes, tags, primitives, projections do not panic | `make fuzz` six fuzz targets |
| Static defects | `go vet ./...`, golangci-lint with Staticcheck and strict additions |
| Known Go vulnerabilities | govulncheck v1.6.0 |
| Blocking hosted CI | quality, lint, Staticcheck, and vulnerability jobs green |
| API drift | generated `api/baseline.txt` diff |
| Docs/examples | required-file, relative-link, and executable-example checks |
| Resource and allocation bounds | hostile pre-traversal and scalar allocation tests |

The versioned [fuzz corpus](fuzz-corpus.md) inventories all six targets, their
seed classes, input caps, and retained regression inputs. Seeds include valid
Unicode/URL/email, invalid UTF-8 and malformed primitives, strict/malformed
tags, escaped and structurally colliding paths, and minimized formatting
oracle cases under `testdata/fuzz`. Compiled-plan fuzzing exercises anonymous
embedded fields, pointers, interfaces, aliases, arrays, maps, slices,
instantiated generic structs, and malformed typed plans.
Cycle, depth, nil-pointer, inaccessible-field, cache-race, and path-budget
behavior has deterministic evidence.

## Mutation report

The dependency-free runner copies the tree to a temporary directory for each
mutant. It changes every standard-rule family: presence states and codes;
numeric, string, collection, cross-field, primitive, and temporal predicates
and boundaries. It also changes all/any/not/when/dependent composition;
report blocking state and identity; reflective required, numeric, and
collection logic; collection traversal; panic containment; diagnostic bounds,
severity, UTF-8, and control safety; typed-plan construction and paths; cache
and reflective field and path bounds; async containment; translation
containment, bounds, and escaping; and JSON-RPC, JSON:API, and HTTP paths,
severity, truncation, and blocking state. The relevant package suite must fail
for every mutant. Current score: 90/90 killed, 100.0%.

## Transport conformance

| Property | JSON-RPC | JSON:API | HTTP |
| --- | --- | --- | --- |
| Stable code | yes | yes | yes |
| Exact location | human path | RFC 6901 pointer | human path |
| Ordered findings | yes | yes | yes |
| Safe parameters | copied | copied | copied |
| Severity | yes | yes | yes |
| Truncation | yes | yes | yes |
| Blocking state | yes | yes | yes |
| Cause/value omitted | yes | yes | yes |

One cross-transport conformance test projects the same mixed-severity,
truncated report through all three packages and compares order, code,
parameters, severity, human paths, JSON pointers, blocking state, truncation,
and hostile-location JSON escaping.

## Findings

Hardening found and fixed singular report grammar, generic map-key path loss,
fail-open truncation, nil-interface panic, unsafe zero contexts, rendered-path
collisions, reflective parity/bounds, projection identity loss, unsafe
observation labels, an over-broad fuzz leak oracle, central path-budget
enforcement, ambiguous parameter identity, reflective numeric precision and
NaN acceptance, missing projection blocking state, missing bounded async
orchestration, and secret-bearing panics escaping function adapters.
String-facing typed and reflective rules now reject oversized values before
parsing, comparison, sorting, or hashing.
Presence tables cover missing, null, empty, zero, and nonzero strings,
integers, booleans, slices, maps, pointers, arrays, and structs. Composition
retains successful and prerequisite warnings without changing blocking truth.
Custom diagnostics with invalid severities, unsafe codes, excessive parameter
metadata, invalid UTF-8, or control characters now become one blocking,
parameter-free `invalid_violation`; formatted paths cannot inject line breaks.
Arbitrary async implementations are panic-contained inside `AsyncAll`, and
deadline-aware validators have termination evidence. Hostile translation
catalogs cannot panic, grow output past the string budget, emit controls or
invalid UTF-8, inject HTML markup, or change machine semantics.
Concurrent cache compilation, hits, length reads, clearing, retained-plan use,
and async execution are race-tested. Typed, reflective, presence, collection,
map-key, map-value, slice, pointer, and custom-value checks preserve caller
data. Numeric evidence includes integer extrema, negative zero, NaN,
infinities, invalid precision, and invalid divisors. Oversized path rejection
now scans length without allocating the hostile rendering.
The requirement-by-requirement audit, local release-equivalent gate, and
blocking hosted CI are green. No release blocker remains in the audited scope.
