# Changelog

All notable repository-level changes are documented here. Module behavior is
documented in each module's changelog.

## Unreleased

### Fixed

- Generate standalone module checksums in dependency order so local `v1.0.0`
  proxy verification cannot retain hashes from unfinished dependency trees.
- Route standalone Makefile targets through the installed `.golib` tooling so
  local repository checks execute the same contract as CI.
- Separate release-rehearsal artifacts from strict verification evidence and
  restore the newest exact content-matching mutation checkpoint across prior
  trusted runs, so release artifacts and stale partial runs cannot trigger
  redundant mutation campaigns or shadow reusable proof.
- Replace the stale 138-module and 107-release-module readiness baseline with
  exact current 142-module CI and 111-module `v1.0.0` rehearsal evidence while
  preserving the separate operational-assurance and public-release boundary.
- Preserve valid Swagger 2.0 output when OpenAPI request bodies use empty,
  malformed, aliased, or unavailable references, request or response content
  uses an incompatible media-type key, a request-body schema is omitted, or a
  response requires normalization.
- Close queue control-plane fleet boundary gaps around exact capacities,
  cumulative runtime, rejection totals, ordering, and metric saturation.
- Make OpenSearch bounded-load readiness verification tolerate only a bounded
  transient shard-initialization period after concurrent writes.
- Retry checksum-pinned CI tool downloads after transient transport failures
  instead of failing unrelated module jobs before verification starts.
- Stabilize Kafka race verification around franz-go's valid empty-transaction
  abort outcomes while still requiring that an abort never reports a commit.
- Strengthen the RabbitMQ Streams stored-offset contract at the exact zero
  boundary so mutation evidence rejects treating the initial offset as invalid.
- Add an attributable manual release-rehearsal mode to the single CI workflow
  so every releasable module can execute its service-backed `v1.0.0` dry-run
  without duplicating the strict source-verification matrix.
- Bind operational-assurance evidence only to the modules that can affect the
  observed scenario, excluding unrelated reverse dependants.
- Stabilize Kafka log-recovery evidence at a deterministic valid 1 MiB segment
  boundary, remove redundant queue control-plane outcome predicates, and
  retain the RabbitMQ Streams adapter's explicit nil-context boundary under
  strict lint.
- Strengthen queue control-plane boundary tests for desired-state targets,
  command metadata defaults, and nonterminal dispatch outcomes, with an exact
  operational-assurance input-digest migration for the test-only transition.
- Remove scheduler-order assumptions from concurrent RabbitMQ Streams consumer
  reconnect verification while retaining causal lifecycle assertions.
- Make the CloudEvents RabbitMQ Streams decoder source stable across Go 1.26.6
  and Go 1.27 formatting while preserving its decoded output.
- Preserve retained operational-assurance evidence across additive RabbitMQ
  Streams fixture orchestration with reviewed, exact input-digest migrations,
  while keeping the four new modules outside prior matrix and consumer claims.
- Replace stale normalization-readiness claims with exact package-attributable
  main and full-event CI proof while preserving separate release blockers.
- Stage only durable attributable CI evidence so cancellation cannot upload
  disposable mutation workspaces, Go build caches, locks, or partial files.
- Replace analyzer-opaque integer narrowing and redundant assignments across
  HTTP, retry, queue, regular-expression, and sequencer boundaries.
- Scope fallback formatting to the packages owned by the selected module so
  root checks cannot override a nested module's explicit formatter policy.
- Honor explicit module `format` and `format-check` targets in the canonical
  repository runner, while retaining gofmt as the fallback for modules without
  formatter ownership.
- Restore trusted content-addressed mutation checkpoints before CI module
  verification so unchanged packages retain their exact proof while changed,
  missing, malformed, or stale evidence executes normally.
- Bind mutation checkpoints and reviewed zero-mutant packages to the complete
  behavior-affecting Gremlins identity rather than its version label, preserve
  exact executable hashes for traceability, and invalidate stale aggregate
  reports as soon as a replacement campaign starts.
- Accept ripgrep's upstream revision suffix while verifying the pinned CI
  binary, so valid checksum-pinned releases reach module verification.
