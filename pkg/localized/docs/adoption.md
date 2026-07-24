# Adoption guide

## Domain values

Replace `map[string]string` fields with `localized.Text`. Construct at the
domain boundary, return `Text` by value, and expose exact lookup unless the
method name explicitly promises matching or fallback. Do not add an application
default to a domain entity.

Present-empty may be meaningful during migrations. Use the boolean from `Get`
or the fields on `match.Result`; never test only `text == ""` for presence.

## HTTP negotiation

Read the response-selection header in the HTTP client adapter, pass its value to
`http.Select`, and decide missing behavior in the application. Do not store a
request preference in `Text` or configure a process-global default.

For http-client, use `localizedhttpclient.WithPreferences` on the desired
request layer. `SelectResponse` reads the originating request from a standard
HTTP response and performs matching only; application fallback remains a
separate call.

## JSON:API, JSON-RPC, and OpenRPC

Represent localized fields as JSON objects whose member names are canonical
language tags. The specifications do not gain new localization semantics:

- JSON:API attributes contain the object directly;
- JSON-RPC params and results contain the same object;
- OpenRPC schemas describe `type: object`, string additional properties, and
  the canonical BCP 47 key constraint in prose.

Errors remain protocol errors only when the localized object is invalid at that
boundary. Selection is an application operation, not a protocol extension.
See [API integration examples](api-integrations.md) for concrete jsonapi,
jsonrpc, and OpenRPC shapes.

## Configuration

Use `localizedconfig.Text` for a presence-aware config field. `Valid=false`
means explicit null. A valid empty localized object is `Valid=true` with an
empty `Localized` value. Application required-language checks belong in a
validator after complete configuration decoding.

## Validation

Compose `localizedvalidation.Rule` values at the boundary. Core construction
always enforces UTF-8 and resource ownership; it does not impose business rules
such as required Finnish or non-empty English.

Use `localizedvalidation.Validator(rules...)` inside a `validation` plan.
It snapshots the rule slice and emits content-free codes at canonical locale
key paths such as `[en-US]`; localized strings are never report parameters or
causes.

## Testing

Use `localizedtest.New(t)` for fixtures and assertion helpers for exact or
resolution semantics. Tests should assert locale, result kind, presence, and
empty separately.

## API queries

Use `localizedquery.ExactValue` or `ExactPredicate` only after the application
has selected the locale policy explicitly. The adapter never matches or falls
back, and it does not invent a database JSON-path mapping; declare that mapping
in the owning `api-query` compiler configuration.
