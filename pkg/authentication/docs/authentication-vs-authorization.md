# Authentication versus authorization

Authentication answers “who presented this credential, and how was that
identity established?” Authorization answers “may this principal perform this
action on this resource?” They change for different reasons and belong in
different packages.

`authentication` owns credential syntax, verification, issuer trust,
principal construction, and authentication failures. It does not own users,
roles, permissions, ACLs, RBAC, ABAC, ownership, or policy evaluation.
`authorization` consumes the immutable principal and domain resource.
`service` may compose both middleware layers but must not reimplement
credential semantics.

Conceptually:

```go
principal, ok := authentication.PrincipalFromContext(r.Context())
if !ok {
	return errors.New("authentication middleware invariant failed")
}
if err := authorizer.Authorize(r.Context(), principal, "orders.read", order); err != nil {
	return err
}
```

Do not treat a scope or tenant hint as a completed authorization decision.
Those values are assertions copied from the credential. The authorizer must
decide whether the issuer is trusted for that assertion, whether the operation
recognizes it, and whether resource-specific rules also pass.

Anonymous routes receive an explicit anonymous principal. Authorization then
decides which public operations, if any, are permitted.