- Provision checksum-pinned ripgrep for package checks that require its search
  semantics, while keeping state-machine, WSDL, and XSD benchmark verification
  portable on the standard shell toolchain.
- Reconcile completed Kafka and Verkle specialist evidence into the aggregate
  inventory, close the resolved Kafka specification-governance risk, and keep
  managed-MSK and Verkle security non-claims explicit.
- Make Kafka service overlapping-member shutdown evidence tolerate only the
  documented transient observer-reentry fence without masking other failures.
- Make HTTP admission verification distinguish immediate capacity from the
  bounded waiter path without relabelling retained operational evidence.
- Make queue-control CLI credential validation mutation-precise and remove the
  scheduler fuzz gate's undeclared ripgrep dependency.
- Remove undeclared ripgrep dependencies from schema-registry provenance,
  SBOM, and official Confluent wire-reference verification.
- Align the focused service-platform binary-size regression with the documented
  Darwin-only absolute budget while preserving portable relative enforcement.
- Make Kafka adapter documentation and specification checks portable to the
  stock CI runner, register their pinned conformance decisions, restore PTY
  tests before closing the controlling endpoint, and complete Linux-only
  Merkle interoperability dependencies.
- Provision the CLI shell-verification runtime from a checksum-pinned Ubuntu
  package without invoking the runner package manager or its shared locks.
- Run repository-contract validation independently from module selection so a
  root metadata failure cannot suppress attributable module and CodeQL results,
  while the required summary still fails closed on that validation.
- Replay each passed operational-assurance record's captured input environment
  when validating its fingerprints, so Linux CI can verify evidence produced
  on another platform without changing the evidence identity.
- Prevent parallel release snapshots from expanding dependency closures a
  second time, so each selected module runs in exactly one isolated lane.
- Normalize all owned-module requirements to the unpublished local `v0.0.0`
  source proxy, reject owned pseudo-versions in repository validation, and
  make mutation fingerprints ignore only owned version locators, preserving
  content-identical evidence without hiding source, test, fixture, tool,
  service, or external dependency changes.
- Ignore ambient `node_modules` trees when generating source-documentation
  manifests so local dependency installations cannot make fresh-clone
  validation stale.
- Propagate the task-owned isolated module file through an opt-in environment
  contract so nested Go tooling can resolve current owned modules without
  weakening unrelated child-process isolation.
- Preserve content-identical package gate evidence across the runner process-
  isolation refactor while keeping service setup and gate commands in each
  checkpoint fingerprint.

### Added

- Record the public durability composition across digest-pinned PostgreSQL 14
  through 18 with Valkey 9.1.0 and correct already-proved OpenSearch partial
  result and point-in-time failure coverage.
- Add a maintained PostgreSQL 14 through 18 reference durability matrix against
  Valkey 9.1.0 so supported database versions exercise the same public
  idempotency, outbox, queue, and transaction composition.
- Add a complete operational-assurance requirement matrix that distinguishes
  proved, partial, external, specialist-owned, and consumer-owned work while
  preserving the current `not ready` verdict and exact remaining proof.
- Record executable OpenSearch dashboard, alert, incident-drill, and runbook
  consistency evidence without treating repository artifacts as installed
  production operations.
- Record secured OpenSearch 2.19.6 and 3.8.0 TLS, least-privilege,
  tenant-isolation, credential-rotation, DNS-change, and recovery evidence
  without treating local test PKI as production identity proof.
- Record real OpenSearch 2.19.6 and 3.8.0 version-matrix, snapshot/restore,
  rebuild, reconciliation, bounded-load, mixed-version, outage-recovery, and
  rolling-upgrade evidence without expanding it into managed-service or
  production-capacity claims.
- Reuse the unchanged HTTP composition proof as bounded observability evidence
  for correlation, in-memory telemetry and audit, readiness recovery, graceful
  shutdown, and restart without implying production operations readiness.
- Reuse the unchanged HTTP composition proof as bounded security evidence for
  signed requests, capabilities, authentication, tenant isolation, validation,
  fail-closed audit delivery, and correlation without implying supply-chain or
  production privacy readiness.
