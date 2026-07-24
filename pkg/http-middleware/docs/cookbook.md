# Cookbook

## Conditional no-store

```go
conditional, err := middleware.When(
    func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/admin/") },
    responsepolicy.NoStore(),
)
if err != nil {
    return err
}
```

The predicate is caller-owned and may inspect route metadata instead of a raw
path. A panic propagates to the surrounding recovery layer.

## Trusted proxy

```go
forwarded, err := proxy.New(proxy.Policy{
    Trusted: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
    Mode: proxy.Forwarded,
})
if err != nil {
    return err
}
```

Configure the ingress to replace untrusted `Forwarded` input. The request's
`RemoteAddr`, URL, Host, and TLS state are never rewritten.

## Credentialed CORS

```go
crossOrigin, err := cors.New(cors.Policy{
    AllowedOrigins:   []string{"https://console.example"},
    AllowedMethods:   []string{http.MethodPost},
    AllowedHeaders:   []string{"Content-Type", "Authorization"},
    AllowCredentials: true,
})
if err != nil {
    return err
}
```

Wildcard fields are rejected in this credentialed policy.

## Maintenance admission

Inject an atomic or otherwise concurrency-safe state source into
`responsepolicy.Admission`. The middleware owns only the 503 transport response;
`service` still owns health and readiness handlers.
