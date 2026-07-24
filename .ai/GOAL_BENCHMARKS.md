# Goal: Build Honest Comparative Benchmarks

## Mission

Build a reproducible comparative benchmark system for every implemented module
in `golib`. For each package, identify maintained direct competitors,
adjacent alternatives, and raw standard-library or backend baselines, then
compare only behavior that is genuinely equivalent.

The goal is to know whether each package is operationally competitive and to
find concrete optimization opportunities. It is not to manufacture a chart
that makes owned packages look fastest by disabling correctness, validation,
durability, output, safety, or lifecycle behavior in only one implementation.

Being slower is not automatically a release blocker when the package provides
stronger semantics. Every material difference MUST be measured, explained,
and either accepted deliberately or improved. For equivalent behavior, the
target is to meet or outperform credible maintained alternatives in latency,
throughput, allocation, and memory without weakening correctness or safety.

## Non-Negotiable Fairness Rules

Before a result may be compared or published:

- Define the user-visible operation and expected output precisely.
- Prove every implementation produces semantically equivalent results.
- Enable equivalent validation, decoding, encoding, error collection,
  durability, retries, acknowledgement, synchronization, and cleanup.
- Use identical logical fixtures and equivalent precomputed state.
- Put equivalent setup inside or outside the timed region.
- Separate cold start, construction, compilation, warm steady state, and
  teardown rather than mixing them selectively.
- Use the same Go version, compiler flags, operating system, architecture,
  hardware, `GOMAXPROCS`, GC policy, process isolation, and dependency service
  versions.
- Pin every compared dependency and record its module version and commit.
- Do not compare an in-process function call with a socket round trip.
- Do not compare asynchronous enqueue acknowledgement with completed durable
  processing.
- Do not compare memory-only behavior with Redis, Valkey, PostgreSQL, NATS,
  NSQ, RabbitMQ, S3, or another external backend.
- Do not compare validation-only output with detailed diagnostic output.
- Do not compare a cached or precompiled path against a parse-and-compile path.
- Do not disable safety limits, body cleanup, transactions, fsync, TLS,
  serialization, or authentication for only one candidate.
- Do not omit failed, timed-out, or incorrect competitor results silently.
- Do not cherry-pick the best run, architecture, fixture, or concurrency level.
- Publish harness source, fixtures, configuration, raw Go benchmark output,
  statistical analysis, failures, and known limitations.

If semantics cannot be aligned, create separate tracks and state that the
results are not directly rankable.

## Comparison Classes

Every package benchmark plan MUST classify each result as one of:

1. **Raw baseline**: standard library, native SDK, backend client, direct SQL,
   or the smallest correct hand-written implementation. This measures owned
   abstraction overhead but is not a feature-equivalent competitor.
2. **Common-denominator comparison**: every implementation performs the same
   minimal supported behavior and returns equivalent output.
3. **Feature-equivalent comparison**: equivalent production policies and
   guarantees are enabled across implementations.
4. **Full owned behavior**: the owned package runs its complete recommended
   path. This is useful for capacity planning but MUST not be ranked against a
   less capable alternative.
5. **Cross-architecture comparison**: alternatives use different transports,
   runtimes, persistence, or process models. Report separately with explicit
   caveats.

Each chart and table MUST display its comparison class.

## Competitor Selection

For every module:

- Search the current Go ecosystem before implementing the benchmark.
- Select maintained, documented, production-used alternatives with overlapping
  capabilities.
- Include the de facto standard even when it is not the fastest.
- Include a performance-focused alternative when credible.
- Include a raw baseline where abstraction overhead can be isolated.
- Exclude abandoned or incomparable projects unless retained as historical
  context, clearly labeled.
- Record stars or popularity only as selection context, never as correctness
  evidence.
- Re-evaluate the set at least before every major release.
- Pin versions for reproducibility while separately testing current releases.
- Review competitor licenses before vendoring benchmark adapters or fixtures.