- Record bounded cross-package adoption evidence for Track, Postal, and
  Location role composition, correlation, resilience, lifecycle, and generic
  bootstrap budgets without treating the fixtures as production services, and
  classify its content-addressed module fingerprints as non-secret checksums.
- Record PostgreSQL and Valkey scheduler lease-store evidence for server-time
  fencing, cancellation atomicity, ambiguous-outcome reconciliation, caller-
  owned schemas, and reconnect recovery.
- Record Redis and Valkey queue lifecycle evidence for deadline redelivery,
  dead-letter recovery, rolling worker replacement, process termination, and
  the explicit at-least-once duplicate window.
- Record focused workflow process-death, deadlock, snapshot-restore,
  replica-promotion, unknown-outcome, fencing, and dead-letter recovery
  evidence without expanding it into managed-service claims.
- Add a task-owned process-death and PostgreSQL/Valkey replacement campaign
  that proves fail-closed outages, exact replay, abandoned-task reclamation,
  and acknowledgement persistence without claiming managed failover.
- Add a constrained native-Linux service load campaign with explicit latency,
  throughput, heap, goroutine, descriptor, and error budgets while preserving
  soak and production capacity as open assurance work, and bind its proof to
  the exact non-releasable harness input.
- Add a maintained PostgreSQL and Valkey durability reference that proves
  transactional business, idempotency, outbox, relay, acknowledgement, replay,
  and unacknowledged-task recovery through public module APIs.
- Record current clean-clone release dry-runs and deterministic all-module
  clean-consumer resolution for all 107 releasable modules at exact `v1.0.0`
  without implying public publication or release readiness.
- Add a content-addressed operational-assurance register and validator that
  inventories every releasable module and required composition scenario while
  keeping the current incomplete verdict explicitly `not ready`.
- Add one root Dependabot policy for all Go modules and GitHub Actions, plus a
  dependency governance guide covering admission, update review, ownership,
  detection, and compromise response.
- Add a repository threat model, security control matrix, and explicit
  residual-risk register so package evidence, operational assurance, and
  release decisions cannot be conflated or silently waived.
- Add a generated benchmark asset catalog and repository performance guide so
  benchmark sources, harnesses, baselines, services, fairness rules, and
  regression expectations are explicit and stale inventories fail validation.
- Add a generated AST-based source documentation inventory that attributes
  missing package comments, exported API comments, malformed comments,
  generated-source gaps, and policy markers to each module.
- Add the root documentation entry point, package and protocol decision guides,
  recommended compositions, integration ownership map, status, versioning, and
  shared terminology.
- Link every non-specialist releasable module to the canonical documentation
  portal and reject future missing backlinks while explicitly leaving the
  Kafka and Verkle specialist-owned modules outside this documentation batch.
- Reject repository documentation with missing or multiple top-level headings,
  skipped heading levels, or unclosed fenced code blocks.
- Reject malformed GitHub repository source links that omit a `blob` or `tree`
  route and repair the propagated queue-control-plane catalog link.
- Reject obsolete standalone-repository URLs across package documentation,
  replace package-local workflow badges with the repository CI workflow, and
  remove queue-control-plane publishing claims unsupported by automation.
- Reject released-version changelog sections for pre-v1 modules and correct
  package status, security, roadmap, and release-candidate guidance to match
  the repository's unpublished state.
- Enforce repository documentation spelling and local/external link integrity
  with pinned, fail-closed, locally runnable CSpell and Lychee gates.
- Harden identity-platform orchestration with pinned goal semantics across
  lifecycle moves, commit/tree-bound preflight identity, durable exact worker
  assignment attestations, and lossless ordinary abandonment evidence.
- Add validated family metadata for every releasable module and split the
  generated consumer package catalog from the exhaustive engineering inventory.
- Add a canonical cohesion gate that fails on stale catalogs, incomplete family
  metadata, missing module entry points, or modules without a public package.
- Add the consumer-facing Golib design language and reviewed cohesion baseline,
  including module families, construction and lifecycle conventions, adapter
  naming findings, intentional package identifiers, and residual decisions.
