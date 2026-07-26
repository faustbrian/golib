# Propagation matrix

| Boundary | Correlation | New request | Causation |
| --- | --- | --- | --- |
| New workflow | generated | generated | empty |
| Outbound child | preserved | generated | parent request |
| Trusted HTTP receipt | preserved | generated | inbound request |
| Untrusted HTTP receipt | generated | generated | empty |
| Queue retry/redelivery | preserved | generated per attempt | message request |
| Scheduled independent run | generated | generated | empty |
| Scheduled propagated run | preserved | generated | scheduled message request |
| Webhook send/receive | preserved | generated per side | previous request |
| JSON-RPC metadata | preserved only when trusted | generated | inbound request |

Context propagation uses a private key and derived contexts. Carriers are
explicit interfaces; there is no package-global current request, goroutine
local storage, or hidden middleware installation.
