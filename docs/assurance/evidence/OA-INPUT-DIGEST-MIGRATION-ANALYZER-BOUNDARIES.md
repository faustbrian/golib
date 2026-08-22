# Analyzer Boundary Input-Digest Migration

Observed at `2026-08-21T15:14:09Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The analyzer remediation made integer bounds explicit in the ECMA-262 parser,
HTTP retry parsing, Valkey management records, and PostgreSQL sequencer ledger
conversion. It also removed a redundant token-bucket assignment and relaxed a
test-only Redis restart connection budget to the production default. The
changes preserve the public results already exercised by the retained HTTP and
external-reference scenarios.

Operational-assurance fingerprints include direct and transitive verification
inputs. The HTTP client, retry package, queue packages, and their dependants
therefore changed identity even where their own source did not change. The
authorized one-way transitions are:

| Module | Previous verified digest | Current digest |
|---|---|---|
| `pkg/cloudevents/adapters/golib` | `ee1d0884ecc7f783e99af0c2a09dcf12499d361b995610423ee38edb350b19c3` | `c0fe0dbb5b95267e9af4d374101522fcbe80d8f8d75329cac80d8e7e498779e9` |
| `pkg/http-client` | `8511ce4ea8f40627e708ac597c58c24a6226c034afac327869305619cb5ce472` | `4fd30885a53d367e76bc0581f43f0f99f9a05b626388d9796a36bbab666851d3` |
| `pkg/lease` | `b6d31a29b62f079c80363f1533ef6d24c5c59c5d4da389079e75cf19fa4ac8bb` | `a232026e5b95c995db26fbd6fa7c576a190474c08efd2f2551cc957e1ca14741` |
| `pkg/localized` | `bedff297b382b2ba91a23040706c8f8bfa6206b0c65fed2a50774a3f085162bf` | `8055919b6ec127a6bf1a379fc98e09928f4e73916129815adbd68da43af823f5` |
| `pkg/queue-control-plane` | `7b6dccf1b89cbdf3f8023dcf9f81183847f698904a1f2cf36d4bfa46d1ecd554` | `c50fd66824a7a8700e566797e72fcd7d46616b157672926705d041e789e953e8` |
| `pkg/queue/queueservice` | `86bc5f4886e9fbc8b029e2a38d3d8cb1ec15406b7b0a30a7cbdac1f0f66804e1` | `2baa42223eb6152cf06d85862bf7a9f9f3c43b1bd1e012a2907c14ffe39f3b0f` |
| `pkg/retry` | `c3e5756cef8873f14c0840c916c5b4326553af122ccd89fd7438c209e72ea687` | `ac9b793b5eda5dc5590788a61d421dcf09fd2f30b4be550e9022a966788df900` |
| `pkg/scheduler` | `aa7a96ea2d5ca54bc07b100b8b5f07c5c357db57f86e47d9303b84d0158e0505` | `71618a4e61f716b230d38afc9e1f7ce9c540ee8d5470819e3e9010d693437118` |
| `pkg/webhook` | `e698e7c55f90cf50f339e0d0e86e9a5f5106bab4054772e6fb50c8b099195690` | `9de763d19b2acb700ec7141fa667559ee11ce499fc773a0c0f9fca89a985831e` |

## Behavioral Proof

The directly changed ECMA-262, HTTP client, queue, retry, and sequencer modules
passed their complete strict local contracts, including exact statement
coverage and exact viable-mutant effectiveness. The authoritative CodeQL scan
for the resulting commit completed without an open alert. The retained
reference scenarios do not exercise the changed Valkey management pagination,
sequencer PostgreSQL conversion, ECMA-262 parsing, or Redis restart test setup.
The HTTP client and retry changes preserve their already tested reservation and
`Retry-After` outcomes.

## Claim Boundary

This record authorizes only the exact transitions above. It does not change an
operational scenario's observation time, environment, scope, accepted risks,
or release verdict, and it does not claim that an operational scenario was
rerun.
