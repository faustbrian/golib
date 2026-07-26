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

The initial mutation run on 2026-07-16 discovered 173 runnable mutations with
100% mutant coverage. It killed 105 of 155 scored mutants, for 67.74% efficacy;
18 deadlocking mutants timed out. Surviving boundary mutants were audited:

- meaningful survivors around independent resource limits, non-zero elapsed
  subtraction, nested waiter result arithmetic, empty shutdown, and observation
  outcomes received stronger assertions;
- equivalent mutations include generation changes after an object is already
  inactive, zero-duration `> 0` boundary changes, and diagnostic arithmetic
  that cannot change whether work fires;
- the release gate requires 100% mutant coverage and at least 65% efficacy, and
  keeps the full JSON result available as a local artifact.

The final release-tree run evaluated 166 mutations. It killed 134 of 149 scored
mutants, timed out 17 deadlocking mutants, covered every mutant, and reached
89.93% efficacy. All 15 survivors are equivalent under maintained invariants:
strict heap comparisons have unique sequence keys, popped indices are never
reused, zero-duration boundary variants add or wait for zero time, empty
shutdown failure is a no-op, and target-selection variants take redundant
coordinator steps without changing request completion or ordering. Meaningful
resource counts, failed-reset rollback, observation outcomes, callback
association, and request-relative result arithmetic are mutation-detecting.

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

The final 2026-07-17 release-tree audit reran the mutation gate with unchanged
thresholds. It evaluated 174 mutations: 138 killed, 15 equivalent survivors,
21 deadlocking timeouts, 100.00% mutant coverage, and 90.20% efficacy. The
survivor audit found only invariant-equivalent strict-boundary substitutions;
meaningful target-selection and callback-progress negations are killed. The
audit also added direct Go `time` differential checks, JSON monotonic-loss
proof, a timer/ticker/callback synctest composition test, explicit
wall/monotonic jump scenarios, a blocking repeated race-stress target, and
cold/contended benchmark baselines.

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
| Fuzz, mutation, compatibility, and performance | `make fuzz mutation api benchmark`; `mutation-results.json`; `docs/performance.md` |
| Release automation and advisory analysis | `make workflows`; blocking CI/release workflows; visible non-blocking `make nilaway` |

The test names above are stable executable contracts, not line-coverage
proxies. The release command reruns them with race detection, fuzz seeds,
mutation operators, leak repetition, and the repository's exact coverage gate.
