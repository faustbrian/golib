# Hardening Goal: Prove knapsack Correct, Bounded, and Competitive

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Audit and harden the completed `knapsack` implementation until every
advertised packing capability is independently proven correct, deterministic,
resource-bounded, cancellation-safe, interoperable, documented, and supported
by meaningful complete test evidence.

This goal MUST be executed after `GOAL.md`. It MUST assess the implementation
that exists rather than rewriting the goal around easier behavior. Missing goal
requirements are implementation defects, not optional hardening ideas.

Go 1.26 MUST remain the minimum supported language and toolchain version unless
the repository-wide version policy has moved forward.

## Required Evidence Artifacts

Create or update machine-readable evidence for:

- public API and capability inventory;
- every feasibility invariant and the tests proving it;
- solver/strategy/objective matrix and proof semantics;
- BoxPacker feature comparison;
- reference implementation and benchmark-corpus provenance;
- fuzz targets, seed corpus, and discovered regression cases;
- mutation operators, executed mutants, survivors, and classifications;
- benchmark environment, semantic normalization, raw output, and thresholds;
- resource limits and adversarial-case outcomes;
- dependency, license, vulnerability, and supply-chain review; and
- every intentional limitation or compatibility divergence.

Evidence MUST identify the package commit, Go version, dependency versions,
fixture hashes, commands, environment, and date. Generated summaries MUST fail
CI when stale.

## API And Contract Audit

- Inventory every exported type, function, method, option, interface, constant,
  sentinel, and error.
- Verify zero values are either safe and useful or rejected consistently.
- Prove constructors validate dimensions, weights, IDs, quantities, stock,
  orientations, lattice resolution, limits, and objective configuration.
- Ensure slices, maps, big-number values, attributes, placements, and diagnostics
  are defensively copied or immutable by contract.
- Verify all errors support stable `errors.Is`/`errors.As` behavior.
- Reject typed-nil interfaces, duplicate IDs, ambiguous quantities, and
  conflicting options.
- Confirm custom extension interfaces are narrow, necessary, and unable to
  mutate internal state.
- Run API compatibility checks and document all intentional breaking changes
  before v1.

## Independent Verifier Audit

The verifier is the core trust boundary and MUST be audited separately from all
solvers.

- Ensure it does not call solver placement predicates as its only proof.
- Recompute all oriented dimensions, coordinates, intersections, support,
  loads, stock, totals, and objective values from immutable inputs.
- Reject unknown containers/items, duplicate placements, omitted required
  items, extraneous items, invalid orientation, and altered dimensions/weight.
- Reject positive-volume overlap while accepting exactly touching faces/edges.
- Reject boundary escape, reserved-region overlap, unsupported placement,
  overload, fragile-top load, exceeded stack count, grouping violation,
  incompatibility, and finite-stock excess.
- Detect integer overflow before every geometric and aggregate calculation.
- Verify a serialized plan after a fresh decode, not only an in-memory result.
- Fuzz supplied plans independently from request solving.
- Treat verifier disagreement as an internal invariant failure and release
  blocker, never as a warning.

## Geometry Exhaustion

Exhaustively test:

- all six orientations and deduplication for cubes and repeated axes;
- half-open interval semantics at zero, exact-touch, one-unit overlap, and
  maximum coordinate boundaries;
- containment at every face, edge, and corner;
- intersection symmetry and transitivity assumptions that algorithms rely on;
- support-area union without double-counting overlapping supporters;
- center-of-gravity and support thresholds at exact boundaries;
- reserved cuboids and fragmented free spaces;
- volume and area products near numeric limits;
- scaling between equivalent lattices; and
- translation and axis-permutation metamorphic properties.

Geometry tests MUST use independent simple or brute-force oracles for bounded
integer grids. Self-round-trip tests are insufficient.

## Solver Correctness

For every heuristic and exact solver:

- verify every returned candidate with the independent verifier;
- prove item/container ordering and tie-break determinism;
- test all orientations, candidate placements, container choices, and objective
  comparison branches;
