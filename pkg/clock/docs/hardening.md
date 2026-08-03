# Hardening evidence

The maintained local gate is:

```sh
make install-tools
make check staticcheck lint nilaway vuln benchmark mutation
```

It covers formatting, module tidiness, vet, unit/state-machine tests, 100.0%
production statement coverage, race execution, fuzz smoke, leak repetition,
forbidden-runtime scans, documentation examples, API compatibility, workflow
validation, Staticcheck, strict golangci-lint, advisory NilAway, govulncheck,
benchmarks, and mutation testing.

`make mutation` now delegates to the canonical content-addressed repository
runner. It requires exact 100% efficacy and mutant coverage, with every viable
mutant killed. Survivors, timeouts, uncovered mutants, malformed reports,
missing packages, and unclassified results fail closed. Earlier standalone
campaigns with accepted survivors or timeouts are historical and are not
release evidence.

Race stress uses 32 concurrent lifecycle workers plus a shutdown race spanning
advance, reset, stop, wait, callback, cancellation, jump, and shutdown. Fuzz
smoke covers bounded operation sequences, the complete signed duration domain,
callback panic and reentrancy, cancellation, and resource limits. Leak checks
repeat callback drain and shutdown paths. Benchmark baselines are documented
in [performance.md](performance.md).

Hosted GitHub Actions are the final external verification after local work is
complete; they are not used as a reason to stop local implementation.

The final resource audit added immediate removal of reset/stopped heap entries
and an explicit cap on outstanding advancement waiters. Regression tests prove
both bounds while callbacks are active. Representative downstream verification
is recorded in [integration.md](integration.md).

The resource audit also added direct Go `time` differential checks, JSON
monotonic-loss proof, a timer/ticker/callback synctest composition test,
explicit wall/monotonic jump scenarios, a blocking repeated race-stress target,
and cold/contended benchmark baselines.

## Requirement evidence matrix

| Audit area | Authoritative evidence |
| --- | --- |
| Timer/ticker states and boundary durations | `docs/state-machines.md`; `TestTimerStopResetAndDrainLifecycle`; `TestDurationOverflowAndUnderflowAreRejected`; `TestResetErrorsPreserveLifecycleState` |
| Go standard-library alignment | `TestSystemTimerAndCallbackDifferentialAgainstTime`; system lifecycle and invalid-input tests; `docs/compatibility.md` |
| Ordering and callback reentrancy | `TestAdvanceFiresEventsByTimestampThenRegistration`; nested waiter association tests; `TestCallbackCanStopAndResetAdditionalClockWork`; callback create/wait tests |
| Waiter quiescence and recursion limits | `TestInternalNestedWaiterIncludesLaterSameInstantCallback`; request-relative result tests; work-limit fan-out tests |
| Concurrency, panic, shutdown, and leaks | repeated `make stress`; full `make race`; callback panic tests; repeated leak target; internal heap-release tests |
| Wall, monotonic, persistence, and synctest | independent jump tests; JSON round-trip test; `clocktest` bubble suites; semantic guide and compatibility matrix |
| Resource and observation budgets | active/waiter/work-limit tests; tag boundary tests; bounded observation type; security scan |
| Fuzz, mutation, compatibility, and performance | `make fuzz mutation api benchmark`; canonical repository evidence; `docs/performance.md` |
| Release automation and advisory analysis | `make workflows`; blocking CI/release workflows; visible non-blocking `make nilaway` |

The test names above are stable executable contracts, not line-coverage
proxies. The release command reruns them with race detection, fuzz seeds,
mutation operators, leak repetition, and the repository's exact coverage gate.
