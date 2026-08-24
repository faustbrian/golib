# JSON-RPC Typed-Dispatch Input-Digest Migration

Observed at `2026-08-24T10:56:34Z` on `darwin/arm64` with Go `1.26.6`.

## Change

`pkg/jsonrpc` adds `Dispatcher.DispatchSingle`, an additive typed-response
entry point for compatibility adapters. The existing `Dispatcher.Dispatch`
keeps its size, UTF-8, nesting, JSON, object-shape, notification, hook, and
error behavior. It now delegates validated single requests to the same
internal dispatch function used by the new entry point and then performs the
same response encoding as before.

The maintained `pkg/service/integration/reference-http` composition continues
to construct `jsonrpc.HTTPHandler`, whose request path calls
`Dispatcher.Dispatch`; it does not call the new typed entry point. The
authorization RPC adapter likewise consumes JSON-RPC request and error types
without calling `DispatchSingle`. The behavior exercised by the retained
`OA-REFERENCE-HTTP` observation is therefore unchanged.

## Authorized Transitions

| Module | Previous digest | Replacement digest |
| --- | --- | --- |
| `pkg/authorization` | `7ad588e59d1e7da4873c73d8c34a455825fc802a22b9d78720557877bb883765` | `7509c4782dda8291e10c9ec91b8547d58804029d7a25575371368241b0239c4e` |
| `pkg/jsonrpc` | `9b9c50ad2a57fd619eabfa11bba6ced6964f243f6c130fdba3568cd2dc0bea4f` | `3e1e9d5d868353d91b386b1fb4478114229c5a145af5cef8493de81299ee7b1d` |

## Claim Boundary

This record authorizes only the exact one-way transitions above. It preserves
the earlier HTTP reference observation without relabeling its execution time,
broadening its claim, or authorizing any future input digest. It does not use
the retained observation as proof of the new `DispatchSingle` API.