- compare tiny instances exhaustively with brute-force enumeration;
- compare exact-solver results with independent optimal values;
- verify branch-and-bound lower bounds never prune an improving feasible plan;
- prove symmetry reductions preserve at least one representative optimum;
- distinguish proven infeasibility from no result found within strategy limits;
- return the best verified result on cancellation/budget exhaustion only when
  contractually safe;
- ensure cross-bin improvement never loses, duplicates, or invalidates an item;
  and
- prove parallel candidate reduction returns the same canonical winner as
  sequential execution.

Add regression tests for every counterexample found by fuzzing, differential
testing, mutation, benchmark validation, or user reports.

## Objective And Optimization Audit

- Verify lexicographic precedence rather than accidental weighted-score tradeoff.
- Test box-count, monetary cost, unused volume, unused weight, balance, height,
  and subset value independently and in documented combinations.
- Use exact `money` and `math` values without float conversion.
- Test equal scores, zero costs, huge costs, duplicate box types, finite stock,
  and deterministic final tie-breaks.
- Prove a lower-priority criterion can never override a better primary score.
- Report quality/optimality gap only against a valid known lower bound or optimum.
- Verify custom objectives cannot mutate state, return invalid values, overflow,
  panic inconsistently, or create nondeterminism without explicit policy.

## Physical Constraint Audit

Build targeted fixtures for:

- upright and keep-flat orientation;
- fragile/non-stackable surfaces;
- maximum supported weight through multi-level support graphs;
- partial support and exact minimum support-ratio boundaries;
- loads shared across multiple supporting items;
- maximum stack count/height;
- center-of-gravity bounds;
- linked/grouped items with feasible and impossible combinations;
- incompatible item classes and separation rules;
- container-specific eligibility and reserved regions; and
- gross weight including tare versus content-only capacity.

Documentation MUST state that these discrete constraints do not constitute a
general physics, crush, dynamic transport, robotic loading, or regulatory
simulation.

## Numeric And Unit Safety

- Verify public length and mass boundaries remain compatible with
  `measurement` and exact arithmetic remains compatible with `math`;
  duplicate local quantity or decimal types are release blockers.
- Property-test exact conversion across every accepted length/mass unit and
  lattice resolution.
- Reject inexact conversion unless an explicit rounding mode is selected.
- Test each rounding mode at positive and negative half boundaries where
  negative input is rejected before packing but conversion code remains shared.
- Exhaust zero, negative, minimum, maximum, huge-scale, mixed-unit, and
  overflow-inducing values.
- Test coordinate, area, volume, total weight, cost, score, quantity expansion,
  index, and allocation-size overflow independently.
- Ensure no authoritative feasibility or objective path uses binary floating
  point.
- Verify canonical encoding preserves exact quantities and cannot silently
  change lattice resolution.

## Determinism And Concurrency

- Permute item and container input orders and require canonical identical output
  where semantic identity is unchanged.
- Repeat each deterministic corpus across CPU counts and scheduler pressure.
- Run parallel and sequential solvers with identical seeds and compare results.
- Race shared immutable requests, options, constraints, objectives, verifiers,
  and solvers.
- Detect mutation or aliasing of caller-provided slices, maps, values, and
  callbacks.
- Test cancellation during generation, verification, exact search,
  cross-container improvement, and parallel reduction.
- Prove all goroutines stop and all memory becomes collectible after return.
- Run sustained loops under the race detector and goroutine/leak checks.

## Resource And Denial-of-Service Hardening

Create adversarial cases for:

- enormous quantity expansion;
- many identical items/orientations causing permutation explosion;
- dimensions that create maximal candidate points/free spaces;
- finite-stock combinations with no solution;
- incompatible constraints discovered only late in search;
- exact-solver branch explosions;
- custom callbacks that are slow, panic, allocate, or ignore context;
- huge IDs/metadata/diagnostic output;
- malformed, deeply nested, duplicate-key, or oversized JSON;
- repeated cancellation and deadline races; and
- memory pressure during parallel solving.