The candidate matrix below is an initial plan, not an eternal allowlist. Each
candidate MUST be verified as maintained, compatible, and benchmarkable when
the harness is implemented.

## Initial Per-Package Matrix

| Owned package | Initial comparison candidates | Matched benchmark focus |
| --- | --- | --- |
| `analysis` | `go vet`, Staticcheck, individual `golangci-lint` analyzers | Same fixture corpus and equivalent analyzer rules; startup, package loading, wall time, peak memory, and diagnostics. Different rule sets MUST NOT be ranked. |
| `api-query` | hand-written parser/planner baseline, Squirrel, goqu, maintained pagination/query planners | Parse and compile identical declared filters, sorts, fields, relationships, and cursors into a normalized plan. SQL builders are adjacent baselines only. |
| `authentication` | `golang-jwt/jwt`, `lestrrat-go/jwx`, `coreos/go-oidc`, direct Basic/API-key parsing | Separate Basic, opaque bearer, API key, JWT verification, JWKS lookup, and OIDC tracks with identical algorithms, keys, claims, and cache state. |
| `authorization` | Casbin, OPA, Ladon, hand-written ACL/RBAC checks | Separate ACL, RBAC, and ABAC policies with equivalent decisions and preloaded policy state; include cold policy load separately. |
| `cache` | Ristretto, BigCache, FreeCache, ttlcache, eko/gocache, raw map with lock | Separate typed-wrapper overhead, memory backend, Redis, and Valkey tracks; match TTL, stale behavior, serialization, concurrency, and capacity. |
| `calendar` | `time`, `rickar/cal`, focused hand-written civil-date operations | Date arithmetic, period operations, business-day lookup, holiday calendars, timezone conversion, and DST boundaries as separate capabilities. |
| `circuit-breaker` | Sony gobreaker, failsafe-go, direct atomic-state baseline | Closed/open/half-open admission, rolling windows, concurrent transitions, snapshots, and observer overhead with matching policies. |
| `clock` | `time`, benbjohnson/clock, clockwork | `Now`, timers, tickers, sleep, manual advancement, callback load, and cancellation; real and fake clock paths separated. |
| `config` | Viper, Koanf, cleanenv, envconfig, direct stdlib decoding | File and environment load, layered merge, strict decode, reload, snapshot, provenance, and secret handling with equivalent formats and validation. |
| `filesystem` | `os`/`io/fs`, Afero, go-billy, native AWS S3/R2/SFTP/FTP clients | Local and memory overhead plus backend-specific tracks. Match bytes, streaming, metadata, checksums, concurrency, and network settings. |
| `geo` | paulmach/orb, geom, GeographicLib-compatible Go implementations | Coordinate parse, distance, containment, intersections, encoding, and large geometry operations with the same CRS and numerical model. |
| `http-client` | `net/http`, Resty, Req, HashiCorp retryablehttp | Plain request, JSON request, middleware chain, retries, rate limiting, pagination, cache, and circuit-breaker tracks with identical transport and policies. |
| `http-middleware` | direct `net/http` wrapping, chi middleware, Alice, Negroni, Gorilla handlers | Equivalent chain depth and matched CORS, compression, recovery, request ID, timeout, body limit, and secure-header behavior. |
| `idempotency` | direct Valkey/Redis or PostgreSQL implementation, maintained idempotency middleware where comparable | Claim, conflict, replay, lease expiry, fencing, result storage, and concurrent duplicate handling per backend. Simple request dedupe is not equivalent to durable ownership. |
| `international` | `golang.org/x/text`, go-i18n data primitives, libphonenumber ports, focused currency/locale packages | Split country, subdivision, language, locale, currency, phone, and postal operations. Do not rank packages that solve different identifier domains. |
| `json-schema` | santhosh-tekuri/jsonschema, qri-io/jsonschema, xeipuuv/gojsonschema if still maintained, Bowtie implementations as cross-language context | Separate parse/compile and validation; same draft, official fixture, output mode, reference registry, formats, and correctness gate. Only fully conforming results rank. |
| `jsonapi` | DataDog/jsonapi, google/jsonapi, api2go where maintained | Marshal, unmarshal, validate, compound documents, relationships, errors, sparse fields, Atomic Operations, and cursor profile. Common core and full-feature tracks separated. |
| `jsonrpc` | creachadair/jrpc2, filecoin-project/jsonrpc, maintained jsonrpc2 implementations | Parse, dispatch, encode, client correlation, notifications, batches, errors, middleware, and HTTP round trips with identical protocol validation. |
| `lease` | bsm/redislock, go-redsync, PostgreSQL advisory-lock baseline, etcd concurrency where relevant | Backend-matched acquire, renew, validate, release, contention, expiry, and fencing. Non-fenced locks MUST be a separate track. |
| `localized` | `golang.org/x/text/language`, go-i18n, direct map lookup | Exact lookup, language matching, fallback chains, immutable updates, and large locale sets with equivalent canonicalization. |
| `log` | `log/slog`, Zap, Zerolog, phuslu/log, established slog benchmark suites | Disabled, text, JSON, attributes, groups, errors, redaction, sampling, fan-out, sync, and async delivery. Compare identical emitted bytes and durability. |
| `migrations` | Goose, golang-migrate, Atlas versioned migrations, direct pgx baseline | Discovery, planning, status, no-op startup, apply, rollback, checksums, locking, and large histories using identical SQL and database state. CLI startup is separate from library use. |
| `opening-hours` | Daquisu/opening_hours.go, opening_hours.js as cross-language context, direct interval baseline | Parse where syntaxes overlap, `IsOpen`, next transition, exceptions, holidays, timezone/DST, and large schedules. Cross-language process overhead is not directly rankable. |
| `openrpc` | maintained Go OpenRPC document libraries if found, direct `encoding/json`, OpenRPC reference tooling as cross-language context | Parse, validate, canonicalize, resolve references, discover, compose, and diff identical documents. Raw JSON is an overhead baseline only. |
| `outbox` | Watermill SQL forwarder, maintained PostgreSQL outbox libraries, direct pgx worker | Transactional write, claim, publish, retry, ordering, cleanup, and contention with the same schema, isolation, batch, and durability. |
| `password` | direct `x/crypto` bcrypt/argon2 calls, alexedwards/argon2id, maintained password wrappers | Hash and verify per algorithm and cost, malformed hashes, upgrade detection, concurrency, memory, and constant-time behavior. Never lower cost parameters for ranking. |
| `postgres` | raw pgx/pgxpool, `database/sql` with pgx, sqlx, Bun, GORM | Separate wrapper overhead from query mapping and ORM tracks; identical schema, SQL, pool, transaction, rows, prepared statements, and network topology. |
| `queue` | Asynq, Machinery, Watermill, River as a cross-backend system, native NATS/NSQ/RabbitMQ/Redis/Valkey clients | One track per backend and delivery guarantee. Measure enqueue acknowledgement, claim, end-to-end completion, retry, delayed jobs, batches, and failure recovery separately. |
| `queue-control-plane` | Asynqmon, River UI/API, direct queue-state queries | Snapshot/list aggregation, filtering, commands, audit writes, streaming updates, and large tenant/queue cardinality. UI rendering and backend query cost separated. |
| `rate-limit` | `x/time/rate`, uber-go/ratelimit, Tollbooth, redis_rate, direct Valkey script | Token bucket, fixed window, sliding counter, and concurrency lease in separate tracks; match burst, clock, keys, backend, and atomicity. |
| `router` | Chi, Gin router, Echo router, httprouter, BunRouter, Gorilla mux, Fiber in a separate `fasthttp` track | Static, parameter, wildcard, miss, method, groups, mounts, middleware, URL generation, and realistic route tables. Fiber MUST not be ranked as a direct `net/http` equivalent. |
| `scheduler` | robfig/cron, gocron, River periodic jobs where relevant, direct heap/timer baseline | Parse, next-run calculation, dispatch, large schedules, overlap prevention, singleton leases, missed runs, and Kubernetes replicas in separate tracks. |
| `service` | plain `net/http`, Chi stack, Gin, Echo, Fiber in a separate architecture track | Startup, idle RSS, binary size, graceful lifecycle, probes, one route, JSON RPC/HTTP request, middleware stack, concurrency, and shutdown with matched features. |
| `tabular` | `encoding/csv`, csvutil, gocsv, Excelize, maintained XLS readers | Separate CSV, delimiters, fixed width, XLS, XLSX, and ZIP tracks; same rows, types, validation, streaming, formulas, shared strings, and limits. |
| `telemetry` | direct OpenTelemetry SDK, no-op OTel providers, Prometheus client where semantics overlap | Wrapper overhead, spans, metrics, propagation, batching, export, sampling, flush, and shutdown with identical exporters and attributes. |
| `temporal` | `time`, golang-module/carbon, kumparan/carbon, jinzhu/now, direct interval algorithms | Parse/format, immutable arithmetic, periods, Allen relations, set normalization, local times, recurrences, timezone/DST, and large interval sets. |
| `validation` | go-playground/validator, ozzo-validation, govalidator or maintained typed validators | Equivalent scalar, struct, nested, collection, cross-field, custom rule, success, and multi-error paths. Tag compilation and rule construction separated. |
| `webhook` | direct `crypto/hmac`, Svix webhook libraries, provider SDK verifiers such as Stripe where semantics overlap | Exact-byte verify, signing, timestamp tolerance, replay storage, secret rotation, batch delivery, retries, and HTTP round trips. Provider protocols are separate tracks. |
| `wire` | standard `encoding/json` and `encoding/xml`; yaml.v3; BurntSushi/toml; vmihailenco/msgpack; fxamacker/cbor; MongoDB BSON; jsoniter, easyjson, or Sonic where compatible | One format per track with identical data model, strictness, number semantics, limits, and output. Generated and unsafe codecs MUST be labeled separately. |