- Add a repository-wide specification decision validator, canonical governance
  contract, and pull request review section so specification-backed modules
  fail visibly on missing provenance, unresolved interpretations, or stale
  executable evidence. The validator supports deterministic all-module and
  explicit module selections through the same root command convention as the
  remaining repository gates.
- Require each specification-backed module README and every existing
  conformance, compatibility, and contribution guide to link the canonical
  decision register instead of merely naming its path as code.

### Fixed

- Enforce `v1.0.0` as every unpublished module's first public release even
  when an obsolete package-local pre-v1 policy file remains present.
- Preserve reviewed OpenSearch operational evidence across the corrected
  real-cluster alias observer while retaining original observation times.
- Preserve operational evidence across reviewed, exact transitive input-digest
  migrations without rewriting observation times or accepting unlisted digest
  changes, while still failing closed on stale artifacts and behavioral inputs.
- Make release plans report operational readiness and make every future
  mutating release path fail before verification or publication when assurance
  is not ready.
- Remove inert package-local Dependabot configurations and reject future
  nested update policies so dependency automation cannot silently diverge.
- Permit cataloged non-releasable fixture modules to use an intentionally empty
  local proxy so their not-applicable verification gates can be recorded while
  unknown module selections still fail closed.
- Make evidence-gate locks atomic and recover legacy ownerless locks so an
  interrupted runner cannot stall later verification indefinitely.
- Isolate the snapshot lifecycle fixture from an inherited nested-run marker so
  the canonical root check exercises its intended top-level process boundary.
- Exclude catalog presentation metadata and root-only orchestration files from
  nested-module gate fingerprints, so unrelated repository catalog and Makefile
  changes no longer invalidate content-identical module evidence.
- Exclude owned-dependency test source from downstream gate fingerprints while
  retaining dependency production inputs, so tests that a consumer never
  compiles cannot invalidate its content-identical evidence.
- Scope `.gitleaks.toml` to secret-scan fingerprints so unrelated secret-policy
  edits do not invalidate format, test, race, coverage, mutation, or other
  content-identical gate evidence.
- Reject specification decision registers that omit their unresolved-decision
  inventory or leave it open outside a stable release-blocking decision entry.
- Require superseded specification decisions to reference a different existing
  replacement in the same register instead of accepting a dangling identifier.
- Read decision lifecycle only from its status field and reject entries with
  missing or multiple statuses instead of inferring state from unrelated prose.
- Validate local Markdown link fragments in specification decisions so renamed
  or removed target sections fail governance instead of leaving stale anchors.
- Require specification decision fields to be structurally labeled instead of
  allowing incidental prose to masquerade as missing governance metadata.
- Register Kafka's exercised broker, resource-reaper, comparison-client, MSK
  signer, and AWS SDK inputs in the generated repository catalog.
- Register Kafka's normative decision record and real-broker conformance
  matrices in repository specification governance.
- Preserve gate execution, durable logs, and attributable evidence when a
  terminal or automation client disconnects from live output, preventing a
  completed long-running gate from being misclassified as `SIGPIPE` failure.
- Validate stable specification decision numbering independently for each
  identifier series so versioned standards can preserve their published IDs.
- Catalog ECMAScript regular expressions against the exact ECMA-262 edition,
  pinned Test262 revision, Unicode data release, and JSON Schema profile.
- Catalog OpenAPI's exact 2.0 through 3.2 feature lines, adjacent JSON Schema
  dialects, generated normative matrices, published artifacts, accepted errata,
  and independent descriptions.
- Catalog OpenRPC's supported 1.3.x and 1.4.x lines, adjacent standards, pinned
  official examples, and generated conformance matrices instead of the stale
  OpenRPC 1.3-only description.
- Accept an explicit `release` field as a provenance version pin so structured
  source manifests do not need a redundant `version` alias.
- Skip Docker discovery for service-free modules and bound server-version
  discovery elsewhere, so an unrelated unresponsive daemon cannot stall every
  verification lane.
- Accept catalog-declared not-applicable gate evidence only when its bound log
  records the catalog-policy decision, so scoped aggregate checks can complete
  without treating a skipped mandatory gate as a pass.
