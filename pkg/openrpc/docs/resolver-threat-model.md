# Resolver threat model

The core parser, validator, reference parser, and pointer evaluator perform no
filesystem or network access. External resolution exists only behind a
caller-supplied `reference.Store`, `AllowExternal`, and exact scheme and host
allowlists.

| Threat | Control | Executable evidence |
| --- | --- | --- |
| SSRF and unsafe schemes | External access defaults off; schemes and hosts require exact allowlisting | `TestResolverDisablesExternalAccessByDefault`, `TestResolverEnforcesExternalSchemeHostBytesAndCancellation` |
| Local file disclosure | No ambient file loader; `FSStore` is scoped to an explicit `fs.FS` and rejects traversal, queries, fragments, credentials, and encoded paths | `TestFSStoreMapsOnlyURIsBelowExplicitBase` |
| Credential-bearing URLs | HTTP authorization rejects `url.User`; safe errors omit the URL | `TestStoreDeniesHostsSchemesCredentialsAndPrivateAddresses` |
| Redirect escape | Every redirect is reauthorized and redirect count is bounded | `TestStoreRejectsLimitsCompressionStatusAndRedirects` |
| DNS rebinding and private addresses | Every resolved address is checked; dialing uses only the checked IP while TLS retains the request host | `TestStoreDeniesHostsSchemesCredentialsAndPrivateAddresses` |
| Decompression bombs | Automatic compression is disabled, non-identity encodings are rejected, and bodies use a streamed remaining-byte limit | `TestStoreRejectsLimitsCompressionStatusAndRedirects`, `TestStoreHandlesStreamingLimitsReadFailuresAndRequestCancellation` |
| Cycles and aliases | Per-chain target identities detect cycles; per-call documents are immutable and deduplicated | `TestResolverRejectsCyclesAndBounds`, `TestResolverFollowsAliasesAndLoadsEachDocumentOnce` |
| Depth and fan-out exhaustion | Depth, document, fetched-byte, pointer, JSON, and aggregate-reference limits are mandatory | `TestResolverBoundsAggregateReferenceFanout`, `TestResourcesBoundsTransitiveReferenceFanout`, `TestBundleBoundsRootReferenceFanoutBeforeLoading` |
| Cancellation races | Context checks surround queue, load, resolve, cache wait, and transform paths; race and leak gates are blocking | `make race`, `make leak` |
| Sensitive diagnostics | Store failures collapse to stable categories; observer events exclude payloads, references, URLs, and error text | `TestResolverCoversInputErrorsCancellationAndLoadFailures`, `TestOperationsContainObserversAndClassifyFailures` |

`AllowHTTP` and `AllowPrivateAddresses` are explicit trust-boundary overrides.
They are intended only for controlled environments. The caller remains
responsible for the ownership and behavior of custom stores and allowlisted
hosts.
