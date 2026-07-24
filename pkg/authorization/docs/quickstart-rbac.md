# Five-minute RBAC quickstart

RBAC assigns subjects to roles and attaches typed permissions to those roles.
Roles, permissions, and assignments carry explicit tenant scopes; role IDs are
globally unique within an evaluator.

```go
roles := []rbac.Role{
    {ID: "reader", Tenant: "acme"},
    {
        ID:      "editor",
        Tenant:  "acme",
        Parents: []rbac.RoleID{"reader"},
    },
}

permissions := []rbac.Permission{
    {
        ID:           "read-documents",
        RoleID:       "reader",
        Tenant:       "acme",
        Action:       "document.read",
        ResourceType: "document",
        Effect:       authorization.Allow,
    },
    {
        ID:           "update-documents",
        RoleID:       "editor",
        Tenant:       "acme",
        Action:       "document.update",
        ResourceType: "document",
        Effect:       authorization.Allow,
    },
}

assignments := []rbac.Assignment{
    {
        ID: "alice-editor",
        Subject: authorization.Subject{
            Kind: authorization.SubjectUser,
            ID:   "alice",
        },
        RoleID: "editor",
        Tenant: "acme",
    },
}

rolesPolicy, err := rbac.New(roles, permissions, assignments)
if err != nil {
    return err
}

decision, err := rolesPolicy.Evaluate(ctx, authorization.Request{
    Subject: authorization.Subject{
        Kind: authorization.SubjectUser,
        ID:   "alice",
    },
    Action: "document.read",
    Resource: authorization.Resource{
        Type: "document",
        ID:   "quarterly-report",
    },
    Tenant: "acme",
})
if err != nil {
    return err
}

// decision.Outcome is authorization.Allow through the inherited reader role.
```

The RBAC evaluator implements `authorization.Evaluator`, so it can be installed
in a revisioned root snapshot exactly like the ACL evaluator.

## Precedence and priority

Every matching permission is considered. A matching explicit deny overrides
all matching allows, regardless of role or priority. `Permission.Priority`
orders effective permissions and matched policy IDs from highest to lowest;
ties use the stable permission ID. Priority does not weaken deny-overrides.

A permission with no resource ID applies to its whole resource type. A
permission with a resource ID only applies to that instance.

## Tenant and global roles

Assignments, roles, and permissions must have the same tenant. Parent roles
must also share the child's tenant, which prevents inheritance edges from
crossing domains.

Global assignments and permissions do not apply to tenant requests by default.
Applications must explicitly opt into that inheritance:

```go
rolesPolicy, err := rbac.New(
    roles,
    permissions,
    assignments,
    rbac.WithGlobalInheritance(),
)
```

With inheritance enabled, global denies also apply inside tenants. Group role
assignments use `authorization.SubjectGroup`; trusted group IDs are supplied by
the application on `Request.Subject.Groups`.

## Inheritance and effective permissions

`Role.Parents` declares inherited roles. Construction rejects missing parents,
cross-tenant edges, cycles, and inheritance deeper than the configured limit.
Diamonds are deduplicated during traversal.

Use `EffectivePermissions` to inspect the ordered permissions a subject gains
from direct roles, group roles, and inherited roles without making a resource
decision.

## Assignment administration

`rbac.Manager` is a synchronized in-memory administrative helper. `Assign`
validates the complete candidate view before committing it,
`RevokeAssignment` removes a stable assignment ID, `Assignments` inspects exact
subject/tenant assignments, and `Evaluator` returns an immutable evaluation
view. The manager revision increases only after a successful mutation.

The manager is not a persistence adapter. Applications that need durable or
distributed policy updates should use the repository and snapshot adapters
once those packages are configured.

## Bounds

`rbac.WithLimits` bounds roles, permissions, assignments, request groups,
matched permissions, batch size, and inheritance depth. Construction or
evaluation fails closed when a limit is exceeded.