- Accept well-formed advisory NilAway evidence during goal audits while
  continuing to reject advisory outcomes for every mandatory gate.
- Scope gate fingerprints to the tool versions and service images that can
  affect each selected gate, preventing unrelated version-pin changes from
  invalidating otherwise identical verification and mutation evidence.
- Resolve changed-line mutation selection relative to nested module roots and
  preserve every non-contiguous added line in a diff hunk, preventing valid
  mutants from being silently skipped in focused verification without
  invalidating unaffected full-package mutation evidence.
- Resolve mutation fingerprints in a module-scoped dependency graph so an
  unrelated workspace module with an unpublished dependency cannot block a
  selected module's mutation gate.
- Exclude GolangCI-Lint runner-concurrency plumbing from gate input identity so
  successful analysis evidence survives operational parallelism fixes.
- Allow isolated GolangCI-Lint runners to execute concurrently so parallel
  module verification cannot fail on the linter's process-wide lock.
- Exclude agent-only policy documentation from executable gate fingerprints so
  policy wording cannot invalidate otherwise identical verification evidence.
- Reuse mutation checkpoints after history rewrites through explicit
  old-to-current package input mappings instead of repository-wide worktree
  identity, so unrelated concurrent changes cannot restart proven campaigns.
- Bound Docker service cleanup so an unresponsive removal cannot stall a
  verification lane indefinitely, while continuing cleanup for later services.
- Reject reduced, implicit, or runtime-overridable package mutation thresholds
  and route package entry points through the canonical exact-100 runner.
- Terminate and await complete verification-lane process trees before deleting
  their snapshots, preserving in-flight evidence during cancellation.
- Ignore deleted tracked paths when fingerprinting the live working tree so a
  clean verification snapshot of the same files can reuse valid gate evidence.
- Reuse ECMA regexp's external Test262 coverage baseline without launching the
  corpus runner for every mutant, preventing concurrent Node and bridge trees
  from multiplying campaign memory.
- Bound Apache Kafka fixture cleanup and give Go test, race, coverage, and the
  complete broker-backed interoperability suite explicit execution deadlines.
- Ignore unrelated workspace membership changes when validating isolated
  module gates while retaining them for workspace-backed benchmarks.
- Resolve explicit module selections from the registered catalog so scoped
  gates are not blocked by unrelated incomplete module work.
- Track shared runners and gate-specific helpers independently so changes to
  mutation-only tooling cannot invalidate unrelated test, race, coverage, or
  fuzz evidence.
- Scope verification-gate fingerprints to the selected module and its owned
  dependencies so unrelated manifest registrations cannot invalidate
  long-running package evidence.
- Restrict package mutation fingerprints to the target package's compiled
  tests and dependency closure so unrelated reverse dependants cannot trigger
  redundant mutation campaigns they do not observe.
- Remove complete mutation scratch runs, including isolated Go caches and
  historical input copies, after every exit path, and safely recover abandoned
  owned runs without disturbing concurrent active runs.
- Pass Gremlins' integration-test CPU setting as distinct command arguments so
  mutation workers execute every target package instead of silently testing
  only the module root.
- Bound each mutant test run to ten times its measured baseline so
  non-terminating mutants are killed without occupying a worker for hours.
- Prove an unmutated unit baseline, then run untagged package tests before
  tagged whole-module integration tests for each mutant so baseline failures
  cannot create false kills and unit-killed mutations do not repeatedly
  provision expensive broker fixtures.
- Include the patched shared-coverage implementation in mutation input
  fingerprints so tooling changes cannot reuse stale exact-mutation evidence.
- Permit releasable source manifests to pin immutable main pseudo-versions
  while isolated verification continues to use the generated local `v0.0.0`
  proxy, restoring clean installation without weakening local source checks.

### Changed

- Record the completed owned package-audit boundary without treating
  specialist-owned work or retained content-identical evidence as a reset.
- Rename the unpublished webhook adapter packages to target-oriented
  `idempotency`, `slog`, `outbox`, `queue`, and `otel` paths before v1.
- Rename the unpublished outbox OpenTelemetry adapter to the target-oriented
  `outbox/adapters/otel` module path before v1.