## Packages Without Direct Competitors

Absence of a direct competitor does not remove the benchmark requirement.
Such packages MUST use:

- a correct hand-written baseline for abstraction overhead;
- native backend or standard-library primitives;
- internal algorithm alternatives;
- realistic service-level capacity tests;
- historical baselines to prevent regressions.

Do not force a comparison against a library that solves a materially different
problem merely to fill a table.

## HTTP Router And Service Benchmark Design

Router and service comparisons require multiple independent suites:

1. In-process router dispatch with `httptest` or equivalent and no socket.
2. Full `net/http` loopback with keep-alive and controlled clients.
3. Full TCP/TLS request paths where production network overhead matters.
4. Framework lifecycle startup, idle memory, graceful shutdown, and probes.
5. Realistic route tables, including a pinned GitHub-style API corpus.
6. Matched middleware and JSON request/response handling.
7. Fiber/`fasthttp` as a separate architecture track with its reset and adapter
   costs disclosed.

Gin, Echo, Chi, and plain `net/http` comparisons MUST account for differing
context, middleware, binding, validation, and rendering behavior. A router-only
result MUST not be presented as a full-framework result.

## Durable And Distributed Benchmark Design

Queue, cache, lease, idempotency, rate-limit, outbox, webhook, migration,
filesystem, and PostgreSQL benchmarks MUST use reproducible containerized
dependencies where practical. Record:

- server version and configuration;
- persistence and fsync settings;
- topology and network path;
- client pool and connection settings;
- payload size and serialization;
- producer/consumer counts;
- acknowledgement and durability point;
- retries, failures, and cleanup state;
- warmup and database/cache reset procedure.

Do not use unsafe durability settings for headline numbers unless every
candidate uses them and the result is labeled non-production.

## Correctness Gate

Every comparative adapter MUST pass a shared correctness suite before its
benchmark result is accepted. The suite MUST verify output, errors, protocol
semantics, state transitions, durability point, limits, and cleanup relevant to
the tested operation.

For formal specifications, run the same conformance fixtures before timing.
An implementation that fails required correctness cannot win the benchmark;
its performance may be retained only as a clearly disqualified diagnostic.

## Harness Architecture

Create a root comparative benchmark harness that:

- keeps competitor dependencies out of production modules;
- isolates comparison dependencies in dedicated benchmark modules;
- exposes small adapters implementing benchmark-only contracts;
- validates adapter equivalence with tests;
- discovers packages and benchmark suites deterministically;
- records dependency versions and environment metadata automatically;
- emits standard Go benchmark output and machine-readable metadata;
- supports CPU, memory, block, mutex, trace, and execution profiles;
- uses `benchstat` or an equivalent statistical method;
- supports repeat counts and confidence analysis;
- stores raw results separately from generated reports;
- can run one package, one competitor, one fixture, or the complete matrix;
- does not require production credentials or public network services;
- cleans temporary processes, containers, files, and data after execution.

