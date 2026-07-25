# Hardening report

This report records the audit findings, their evidence, impact, and current
disposition. Severity reflects production worker impact, not patch size.

| ID | Severity | Area | Evidence and reproduction | Impact | Disposition |
| --- | --- | --- | --- | --- | --- |
| H-01 | High | Ring | Concurrent queue/request race tests under `-race` | Corrupt capacity decisions | Fixed by serializing ring state |
| H-02 | High | Lifecycle | release-before-start deadlocked; repeated start launched schedulers | Deadlock or duplicate processing | Fixed with idempotent startup and drain transition |
| H-03 | High | Core callbacks | Panicking observer/metric/logger/after callback regressions | Process exit or corrupted worker count | Fixed with panic boundaries and authoritative accounting |
| H-04 | High | Settlement | Ack/nack callback panic regressions | Process exit after side effects | Fixed; panic becomes settlement failure |
| H-05 | High | Redis Streams | Message queued before first request was skipped by group at `$` | Silent durable-work loss | Fixed; group starts at `0` |
| H-06 | High | Redis Streams | Shutdown re-added a PEL delivery while retaining the original | Duplicate durable entry | Fixed; original remains pending |
| H-07 | High | Broker input | All delivery decoders accepted unbounded JSON and arbitrary retry state | Memory/CPU denial of service | Fixed at 1 MiB, 100 retries, validated finite timing |
| H-08 | High | RabbitMQ | Durable entities used transient messages and no publisher confirms | Publish could be lost while reported successful | Fixed with persistent messages and bounded confirms |
| M-01 | Medium | RabbitMQ | Malformed manual-ack delivery remained unsettled | Infinite poison requeue | Fixed with confirmed terminal publish before source ack |
| M-02 | Medium | NSQ | Malformed delivery remained unfinished | Infinite poison redelivery | Fixed by FIN plus decode error |
| M-03 | Medium | Redis logging | Debug dump included connection URI/options | Credential disclosure | Fixed with a redacted summary regression |
| M-04 | Medium | RabbitMQ | Publish used `context.Background()` | Enqueue could block indefinitely | Fixed with validated five-second default timeout |
| M-05 | Medium | In-memory | Default ring capacity was unlimited | Admission-driven memory exhaustion | Fixed with 10,000 default; explicit zero remains an unsafe escape |
| M-06 | Medium | Configuration | Non-positive retry interval reached `time.NewTicker` | Scheduler goroutine panic | Fixed by constructor validation |
| M-07 | Medium | Integration | Floating images and Redis cluster advertised container-only nodes | Non-reproducible or skipped evidence | Fixed with digests and portable all-slot cluster setup |
| M-08 | Medium | Redis Pub/Sub | Immediate publish after constructor failed under `-race` | Healthy-start message loss | Fixed by awaiting the Redis `SUBSCRIBE` acknowledgement |
| M-09 | Medium | Integration | 143 legacy test sleeps included synchronization delays | Slow and potentially flaky evidence | Fixed; five remaining sleeps deliberately create timeout or benchmark load |
| M-10 | Medium | Failure injection | Broker restart and interruption behavior lacked real evidence | Reconnect/loss claims were not fully proved | Fixed with same-endpoint stop/start tests; packet-level half-open behavior is not a package guarantee |
| M-11 | Medium | Credentials | Malformed Redis, NATS, and AMQP URLs were repeated by client errors | Constructor logging could expose passwords | Fixed with safe error text that preserves programmatic cause identity |
| H-09 | High | Valkey settlement | Handler completion could be confused with stream delivery | Premature acknowledgement and lost crash recovery | Fixed with deferred ack, pending reclaim, and native Valkey 9 evidence |
| H-10 | High | Valkey lifecycle | Blocking reads, reclaim scans, and shutdown needed explicit ownership | Goroutine or connection leaks during outage | Fixed with cancellation, bounded waits, forced close, race, and leak gates |
| M-12 | Medium | Valkey errors | Native dial/command errors could repeat endpoints or metadata | Sensitive deployment detail disclosure | Fixed with safe public text and retained programmatic causes |
| M-13 | Medium | Valkey DLQ | Append and source ack are not one atomic command | Duplicate terminal entries after ambiguous ack | Accepted at-least-once boundary; original ID is recorded for deduplication |
| L-01 | Low | API | Legacy constructors panic | Startup crash if error is not handled | Retained for compatibility; `NewWorkerE` is required in production |
| L-02 | Low | Redis Streams | Failed jobs remain in PEL without automatic claim/DLQ | Pending growth without operations | Accepted policy boundary; monitor and operate explicitly |
| L-03 | Low | Handlers | Go cannot forcibly stop code that ignores its context | User goroutine/resource retention | Accepted language boundary and documented requirement |

## Verification scope

Each behavioral correction began with a failing regression. Production
statement coverage remains 100%, delivery, option, native-response, and
message-state fuzz targets cover every network wrapper, and race/static/vulnerability/workflow gates are part of
the final command set in this report.

## Release verdict

The 2026-07-15 audit verdict is **go** for a pre-v1 hardening release. Every
high- and medium-severity finding is fixed, the final local and tagged backend
gates pass without skips, production statement coverage is 100%, and the
remaining low-severity risks are explicit operational or compatibility
boundaries rather than hidden delivery guarantees.

The acknowledgement, retry, payload, startup, callback, and default-capacity
changes are SemVer-significant; the recommended release is a pre-v1 minor. If
released after v1, they require a major version because delivery and admission
semantics changed.

## Latest gate results

| Command | Result |
| --- | --- |
| `make check` | Pass, including safety, race, fuzz, coverage, benchmarks, docs, and vulnerability scan |
| `make integration` | Pass for every tagged backend without skips |
| `go test ./... -count=1 -timeout=60s` | Pass |
| `go test -race ./... -count=1 -timeout=120s` | Pass |
| `scripts/check-coverage.sh` | Pass, 100% production statements |
| `scripts/check-docs.sh` | Pass |
| `go vet ./...` | Pass |
| `staticcheck@v0.6.1 ./...` | Pass |
| `govulncheck@v1.6.0 ./...` | Pass, no called vulnerabilities |
| `actionlint@v1.7.7` | Pass |
| `FUZZ_TIME=2s scripts/check-fuzz.sh` | Pass for all targets |
| `BENCH_TIME=10x make benchmark` | Pass |
| `go test -tags=integration -count=1 -timeout=15m ./valkeystream` | Pass without skips against pinned Valkey 9.1.0 |
| `go test -race -tags=integration -count=1 -timeout=15m ./valkeystream` | Pass |