- Rename the unpublished event-sourcing outbox and queue adapters and the
  outbox queue adapter to target-oriented module paths before v1.
- Rename the unpublished rule-engine adapters to target-oriented `math`,
  `measurement`, and `temporal` module paths before their first public release.
- Treat unavailable historical mutation identities as absent reuse candidates
  instead of aborting current-input mutation verification; current identity
  calculation and mutation execution remain fail-closed.
- Run package benchmark targets through the same isolated module graph as
  tests, analyzers, and mutation instead of loading unrelated workspace modules.
- Remove the final module-specific pre-v1 release exception so every currently
  unpublished releasable module plans its first public tag as `v1.0.0`.
- Keep Kafka and Verkle Tree implementation and verification outside the
  repository-hardening execution lane while consuming their specialist-owned
  evidence at the final repository release boundary.
- Add the repository-wide hardening report that records the canonical
  contract, pinned toolchain, package-attributable evidence, interoperability
  scope, and final release-readiness boundary.
- Expand release selections to every transitive owned dependency and verify
  them in dependency-first order before the requested module.
- Default an unpublished module's first public release to stable `v1.0.0`,
  allow strict module-local release metadata to select another canonical
  initial version, and verify dry-run consumers against that exact version
  while keeping `v0.0.0` strictly internal to unpublished workspace checks.
- Generalize the worker-balancing goal for applications with 30--40 queues,
  bounded per-queue scaling demand, and a small number of autoscaled worker
  groups instead of one Kubernetes workload per queue.
- Define the cross-module worker-balancing goal for priority-aware per-queue
  reservations, limits, borrowing, runtime convergence, and HPA/KEDA ownership.
- Use local `v0.0.0` requirements by default while allowing releasable source
  manifests to pin exact main pseudo-versions for clean consumers.
- Use `v0.0.0` for isolated local-proxy verification and resolve public
  clean-consumer checks from `main`.
- Restrict releasable-module gate fingerprints to their files and declared
  owned-dependency closure, excluding independently versioned nested modules so
  adapter changes do not invalidate parent-module evidence.
- Disable inherited Git file-system monitoring inside clean verification
  snapshots so checkout and module gates cannot hang on global fsmonitor state.
- Build CodeQL targets from each owning module, compile every cataloged
  production package to an isolated output, provision a second pinned
  JavaScript engine for ECMAScript differential checks, and execute API tools
  against isolated module sums.
- Resolve external tools through the configured complete module proxy instead
  of an incomplete runner cache, isolate CodeQL build outputs per module, and
  fail when a cataloged module resolves no buildable package.
- Catalog non-default production build tags so CodeQL compiles tagged harnesses,
  and provision the pinned shell required by CLI documentation checks.
- Upload root-module evidence from the valid artifact root rather than the
  rejected `.artifacts/.` path.
- Restrict release verification matrices to modules explicitly cataloged as
  releasable instead of failing on root tooling, fixtures, and harnesses.
- Preserve the WSDL module's provenance-pinned W3C SOAP encoding fixture
  byte-for-byte across source archives and clean verification snapshots.
- Reuse and reset each verification lane's injected PostgreSQL service for
  state-machine gates, avoiding competing Testcontainers reapers and state.
- Make the HTTP admission waiter-bound test use durable synchronization so
  heavily parallel coverage runs cannot lose its handler-entry signal.
- Treat an explicit root local-proxy selection as the complete releasable
  catalog while continuing to reject unknown module selections.
- Include nested child modules and their owned dependency closure in isolated
  proxies so parent-owned gates can enter child modules without network drift.
- Restrict knapsack evidence fingerprints to Git-controlled source inputs so
  ignored local dependency trees cannot make clean snapshots appear stale.
- Exclude the shared verification artifact mount from snapshot Git inputs so
  root evidence remains valid after parallel child checkpoints are written.
- Keep dependency updates writable only in isolated module files, avoid leaking
  module-only flags into child tools, and preserve globally ignored
  interoperability locks in clean verification snapshots.
