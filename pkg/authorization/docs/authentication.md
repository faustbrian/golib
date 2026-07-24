# Authentication integration

Authorization consumes authenticated identity; it does not validate
credentials. The `authn` package accepts a small immutable `Principal`
interface implemented by `authentication` principals, so authentication has
no dependency on this module.

```go
subject, err := authn.Subject(principal, authn.Config{
    Kind: authorization.SubjectUser,
    AttributeClaims: map[authorization.AttributeName]string{
        "department": "department",
        "clearance":  "clearance",
    },
    GroupsClaim: "groups",
    MaxGroups:   50,
})
```

Only explicitly mapped claims become ABAC attributes. Supported claim values
are null, strings, booleans, bounded signed or unsigned integers, finite
floats, timestamps, IP addresses, and string sets. Missing claims, unsupported
nested values, overflowing integers, invalid floats, malformed groups, and
anonymous principals fail closed.

Authentication scopes and tenant hints are deliberately not converted into
permissions or authoritative tenant membership. Application code must verify
tenant membership and decide whether credential scopes have domain meaning
before constructing the authorization request.
