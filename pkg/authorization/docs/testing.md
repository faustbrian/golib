# Authorization testing

The `authorizationtest` package provides deterministic fixtures without
requiring an application framework or a storage service. Its default request
uses stable identifiers and a fixed UTC time; each `Build` call returns fresh
groups and attribute maps.

```go
request := authorizationtest.NewRequest().
    WithSubject(authorization.SubjectServiceAccount, "worker-1").
    WithAction("invoice.refund").
    WithResource("invoice", "invoice-1").
    WithTenant("tenant-1").
    Build()
```

`AllowEvaluator`, `DenyEvaluator`, `NotApplicableEvaluator`, and
`ErrorEvaluator` are small policy fixtures. `MustSnapshot` and `MustEngine`
keep test setup concise, while `RequireDecision` checks the complete decision
including revision, matched policy IDs, and trace.

Use `CanonicalDecisionJSON` for reviewable golden files. It uses stable field
ordering and textual outcomes. Preserve matched-policy and trace order because
they describe evaluation order and combining behavior.

## Integration conformance

`RunAuthorizerConformance` verifies that an adapter preserves allow and deny,
defaults a non-applicable result to deny, fails closed for evaluation errors
and invalid requests, and respects canceled contexts:

```go
authorizationtest.RunAuthorizerConformance(t, func(
    evaluator authorization.Evaluator,
) authorization.Authorizer {
    snapshot := authorizationtest.MustSnapshot(t, 1,
        authorization.DenyOverrides,
        authorization.PolicyDefinition{
            ID: "conformance",
            Evaluator: evaluator,
        },
    )
    return authorizationtest.MustEngine(t, snapshot)
})
```

Applications should run the same suite against their outer authorization
adapter, not only against the core engine. That catches accidental conversion
of internal errors into allows and loss of cancellation behavior.

## Mutation testing

`./scripts/check-mutation.sh` runs the pinned Gremlins release against all
production packages. The repository enforces at least 85 percent test efficacy
and 95 percent mutant coverage. These thresholds are based on a complete local
baseline rather than a dry run and should only move upward.

Timed-out mutants are reported separately because context, synchronization,
and polling mutations can deliberately prevent termination. Lived mutants are
not equivalent to defects, but each is a prompt to decide whether an assertion,
an implementation simplification, or a documented equivalent mutant is
appropriate.
