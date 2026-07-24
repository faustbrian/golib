# Cookies and Isolated Sessions

Cookie state is opt-in. A zero `Config` leaves `http.Client.Jar` nil, so clients
never acquire ambient package-global or process-global session state.

## Isolated default jar

```go
client, err := httpclient.New(httpclient.Config{
	Session: &httpclient.SessionConfig{},
})
```

Every such client receives a different standard-library cookie jar. The jar
uses `golang.org/x/net/publicsuffix.List`, preventing a server from setting a
cookie for a public suffix such as `com`. Internally created jars are owned by
the client.

## Redirect policy

`CookieRedirectSameOrigin` is the zero-value default. The session middleware
captures the initial logical-operation origin and strips `Cookie` after the
standard jar has populated a physical redirect request whenever its scheme,
host, or effective port changes. Same-origin redirects retain matching jar
cookies.

Some providers intentionally share a cookie across sibling origins. That
trust decision must be explicit:

```go
Session: &httpclient.SessionConfig{
	RedirectPolicy: httpclient.CookieRedirectJar,
},
```

`CookieRedirectJar` delegates redirects to the jar's domain, path, secure,
expiry, and public-suffix rules. It can therefore send a parent-domain cookie
to a sibling subdomain.

The strict redirect policy is operation middleware and applies through
`Client.Do` and `Client.DoWithMiddleware`. Calling `Client.HTTPClient().Do`
directly bypasses that middleware and uses only the jar's standard matching
rules.

Session stripping has a fixed priority before authentication. Authentication
therefore evaluates the already-sanitized attempt request, and both policies
remain inspectable through `Client.InspectPipeline`.

## Custom jars and ownership

Applications can supply any standard `http.CookieJar`:

```go
Session: &httpclient.SessionConfig{
	Jar: customJar,
},
```

Custom jars are borrowed by default. `Client.Close` does not close them. Set
`JarOwnership: CookieJarOwned` to transfer lifecycle ownership. If the owned
jar also implements `io.Closer`, it is closed exactly once with the client.
Setup failures also close an owned jar before returning.

A custom `PublicSuffixList` applies only when core constructs the jar. A custom
jar owns its own suffix and cookie policy, so configuring both is rejected.

Sharing one custom jar across clients deliberately shares session state. Use a
different jar per client, credential, tenant, or account when those boundaries
must remain isolated.

## Persistence port

Persistence is an application-provided port; core does not impose a file,
database, Redis, or serialization format:

```go
type SessionPersistence interface {
	Load(context.Context, http.CookieJar) error
	Save(context.Context, http.CookieJar) error
}
```

The client serializes calls to the port. Implementations must honor context
cancellation and sanitize stored cookie data appropriately.

Configure automatic lifecycle operations explicitly:

```go
Session: &httpclient.SessionConfig{
	Persistence:        store,
	LoadOnStart:        true,
	SaveOnClose:        true,
	PersistenceTimeout: 2 * time.Second,
},
```

Zero `PersistenceTimeout` selects five seconds. Load-on-start failure prevents
client construction and closes owned resources. Save-on-close starts after
pending client operations are canceled, uses its own finite context, and does
not prevent the owned jar from closing if persistence fails.

Applications can control timing and deadlines directly:

```go
if err := client.LoadSession(ctx); err != nil {
	return err
}
if err := client.SaveSession(ctx); err != nil {
	return err
}
```

Manual calls reject canceled contexts and stop when the client closes. They
return `ErrSessionDisabled` without a session and
`ErrSessionPersistenceUnavailable` without a configured port.

Persistence failures use `SessionPersistenceError` with a closed load/save
operation enum. Owned-jar close failures use `SessionCloseError`. Both unwrap
their causes for `errors.Is` and `errors.As` while omitting causes from rendered
messages because storage errors can contain cookie values or identifiers.
