# Adoption Guides

## REST-like APIs

Use explicit method sets and `{id}` path wildcards. Group stable API versions,
attach generic middleware by concern, publish names only for URLs that are a
SemVer contract, and inspect `Routes` during startup documentation checks.

## JSON-RPC

Create the dispatcher and HTTP adapter in `jsonrpc`, then mount that
`http.Handler` at `/rpc` with `StripPrefix` only when the adapter expects `/`.
JSON-RPC method names remain inside the dispatcher and never become HTTP routes.

## Webhooks

Mount each provider handler at an explicit boundary or register one POST route.
Attach provider authentication as route middleware. Do not infer authentication
from handler type or metadata.

## Health, metrics, and debug

Mount ordinary handlers at explicit paths and hosts. Keep operational endpoints
on a dedicated host or group when policy differs. A mount does not inherit any
authorization beyond middleware visible in its descriptor.

## Mixed services

REST routes, an RPC mount, webhooks, and probes can share one builder. Compile
once, inspect the flattened table, then give the immutable handler to
`service`. Track, Postal, and Location retain ownership of handlers and
middleware lifecycle.