Every public entry point MUST enforce documented item, type, quantity,
orientation, candidate, node, iteration, memory, parallelism, metadata, input,
and output limits before uncontrolled work. Default limits MUST be safe for a
server accepting untrusted requests.

## Fuzzing

Maintain native Go fuzz targets for:

- strict request and plan decoding;
- unit/lattice normalization;
- orientation enumeration;
- cuboid containment/intersection/support;
- item/container validation;
- fixed-container packing;
- variable-container selection;
- exact solver on tiny bounded inputs;
- independent plan verification;
- each built-in physical constraint;
- objective comparison and tie-breaking; and
- cancellation/budget option combinations.

Fuzz properties MUST include no panic/hang/leak, bounded rejection, deterministic
replay, verifier acceptance for every returned plan, exact agreement on tiny
cases, and stable canonical encoding. Minimized failures MUST become permanent
named regression fixtures.

## Mutation Testing

Mutation testing MUST exercise and kill meaningful changes to:

- `<`, `<=`, equality, and interval endpoints;
- axis/orientation selection;
- containment and overlap predicates;
- support and load-bearing arithmetic;
- container weight/stock limits;
- item conservation and duplicate detection;
- objective precedence and tie-break direction;
- branch-and-bound pruning and lower bounds;
- cancellation and budget checks;
- status selection (`optimal`, `infeasible`, `budget_exhausted`);
- overflow and unit-validation guards;
- defensive copy and canonical ordering; and
- strict-decoder and resource-limit checks.

All eligible production statements MUST receive mutation coverage. Every
survivor MUST be killed or classified with concrete proof that it is equivalent,
unreachable by contract, or unsupported by the mutation tool. A generic “not
useful” waiver is forbidden.

## Differential And Corpus Verification

Pin exact reference revisions and licenses. Maintain semantic adapters for:

- `dvdoug/BoxPacker`;
- `jcoruiz/gopackx`;
- `gedex/bp3d`; and
- selected independently published benchmark datasets/exact results.

For each comparison, publish the common supported semantics. Dimensions,
weights, rotations, stock, constraints, objectives, and acceptance rules MUST be
identical. Independently verify every competitor plan before comparing quality
or runtime. If a competitor cannot expose placements or an equivalent result,
report that limitation rather than treating its output as valid automatically.

Compare at least:

- feasibility success rate;
- invalid-result rate;
- box count and total cost;
- volume and weight utilization;
- optimality gap where independently known;
- runtime distribution, allocations, and peak memory; and
- timeout/cancellation behavior.

Cross-language BoxPacker comparisons MUST disclose PHP and Go versions, process
startup treatment, warm-up, serialization, and whether wall time includes input
and verification. Do not publish misleading “times faster” summaries based on
different objectives or weaker constraints.

## Performance Regression Discipline

Maintain reproducible benchmarks for tiny exact, ordinary e-commerce,
orientation-heavy, weight-limited, stability-heavy, finite-stock, impossible,
adversarial, and large bounded requests.

Each benchmark MUST validate the returned plan and record solution quality.
Set reviewed budgets for:

- p50/p95 runtime per corpus class;
- allocations and bytes per item/candidate;
- peak RSS and goroutine count;
- cancellation latency;
- verifier overhead;
- exact-solver nodes per second; and
- objective quality or maximum accepted regression.

A faster invalid or materially worse plan is a regression. A denser plan that
violates the latency/memory budget is also a regression unless explicitly
approved and selected through a different solver profile.

## Serialization And Compatibility

- Version canonical request and plan schemas.
- Reject unknown critical versions, duplicate keys, trailing garbage, oversized
  values, invalid UTF-8, numeric precision loss, and non-canonical exact values.
- Golden-test stable encoding and backward decoding for every supported version.
- Ensure N and N-1 library versions can verify persisted plans during the
  documented compatibility window.
- Treat removal or semantic reinterpretation of an orientation, status,
  constraint, objective, or default as a breaking change.
- Test migration tools or adapters against pinned BoxPacker/common-Go examples.

## Static, Security, And Supply-Chain Gates

