# Five-minute ABAC quickstart

ABAC evaluates typed attributes supplied by the application on the subject,
resource, request, and environment. Conditions use a closed expression tree;
applications cannot inject arbitrary callbacks, reflection-driven operators,
I/O, or executable policy text.

```go
condition := abac.All(
    abac.Equal(
        abac.Reference{
            Source: abac.Subject,
            Name:   "department",
        },
        authorization.StringValue("finance"),
    ),
    abac.Equal(
        abac.Reference{
            Source: abac.Request,
            Name:   "mfa",
        },
        authorization.BoolValue(true),
    ),
)

attributesPolicy, err := abac.New(
    []abac.Rule{
        {
            ID:           "finance-can-read-reports",
            Tenant:       "acme",
            Action:       "report.read",
            ResourceType: "report",
            Effect:       authorization.Allow,
            Condition:    condition,
        },
    },
    nil,
)
if err != nil {
    return err
}

decision, err := attributesPolicy.Evaluate(ctx, authorization.Request{
    Subject: authorization.Subject{
        Kind: authorization.SubjectUser,
        ID:   "alice",
        Attributes: authorization.Attributes{
            "department": authorization.StringValue("finance"),
        },
    },
    Action: "report.read",
    Resource: authorization.Resource{
        Type: "report",
        ID:   "quarterly",
    },
    Tenant: "acme",
    Attributes: authorization.Attributes{
        "mfa": authorization.BoolValue(true),
    },
})
if err != nil {
    return err
}

// decision.Outcome is authorization.Allow.
```

The evaluator implements `authorization.Evaluator`, so it can be composed with
ACL and RBAC evaluators in one revisioned root snapshot.

## Typed values and sources

Attribute values are created with explicit constructors for null, string,
boolean, integer, finite float, time, IP address, and string set values. ABAC
does not coerce between kinds: an integer and a float are different types even
when they print the same value.

References select one of four sources:

- `abac.Subject` reads `Request.Subject.Attributes`;
- `abac.Resource` reads `Request.Resource.Attributes`;
- `abac.Request` reads `Request.Attributes`; and
- `abac.Environment` reads `Request.Environment.Attributes`.

Attribute maps are request inputs. Evaluators only read them; applications must
not mutate a request concurrently with evaluation.

## Missing, null, mismatch, and negation

Missing, explicit null, and type mismatch are distinct non-matching statuses.
`Exists` and `IsNull` handle those cases deliberately. Other operators do not
turn missing or mismatched data into a match. In particular, `Not` preserves
missing, null, and mismatch as non-matches rather than treating absent data as
the negation of a comparison.

`EvaluateCondition` returns the status and consumed cost when an application
needs to test or explain a condition independently of a rule.

## Operators

The bounded operator set includes:

- `Equal`, `Exists`, and `IsNull`;
- `All`, `Any`, and `Not`;
- `GreaterThan` and `LessThan` for same-kind string, integer, float, and time
  values;
- `In` and `SetContains`;
- `HasPrefix`, `HasSuffix`, and `StringContains`; and
- `IPIn` for CIDR membership.

There are no user-defined operator callbacks. Business invariants that need
domain data or I/O remain in application code and should be supplied as a
trusted typed attribute only when that tradeoff is intentional.

## Versioned named conditions

Named conditions let several rules reuse a validated behavior while pinning a
version:

```go
named := []abac.NamedCondition{
    {
        Name:      "mfa-present",
        Version:   1,
        Condition: abac.Exists(mfaReference),
    },
}

rule := abac.Rule{
    ID:               "sensitive-read",
    Action:           "document.read",
    ResourceType:     "document",
    Effect:           authorization.Allow,
    ConditionName:    "mfa-present",
    ConditionVersion: 1,
}
```

Construction rejects missing versions, duplicate name/version pairs, unknown
references, and invalid named expressions.

## Precedence and bounds

Rules are evaluated by descending priority and then stable ID. Every matching
rule remains subject to explicit deny-overrides; priority controls deterministic
order and explanation, not whether a deny is honored.

`abac.WithLimits` bounds rules, named conditions, nesting depth, expression
cost, literal and runtime set cardinality, matched rules, and batch size.
Cancellation or budget exhaustion returns a deny decision and an error. A
condition never performs network or database I/O while consuming its budget.
