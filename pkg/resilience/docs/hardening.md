# Hardening evidence

This matrix identifies the executable evidence for the package-specific goal.
Repository gate checkpoints remain authoritative for release status.

| Requirement | Evidence |
| --- | --- |
| Deterministic policy composition | `TestGeneratedPolicyStacksMatchReferenceInvocationOrder`, `FuzzPolicyStackOrderAndEventHistory` |
| Invalid and repeated policy handling | `TestExecutorRejectsInvalidCompositionsBeforeExecution`, `TestExecutorAcceptsOnlyFullyCompatibleRepeatedPolicies` |
| Shared retry and hedge budget | `TestBudgetSharesRetryAndHedgeAmplificationLimits`, `FuzzBudgetStateMachine` |
| Concurrent accounting | `TestBudgetConcurrentAdmissionNeverExceedsConfiguredCapacity`, race and stress gates |
| Panic and cancellation cleanup | `TestExecutorReleasesBudgetPermitWhenOperationPanics`, `TestDetachedPolicyReceivesCancellationAfterAttemptStarts` |
| Uncooperative operations | `TestUncooperativeOperationRemainsCallerOwnedAndSynchronous` |
| Observer safety | `TestObserverCanReenterExecutorWithoutDeadlock`, observer panic tests |
| Kubernetes replica semantics | `TestReplicaLocalBudgetsMultiplyClusterCapacity`, `TestMixedReplicaRevisionsKeepIndependentLimits` |
| Termination and abrupt loss | `TestPodTerminationCancellationRemainsCallerOwned`, `TestAbruptLossRecoversOnlyExpiredConcurrentCapacity` |
| Bounded hostile inputs | `FuzzMetadataAndAttempts`, boundary tests, exact identity limits |
| Exact statement coverage | `scripts/check-coverage.sh` rejects every production package below 100% |
| Viable mutation effectiveness | canonical repository mutation gate requires 100% killed and 100% covered |
| API compatibility | generated `api/baseline.txt` checked by `scripts/check-api.sh` |
| Comparative performance | [published benchmark report](benchmarks/2026-08-02-darwin-arm64.md) |

The focused `retry` and `hedge` packages must still consume the shared budget
contract before the repository-wide resilience goal can be declared complete.
