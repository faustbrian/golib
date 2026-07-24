# Ordering reference

For `[A, B, C]`, request execution is `A -> B -> C -> handler`; response unwind
is `handler -> C -> B -> A`. A short circuit prevents every inner layer from
running. An outer observation layer sees inner short circuits.

| Layer | Request effect | Response effect | Required relationship |
|---|---|---|---|
| recovery | installs panic boundary | emits safe error before commit | outermost |
| trusted proxy | derives effective peer data | none | before client policy |
| request ID | validates or creates ID | returns ID header | before observation |
| observation | starts bounded event | records all inner outcomes | after proxy and ID |
| CORS | validates origin/preflight | applies CORS fields | outside application |
| security headers | installs fixed policy | reasserts fields at commit | outside application |
| admission | acquires bounded permit | releases permit | before expensive work |
| body limit | wraps encoded body | reports unread overflow | before decoders |
| deadline | derives shorter context | cancels timer/context | before application |
| authentication | owning package | owning package | before authorization |
| rate limit | owning package | owning package | before expensive policy |
| authorization | owning package | owning package | after authentication |
| idempotency | owning package | owning package | before representation |
| compression | negotiates coding | transforms representation | around application |
| application/router | handles request | creates representation | innermost |

Named descriptors make security-sensitive constraints inspectable. Duplicate
names fail unless every duplicate explicitly permits repetition. `Before`
must precede the first occurrence of a duplicated target, and `After` must
follow its last occurrence. Use
`adapter.ValidateGoService` before combining a chain with `service` defaults.

The blocking composition suite executes this complete stack, every possible
short-circuit position, nested re-entry, cancellation, duplicates, and the
empty, nil, single, and maximum-depth boundaries. It asserts the exact request
and reverse response sequence rather than only checking that layers ran.