- Isolate verification runs in dirty-tree-preserving source snapshots, support
  bounded parallel jobs, and serialize duplicate evidence writers so concurrent
  checks cannot invalidate or overwrite one another.
- Preserve gate checkpoints by complete input fingerprint so a concurrent
  snapshot for different inputs cannot overwrite proof needed by an active
  aggregate verification run.
- Isolate root digest fixtures from the repository Go wrapper so the
  CI-equivalent clean module environment exercises fixture-local modules.
- Refresh all stale owned-module checksums against the final consolidated
  source archives through the canonical isolated tidy command.
- Pass isolated module flags to supported outer Go commands and documentation
  through command-local environment without leaking a temporary modfile into
  nested test processes or versioned-tool commands.
- Install versioned analysis tools outside the target module, then execute
  them against its isolated module graph so nested modules are linted instead
  of being misreported as empty.
- Generate CycloneDX SBOMs with the same external-tool isolation so the
  generator cannot corrupt or misread the module graph it is documenting.
- Use deterministic execution counts for canonical fuzz smoke gates while
  preserving explicit duration overrides for extended fuzz campaigns.
- Recognize valid fuzz targets regardless of parameter naming, honor explicit
  `fuzz-smoke` targets, and fail when a declared fuzz gate executes nothing.
- Reuse reset-safe mutation checkpoints by exact package inputs and report
  digests so unrelated repository changes cannot trigger redundant campaigns.
- Resolve mutation fingerprint dependencies through the canonical workspace so
  isolated callers and local runs produce the same content identity, with
  exact legacy-fingerprint migration for existing checkpoints.
- Match the webhook public-vector secret allowlist at both repository and
  module scan roots without broadening it beyond the exact fixture.
- Consolidated the standalone libraries into independently versioned modules
  under `pkg/` with canonical `github.com/faustbrian/golib/pkg/...` paths.
- Replaced fragmented package workflows with one changed-module and
  reverse-dependant CI matrix backed by the root command surface.
- Isolate local-proxy module checks behind temporary module and checksum files,
  so owned source changes cannot invalidate committed release checksums while
  external dependency drift remains fail-closed.
- Standardized repository governance, module inventory, dependency graph,
  exact coverage, mutation, security, service, and release policies.
- Keep content-addressed mutation proof valid when only owned module archive
  checksums change, while retaining executable code and observer inputs.
- Added a portable production-source safety scan and fuzz discovery that do
  not depend on ripgrep, and made the safety and advisory NilAway checks part
  of the canonical module contract.
- Replaced regex-based unsafe detection with a syntax-aware scanner that
  distinguishes imports and directives from ordinary string literals.
- Execute a module's explicit API compatibility script when no equivalent
  Make target exists, while still failing declared gates with no command.
- Run module fallbacks only when no matching Make target exists, so failures
  in package-owned documentation, fuzz, API, or interoperability gates cannot
  be masked by a successful fallback.
- Added repository-level workflow linting and build-constraint-aware gate
  selection so explicit harness modules are not assigned inapplicable checks.
- Classified every discovered package by module ownership, lifecycle,
  executable production behavior, and exact-coverage applicability, with
  fail-closed filesystem and build-constraint discovery.
- Declared required integration test tags and pinned backend identity
  variables in the module catalog so local and CI test, race, and coverage
  gates exercise the same backend-dependent behavior.
- Replaced package-local mutation dispatch with one manifest-driven runner
  that mutates every executable production package and accepts only nonempty
  reports containing real killed outcomes at exact 100% thresholds.
- Defined content-addressed gate evidence so history rewrites and unrelated
  commits cannot trigger expensive reruns when every behavior-affecting input
  is unchanged, and required atomic per-unit checkpoints as soon as results
  are received.
- Separate mutation campaign inputs from evidence-orchestration bookkeeping,
  and migrate the exact report-hash-pinned checkpoints retained across the
  repository's history-only reset without rerunning proven mutant campaigns.
- Re-anchor the exact mutation checkpoint migration ledger after the approved
  follow-up squash removed its previous replacement commit object.
- Replace the reset ledger's commit-object dependency with a deterministic
  repository-content fingerprint so repeated history-only rewrites cannot
  invalidate exact retained evidence.
