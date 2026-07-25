# Policy Scopes

Shared policy state needs a stable identity boundary that is independent of
URLs and raw telemetry labels. `PolicyScope` snapshots endpoint, credential,
tenant, account, and caller-defined values on a request context:

```go
scope, err := httpclient.NewPolicyScope(httpclient.PolicyScopeOptions{
	Endpoint:   "widgets.list",
	Credential: credentialIdentity,
	Tenant:     tenantID,
	Account:    accountID,
	Custom:     map[string]string{"region": regionID},
})
if err != nil {
	return err
}

ctx, err = httpclient.WithPolicyScope(ctx, scope)
request = request.WithContext(ctx)
```

Origin and host always come from the concrete request URL. Scope values reject
control characters and excessive lengths. Custom names use a bounded ASCII
identifier. Input maps are copied and cannot mutate a previously constructed
scope.

`ResolvePolicyScope` returns a versioned SHA-256 `PolicyScopeKey`. Its string
contains no raw origin, host, endpoint, credential, tenant, account, custom
value, path, query, cookie, or authorization material. `Dimensions` exposes
only the ordered dimension names as provenance.

## Resource defaults

Empty dimensions select resource-specific defaults:

| Resource | Default dimensions |
| --- | --- |
| Transport | origin |
| Cookies | origin, credential, tenant, account |
| OAuth tokens | origin, credential, tenant, account |
| Cache and coalescing | origin, credential, tenant, account |
| Rate limiter | origin, credential, tenant, account |
| Circuit breaker | origin, endpoint, credential, tenant, account |
| Metrics | origin, endpoint |

Metrics deliberately exclude credential, tenant, account, raw path, and query
dimensions by default. Applications can resolve an explicit dimension list,
including values created by `CustomScopeDimension`, when a resource needs a
different boundary.

The HTTP cache includes the default cache scope in both persistent keys and
in-flight coalescing keys. Existing Authorization, Proxy-Authorization, and
Cookie headers are hashed into the credential dimension when an explicit
credential scope is absent.

Authentication middleware may add credentials only at the physical-attempt
stage, after an operation cache has selected its key. A client that combines
such authentication with caching must attach a stable credential, tenant, or
account scope to the original request context. This makes the identity
boundary explicit without storing or rendering the credential itself.
