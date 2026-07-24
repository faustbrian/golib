# Model selection

Choose the simplest authority that owns the rule. Combining models is useful,
but it also increases review and explanation cost.

```text
Is this invariant application behavior rather than policy?
├─ yes -> keep it in application code and call authorization at the boundary
└─ no
   Is access an explicit subject/group-to-resource grant or deny?
   ├─ yes -> ACL
   └─ no
      Is access derived from reusable job functions or tenant roles?
      ├─ yes -> RBAC
      └─ no
         Does the decision compare trusted subject, resource, request,
         environment, time, network, or ownership attributes?
         ├─ yes -> ABAC
         └─ no -> clarify the invariant before adding a policy model
```

Use application code for workflow state transitions, data integrity, and
business operations that are invalid for everyone. For example, “a settled
invoice cannot be edited” is normally a domain invariant. “Finance operators
may settle an open invoice” is authorization.

Use ACL for document sharing, explicit exceptions, resource ownership grants,
and revocations. ACL is strongest when the grant itself is authoritative and
enumerable.

Use RBAC for tenant administrators, support operators, service job functions,
and stable permission bundles. Prefer shallow inheritance and tenant-local
roles. Do not create one role per user as a substitute for ACL.

Use ABAC when policy depends on typed facts such as owner ID, region, risk,
time, source network, or resource classification. Only map attributes from
trusted sources. ABAC should not fetch data during evaluation.

## Combining models

The common production shape is deny-overrides across a small number of model
evaluators: an explicit suspension deny, tenant RBAC grants, resource ACLs, and
targeted ABAC constraints. Use priority-order when one policy tier must be
authoritative by design. Treat allow-overrides as exceptional because it can
defeat an explicit deny.

If reviewers cannot predict precedence from the snapshot and its combining
algorithm, split the authorization boundary or simplify the policies.