- Narrowed mutation fingerprints to the compiled source, tests, embedded
  data, conventional fixture corpora, module manifests, owned dependencies,
  and exact tooling used by integration-mode mutation runs. Documentation
  edits now preserve valid evidence, while executable module changes correctly
  invalidate every checkpoint whose mutant command runs `go test ./...`.
  Content-identical legacy checkpoints migrate through current or historical
  input comparison without executing mutants again.
- Reuse one immutable full-module coverage profile across package-attributable
  mutation runs, avoiding repeated integration-suite coverage collection while
  preserving the same `go test ./...` observer set for every mutant.
- Correct interoperability-tool discovery for modules beneath `pkg/` and keep
  the XSD catalog aligned with its XML Schema 1.0 Second Edition scope.
- Fail when a module declares interoperability tools without an executable
  gate instead of reporting the missing proof as not applicable.
- Route benchmark checks through package-owned harnesses, persist attributable
  output atomically, and fail when no Go benchmark actually ran.
- Run generic benchmark fallbacks against the checked-out workspace so owned
  dependencies are measured before their initial public tags exist.
- Override package-local `GOWORK=off` defaults for benchmark dispatch so every
  owned dependency resolves from the same canonical checkout.
- Prevented root module documentation checks from recursively invoking the
  repository-wide documentation orchestrator.
- Replaced revision-only mutation artifacts with content-addressed,
  per-package atomic checkpoints that survive interruptions and unrelated
  repository history changes.
- Added deterministic local module-proxy verification so isolated pre-release
  gates resolve the current owned dependency graph without workspace or
  module replacements, while public tag resolution remains a separate gate.
- Added dependency-ordered module execution and an isolated tidy command so
  owned dependency checksums are generated from the final source snapshot.
- Made every non-tidy isolated gate read-only for module metadata so checks
  cannot silently repair missing requirements or checksums while they run.
- Bound isolated module-cache storage to one temporary gate lifecycle while
  reusing the host download cache for immutable external dependencies.
- Refresh only owned release-candidate checksums during explicit tidy runs so
  source-snapshot changes cannot weaken external dependency authentication.
- Validate the repository license once while excluding only the canonical
  owned-module namespace from `go-licenses`; external dependency licenses
  remain fail-closed.
- Added canonical module export baselines and an `api-update` command so every
  declared API compatibility gate has an executable fail-closed implementation.
- Run WSDL interoperability through the same digest-pinned Java container
  locally and in CI instead of masking host-runtime dependencies in CI setup.
- Scope repository secret scanning away from generated artifacts and narrowly
  documented public fixtures while retaining the complete default rule set.
- Require every releasable module to carry a nonempty module-local licence so
  normal Go module archives and generated SBOMs retain distribution terms.
- Add a first-class fail-closed conformance gate for every public module that
  declares a specification or official corpus, separate from interoperability,
  with atomic attributable output for both gates.
- Persist every module gate result immediately as an atomic log and
  machine-readable checkpoint, and reuse successful evidence only after its
  complete-input fingerprint and log checksum validate. Revalidation preserves
  the original execution revision instead of rerunning proof after
  history-only changes, while interrupted gates discard only their incomplete
  temporary unit and exit immediately.
- Inventory every historical goal with a requirement hash, implementation
  evidence, and the canonical verification contract. Emit a fail-closed
  module traceability report only after every gate checkpoint remains current
  and its attributable log checksum validates.
- Limit mutation fingerprints for owned dependency modules to production
  inputs, because their test suites are not observers of another module's
  mutants. Tests and fixtures from every package in the module under mutation
  remain part of its complete input identity.
- Make the canonical module runner compatible with the Bash version shipped
  by macOS instead of requiring Bash 4 array-loading builtins.
- Apply the canonical root secret-scanning policy to every module gate rather
  than silently falling back to Gitleaks defaults from nested directories.
- Keep fuzz discovery within the selected `go.mod` boundary so independently
  checked child modules are not misreported as parent packages.
- Export the canonical pinned tool manifest to package-owned gates and require
  every API compatibility script to use the same current `apidiff` revision.
