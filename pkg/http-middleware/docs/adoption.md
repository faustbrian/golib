# Adoption guides

## REST-like APIs

Use recovery, trusted proxy policy, request IDs, observation, CORS, API security
headers, admission, body limits, content guards, owning authentication and
authorization, then compression. Protocol-specific JSON error bodies require
an injected application handler; generic short circuits remain plain text.

## JSON-RPC

Keep JSON-RPC parsing, batch semantics, error objects, method dispatch, and
idempotency in their owning packages. Apply generic connection, body, timeout,
proxy, and observation policies outside the JSON-RPC handler. Avoid generic
406/415 responses if the transport requires a JSON-RPC error envelope.

## Webhooks

Limit the encoded body before signature verification, preserve the raw body
format required by the signer, disable response compression unless useful, and
delegate replay/idempotency state to `idempotency`. CORS is normally absent
for server-to-server webhooks.

## Administrative APIs

Use `responsepolicy.NoStore`, strict origin allowlists, short body and response
limits, local admission, and explicit authentication/authorization. Never put
tenant, actor, record, query, or raw path values into default observation labels.

## Health and Kubernetes endpoints

`service` owns health and readiness handlers. A transport-only state source
may gate other endpoints during maintenance. Keep probe chains small: recovery,
bounded observation, security headers, and no-store are usually sufficient.

## Track, Postal, and Location

Build the shared transport prefix once, inspect it before serving, then append
service-owned representation and access policy explicitly. Do not recreate
Laravel middleware aliases, kernel groups, containers, sessions, or templates.