Run and enforce:

- formatting and `go vet`;
- strict non-contradictory `golangci-lint` and staticcheck configuration;
- NilAway as a warning-only annotated job until its signal is proven;
- `govulncheck`, dependency review, license review, and secret scanning;
- architecture/dependency-direction tests;
- forbidden float/ambient-randomness/ambient-clock checks in authoritative core
  paths;
- generated-artifact and module tidiness checks;
- reproducible build, SBOM, provenance, and release artifact verification; and
- the latest approved Go version and dependency versions.

The root module SHOULD depend only on the standard library and approved sibling
numeric/measurement packages. Optional visualization or adapters MUST not pull
heavy dependencies into core consumers.

## Documentation Audit

Verify that documentation lets a new consumer correctly choose and use:

- pack-all versus fixed-bin versus subset selection;
- units and lattice resolution;
- item and container models;
- rotations and orientation restrictions;
- objectives and deterministic tie-breaks;
- solver profile and budgets;
- weight, fragility, support, and load-bearing constraints;
- finite stock, linked items, custom constraints, and impossible items;
- result statuses, independent verification, and diagnostics;
- JSON persistence and compatibility;
- BoxPacker/common-Go migration;
- performance/quality tradeoffs and fair benchmarks;
- security limits and untrusted-input deployment; and
- troubleshooting, FAQ, architecture, contribution, release, and support.

Every exported symbol MUST have useful Go documentation explaining semantics,
units, ownership, errors, determinism, and limits where applicable. Comments
MUST explain non-obvious invariants and pruning decisions rather than narrating
syntax.

## Required Final Commands

From a clean checkout, run and record at least:

```text
make format
make lint
make test
make coverage
make race
make fuzz
make mutation
make benchmark
make benchmark-compare
make verify-corpus
make docs
make vulnerability
make check
make release-check
```

No target may silently skip a missing tool, corpus, service, generated artifact,
or unavailable comparison. If an expensive or external certification cannot run
on every pull request, it MUST run in a required protected release workflow with
fresh evidence.

## Release Blockers

- Any independently invalid placement or incorrect verifier acceptance.
- Lost, duplicated, altered, or ambiguously identified item.
- Overlap, boundary escape, forbidden orientation, overweight container,
  stock excess, unsupported stack, load-bearing violation, or accounting drift.
- Heuristic failure reported as infeasible or unproven result reported optimal.
- Exact solver disagreement with brute force or an independent known optimum.
- Incorrect pruning, objective precedence, deterministic tie-break, or status.
- Float-dependent feasibility, silent unit/rounding behavior, or overflow.
- Panic, race, deadlock, livelock, goroutine leak, ignored cancellation, or
  work continuing after return.
- Unbounded input, quantity, candidates, search, diagnostics, memory,
  parallelism, callback, serialization, or visualization.
- Mutation survivor without accepted concrete classification.
- Benchmark claim with mismatched semantics, unverified output, hidden startup,
  omitted quality, or irreproducible evidence.
- Unlicensed/unpinned corpus or copied implementation material.
- Missing meaningful 100% coverage, stale generated evidence, undocumented API,
  or failing required gate.
- Any advertised capability without complete correctness, limits,
  interoperability evidence where applicable, and documentation.

## Completion Criteria

Hardening is complete only when:

- every public API and advertised capability is inventoried and proven;
- every solver result is independently verified;
- exact mode agrees with brute-force and independent small-instance oracles;
- deterministic behavior survives permutation, parallelism, and repeated runs;
- all resource/cancellation limits withstand adversarial tests;
- meaningful 100% production coverage and complete mutation execution pass;
- fuzz, race, leak, corpus, differential, benchmark, static, vulnerability,
  supply-chain, documentation, and release gates pass from a clean checkout;
- BoxPacker and Go comparisons are semantically fair and reproducible;
- no unresolved high- or critical-severity correctness/security finding remains;
  and
- `CHANGELOG.md`, capability matrix, provenance manifests, benchmark evidence,
  API docs, and limitation records are current.