Benchmark adapter code MUST be reviewed with the same rigor as production
code because small adapter differences can invalidate all results.

## Measurement And Statistics

- Use multiple process invocations, not only repeated iterations in one
  process.
- Record median and distribution, not only the smallest number.
- Use sufficiently long benchmark times for stable measurements.
- Detect thermal throttling, CPU frequency changes, noisy neighbors, and
  background load where possible.
- Report `ns/op`, operations/second, `B/op`, allocations/op, and relevant
  service-level latency percentiles.
- Report peak and steady-state RSS for services and large workloads.
- Measure binary size and startup only when build tags and linked features are
  equivalent.
- Never combine results from different machines in one ranking.
- Keep historical trends keyed by hardware and toolchain.

CI shared runners MAY check compilation and gross regressions but MUST NOT
publish authoritative rankings. Stable comparative reports require controlled
hardware or explicitly noisy, non-gating labels.

## Reporting

Every published report MUST include:

- benchmark objective and comparison class;
- exact owned and competitor versions;
- capability and correctness matrix;
- fixture and workload definition;
- timed-region definition;
- hardware, OS, Go, GC, and service configuration;
- raw result links;
- statistical method and sample count;
- failures, exclusions, caveats, and non-equivalent features;
- latency, throughput, allocation, and memory results;
- an interpretation that separates measured fact from inference.

Do not use “fastest” without defining scope and showing current reproducible
evidence. Do not conceal cases where an alternative is faster.

## CI And Maintenance

- Compile benchmark harnesses on every relevant pull request.
- Run short correctness-backed benchmark smoke tests for changed modules.
- Run stable comparative suites on controlled scheduled runners.
- Run the full competitor matrix before major releases.
- Use dependency automation to propose competitor updates separately from
  production dependencies.
- Rebaseline intentionally when Go, hardware, fixtures, or a major competitor
  changes.
- Fail on missing metadata, correctness failures, benchmark disappearance, or
  accidental fixture reduction.
- Gate performance regressions only where the environment and statistical
  confidence justify it.

## Required Deliverables

1. A reviewed per-package competitor and capability matrix.
2. A correctness-tested root benchmark harness.
3. Reproducible local and controlled-runner commands.
4. Pinned fixtures, competitor versions, and environment manifests.
5. Raw and generated reports for each comparison class.
6. Package documentation linking to honest current results.
7. A backlog of measured performance gaps ranked by production impact.

## Completion Criteria

This goal is complete only when:

- every implemented package has either credible direct comparisons or a
  documented reason and raw baseline strategy;
- every ranked competitor passes the same correctness contract;
- setup, semantics, outputs, persistence, and timed regions are equivalent;
- architecture differences are split into separate tracks;
- all harnesses and fixtures are reproducible from a fresh clone;
- raw results and environment metadata are published with every report;
- statistical analysis is repeatable and not based on cherry-picked runs;
- performance claims are scoped, current, and supported by evidence;
- measured gaps have an explicit fix or acceptance decision;
- no optimization used to improve ranking weakens correctness, conformance,
  durability, security, cancellation, or resource bounds.
