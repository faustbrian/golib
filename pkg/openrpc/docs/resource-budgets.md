# Resource budgets

All defaults are finite. A zero or negative configured limit is rejected.
Callers should lower limits when their deployment contract is smaller.

| Surface | Default budget | Enforcement |
| --- | --- | --- |
| Generic JSON | 16 MiB, depth 256, 2,000,000 tokens | `jsonvalue.Policy` rejects before ownership or model parsing |
| OpenRPC parse | 10,000 methods, parameters, servers, variables, tags, errors, links, and examples; 100,000 components | `parse.Options` rejects the affected collection |
| Draft 7 compile | 1,024 explicit resources, 64 MiB aggregate schema bytes, 1,000 issues, 100 ms regexp timeout | `jsonschema.ValidationOptions` checks resources before compiler registration |
| Semantic validation | 10,000 methods and 1,000 diagnostics | `validate.Options` checks method count before copying and stops bounded reporting |
| URI reference | 16 KiB encoded length | `reference.Policy` rejects before URI parsing |
| JSON Pointer | 16 KiB, 256 tokens, 19 index digits | `reference.PointerPolicy` rejects before token or index work |
| Resolution | depth 64, 32 documents, 16 MiB fetched, 100,000 aggregate references | One per-call `reference.ResolvePolicy` budget covers inputs, aliases, bundles, and transitive resources |
| Dereference | depth 256, 100,000 references, 64 MiB output, 4,000,000 output tokens | `reference.TransformPolicy` checks traversal and reparses bounded output |
| Runtime expression | 16 KiB source, 256 expressions, 256 segments, 19 index digits, 4,096 selected nodes, 1 MiB output | `expression.Policy` applies during parse and evaluation |
| Discovery | 64 MiB canonical output and 1,000 validation diagnostics | `discovery.Options` rejects before publishing a snapshot |
| Filter | 10,000 methods | `compose.FilterOptions` checks before predicate traversal |
| Merge | 32 documents, 10,000 methods, 100,000 components | `compose.MergeOptions` counts across every input |
| Overlay | 1,000 actions and 64 MiB output | `compose.OverlayOptions` checks after every ordered action |
| Rename | 100,000 renames and 64 MiB output | `compose.RenameOptions` checks before and after rewriting |
| Semantic diff | 10,000 methods, 100,000 components, 10,000 findings | `diff.Options` bounds comparison and report output |
| HTTP store | 3 redirects, 15 s request, 5 s dial and header waits, 1 MiB headers | `httpstore.Policy`; response bytes come from the resolver's remaining allowance |

Generation and CLI budgets are not applicable because those optional surfaces
are not present. Adding either surface requires explicit input, semantic,
output, and diagnostic limits before it can enter the release gate.

Errors are stable categories. Limit errors and diagnostics do not include
documents, schemas, fetched bodies, URLs, credentials, method names, or raw
extension values.
