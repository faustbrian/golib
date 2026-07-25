# Authentication Cookbook

Authentication is expressed as immutable `RequestEditor` values around normal
`*http.Request` objects. Editors are safe for reuse and do not introduce a
global credential registry.

Use `NewAuthenticationMiddleware` for credentials. It creates one operation
middleware that fixes the trusted origins and one attempt middleware that
decorates every physical request. The default trusted set contains only the
logical operation's initial origin. Same-origin redirects are decorated again;
cross-origin redirects are not. Trusted credential origins must use HTTPS.
Local tests that use `httptest.NewServer` may set `AllowInsecure: true`; do not
enable that option for production credentials.

## Basic authentication

```go
editor, err := httpclient.NewBasicAuth(username, password)
```

The user name must be non-empty and cannot contain the Basic-auth delimiter
`:`, and both values must be valid UTF-8. The editor replaces any existing
`Authorization` values with one standard Basic value.

## Bearer authentication

```go
editor, err := httpclient.NewBearerAuth(accessToken)
```

Bearer tokens are validated against the RFC 6750 `b64token` syntax before they
are retained. Empty tokens, whitespace, control bytes, and misplaced padding
are rejected without including the token in the error.

## OAuth2 token sources

Existing `golang.org/x/oauth2.TokenSource` implementations can be adapted
directly:

```go
editor, err := httpclient.NewOAuth2Auth(tokenSource)
```

The source is wrapped with `oauth2.ReuseTokenSource`, which caches valid tokens
and serializes refresh calls. The editor validates the returned token and
authorization header before mutating the attempt request. `OAuth2TokenError`
unwraps source and validation failures without rendering them.

The standard `oauth2.TokenSource` interface does not accept a context, so an
arbitrary implementation cannot promise that an in-progress refresh stops on
request cancellation. Context-aware sources can use:

```go
editor, err := httpclient.NewContextOAuth2Auth(contextTokenSource)
```

`ContextTokenSource.Token` receives the physical request context and must honor
cancellation while obtaining a token.

## OAuth2 client credentials

`NewClientCredentialsTokenSource` implements a context-aware outbound
client-credentials flow through `golang.org/x/oauth2/clientcredentials`:

```go
source, err := httpclient.NewClientCredentialsTokenSource(
	httpclient.ClientCredentialsOptions{
		Client:       client,
		TokenURL:     "https://identity.example.com/oauth/token",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"widgets:read"},
	},
)
if err != nil {
	return err
}

editor, err := httpclient.NewContextOAuth2Auth(source)
```

The token source has these lifecycle rules:

- one caller refreshes while concurrent callers wait on bounded shared state;
- every waiting caller can stop independently through its context;
- closing `Client` stops refreshes and rejects cached tokens;
- every caller receives an independent token copy;
- token endpoint calls reuse the client's transport and finite timeout;
- integration middleware and cookie jars are bypassed, preventing recursive
  authentication, nested retry loops, and ambient session state; and
- the default client authentication style is HTTP Basic in the header, avoiding
  the auto-detection request pair in `golang.org/x/oauth2`.

Token URLs must use HTTPS, cannot contain user information, queries, or
fragments, and are copied with scopes and endpoint parameters. Local tests may
opt in to HTTP with `AllowInsecureURL`. Use `AuthStyleInParams` only when the
provider requires credentials in the request body. Reserved OAuth2 parameters
cannot be overridden through `EndpointParams`.

`EarlyExpiry` controls how far before expiry a cached token becomes unusable;
zero selects ten seconds. `Now` is an optional concurrency-safe clock seam for
deterministic tests. Client-credentials failures use `ClientCredentialsError`,
which preserves its cause but never renders the endpoint response or secret.

## API keys

Prefer a header when the provider supports it:

```go
editor, err := httpclient.NewAPIKeyHeader("X-API-Key", apiKey)
```

The editor replaces rather than appends values. Include custom credential
headers in `SensitiveHeaders` so caller-provided values are also stripped when
a redirect crosses the trust boundary:

```go
authentication, err := httpclient.NewAuthenticationMiddleware(
	httpclient.AuthenticationOptions{
		Name:             "vendor-auth",
		Layer:            httpclient.MiddlewareClient,
		SensitiveHeaders: []string{"X-API-Key"},
	},
	editor,
)
```

Some providers require a query credential. Its URL placement is deliberately
explicit:

```go
editor, err := httpclient.NewAPIKeyQuery("api_key", apiKey)
```

Query credentials can be exposed through server logs, browser history,
proxies, and third-party instrumentation. `TransportError` removes complete
query strings, but that cannot protect external systems. Prefer a header.

## HMAC signing

HMAC protocols disagree about canonical paths, query normalization, signed
headers, body digests, signature encodings, and authorization syntax. Core
therefore owns only the cryptographic HMAC calculation; the vendor package owns
canonicalization and signature placement:

```go
editor, err := httpclient.NewHMACAuth(httpclient.HMACOptions{
	Secret:  secret,
	NewHash: sha256.New,
	Canonicalize: func(request *http.Request) ([]byte, error) {
		return vendorCanonicalRequest(request)
	},
	ApplySignature: func(
		request *http.Request,
		signature []byte,
	) error {
		request.Header.Set("X-Signature", hex.EncodeToString(signature))

		return nil
	},
})
```

The secret byte slice is copied at construction. `HMACError` identifies
canonicalization, calculation, or application and unwraps the cause without
rendering it. Canonicalizers must not consume a non-replayable body. Arrange
request-mutating middleware before HMAC signing with explicit priorities.

## Generated clients and custom editors

`RequestEditorFunc` matches the common generated-client request-editor shape:

```go
middleware, err := httpclient.NewRequestEditorMiddleware(
	httpclient.MiddlewareOptions{
		Name:     "generated-editor",
		Scope:    httpclient.ScopeAttempt,
		Layer:    httpclient.MiddlewareEndpoint,
		Priority: 10,
	},
	httpclient.RequestEditorFunc(generatedEditor),
)
```

This generic adapter does not add an origin boundary. Use
`NewAuthenticationMiddleware` whenever the editor places credentials.

## Explicit trusted origins

`AllowedOrigins` replaces the same-origin default with an exact set of HTTPS
origins. Values cannot contain user information, paths, query strings,
fragments, or malformed ports. Default ports are normalized. Supplying
multiple origins explicitly allows the same credential across those trust
boundaries:

```go
AllowedOrigins: []string{
	"https://api.example.com",
	"https://uploads.example.com",
},
```

`Authorization`, `Proxy-Authorization`, and `Cookie` are always stripped on an
untrusted redirect. Additional provider-specific credential headers must be
listed in `SensitiveHeaders`.

## Errors and secret handling

Configuration failures match `ErrInvalidAuthentication`. Runtime editor
failures use `RequestEditorError`, and HMAC callback failures use `HMACError`.
Both retain causes for `errors.Is` and `errors.As`, but rendered messages omit
the cause because it may contain a token, secret, canonical request, or query.
