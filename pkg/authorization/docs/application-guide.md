# Application patterns

Map authenticated, trusted application state into one typed `Request`, call an
`Authorizer`, and continue only when the returned outcome is `Allow` and the
error is nil. Never infer permission from an empty error alone.

## Tenant roles

Define tenant-local RBAC roles such as `viewer`, `operator`, and `admin`.
Assignments, roles, and permissions must carry the same tenant. Global role
inheritance is disabled unless explicitly enabled.

Keep administrative authority separate from ordinary resource permission. A
tenant admin may assign approved tenant roles without being allowed to publish
global policy manifests.

## Resource ACLs

Use ACL entries for explicit document shares and exceptions. Include a concrete
resource ID when the grant applies to one object. A type-wide entry intentionally
applies to every resource of that type and cannot be converted into a complete
resource listing.

For list endpoints, use `acl.ListResourceIDs` only for bounded explicit grants,
then apply those IDs to the application query. Do not load every database row
and authorize it one at a time.

## Trusted attributes and ownership

ABAC attributes must come from an authoritative mapper. Subject claims come
from verified authentication; resource attributes come from the loaded domain
object; environment attributes come from controlled request metadata.

For dynamic ownership, let the application compare the authenticated subject
with the loaded resource, then map only the trusted boolean result:

```go
owner := abac.Equal(
    abac.Reference{Source: abac.Resource, Name: "owned_by_subject"},
    authorization.BoolValue(true),
)
```

Do not accept `owner_id`, tenant, role, or service identity directly from an
untrusted JSON body or query string. Set `owned_by_subject` only after comparing
the authenticated internal subject ID with the authoritative resource owner.
When ownership is also a domain invariant, load and validate the resource before
constructing the request.

## Explicit deny

Use explicit deny for suspension, legal hold, high-risk operations, and narrow
exceptions that must defeat grants. Compose these policies with
`DenyOverrides`. Keep the deny reason structured and non-sensitive; detailed
investigation data belongs in separately protected application logs.

## API keys and services

Represent API keys as `SubjectAPIKey` and workloads as
`SubjectServiceAccount`. Use stable internal IDs, not secret key material or a
raw bearer token. Put scopes, issuer, workload, or environment into typed
attributes only after authentication verifies them.

Service accounts should receive narrow RBAC permissions or ACL grants. Avoid a
shared “internal service” bypass. Machine identities need tenant isolation and
explicit deny semantics just like users.

## Batch decisions

Use `Engine.DecideBatch` or a model's `EvaluateBatch` when one operation needs a
bounded set of independent decisions. A batch observes one snapshot revision,
preserves input order, and returns per-request decisions. It joins evaluation
errors; inspect each decision and treat every errored item as denied.

Keep the configured batch limit below the transport response limit and the
application's cancellation budget. Batch authorization is not a substitute for
an indexed database predicate when filtering an unbounded collection.

## Transport boundary

HTTP and JSON-RPC adapters require an explicit request mapper. The mapper should
parse route or method identity, load required trusted resource fields, and map
the authenticated principal. Denial and internal-error handlers are separate;
neither calls the protected handler.

Store the returned decision in request context only for downstream explanation
or audit correlation. Downstream code should not silently re-authorize with a
different mapper or snapshot unless it is protecting a distinct operation.

## Policy administration

Validate and compile a candidate manifest, diff it, dry-run representative
requests, then persist it with the expected current revision. Publish the new
revision hint only after the source-of-truth write succeeds. Restrict this path
more tightly than ordinary application authorization.
