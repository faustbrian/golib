# Five-minute ACL quickstart

ACL entries directly allow or deny a subject an action on a resource type or a
specific resource. The core engine supplies revisioning, default deny,
explanation, activation windows, and composition.

```go
entries := []acl.Entry{
    {
        ID: "alice-can-read-documents",
        Subject: authorization.Subject{
            Kind: authorization.SubjectUser,
            ID:   "alice",
        },
        Action:       "document.read",
        ResourceType: "document",
        Effect:       authorization.Allow,
    },
    {
        ID: "alice-cannot-read-secret-document",
        Subject: authorization.Subject{
            Kind: authorization.SubjectUser,
            ID:   "alice",
        },
        Action:       "document.read",
        ResourceType: "document",
        ResourceID:   "secret",
        Effect:       authorization.Deny,
    },
}

accessList, err := acl.New(entries)
if err != nil {
    return err
}

snapshot, err := authorization.NewSnapshot(
    1,
    authorization.DenyOverrides,
    authorization.PolicyDefinition{
        ID:        "document-acl",
        Evaluator: accessList,
    },
)
if err != nil {
    return err
}

engine, err := authorization.NewEngine(snapshot)
if err != nil {
    return err
}

decision, err := engine.Decide(ctx, authorization.Request{
    Subject: authorization.Subject{
        Kind: authorization.SubjectUser,
        ID:   "alice",
    },
    Action: "document.read",
    Resource: authorization.Resource{
        Type: "document",
        ID:   "secret",
    },
})
if err != nil {
    return err
}

// decision.Outcome is authorization.Deny.
// decision.Reason is acl.ReasonExplicitDeny.
// decision.MatchedPolicyIDs names both matching ACL entries.
// decision.Revision is 1.
```

## Matching and precedence

An entry matches when subject kind and ID, action, resource type, tenant scope,
and optional resource ID all match. An empty entry resource ID applies to the
whole resource type. Resource-instance entries do not implicitly outrank type
entries: any matching explicit deny overrides every matching allow.

Tenant entries only apply to the same tenant. Global entries only apply to
global requests by default. Applications that intentionally inherit global
entries into every tenant must opt in:

```go
accessList, err := acl.New(entries, acl.WithGlobalInheritance())
```

With inheritance enabled, a matching global deny also overrides a tenant
allow. This behavior is deliberately explicit because silently inheriting a
global grant can widen access across every tenant.

Group ACLs use `authorization.SubjectGroup` entries. The application supplies
trusted group IDs on `Request.Subject.Groups`; the authorization engine does
not load memberships or trust group names from unvalidated client input.

## Listing resources without a full scan

`ListResourceIDs` examines only the evaluator's subject/action/resource index.
It returns explicitly allowed instance IDs after applying denies. A type-wide
allow cannot be converted into a finite list without knowing every application
resource, so the method returns `acl.ErrUnboundedResourceSet` instead of
silently scanning or returning an incomplete result.

## Bounds

`acl.WithLimits` bounds stored entries, request groups, matched entries, and
batch size. Limit exhaustion returns an error and a deny decision where a
decision is available. The defaults are intentionally finite; applications
should lower them when their known policy sizes permit it.
