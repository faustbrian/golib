# JSON-RPC specification decisions

This register records observable choices where JSON-RPC 2.0, its examples, or
its HTTP use leave more than one credible implementation. The
[JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification) is the
normative source. [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259) governs
the underlying JSON syntax. Defensive choices are identified as policy rather
than presented as JSON-RPC requirements.

Statuses are `resolved`, `unresolved`, or `superseded`. A resolved decision is
part of the compatibility contract. Changes require protocol review,
executable evidence, and a changelog entry.

## JSONRPC-DEC-001: Invalid notification-shaped objects

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Request Object](https://www.jsonrpc.org/specification#request_object), [Notification](https://www.jsonrpc.org/specification#notification), and [Batch](https://www.jsonrpc.org/specification#batch) |
| Classification | Ambiguity between the no-response notification rule and error responses for invalid batch members |
| Issue | An object without `id` resembles a notification, but it may fail the requirements for a Request Object. Calling every such object a notification suppresses validation errors; calling it invalid produces a response with `id: null`. |
| Credible interpretations | Suppress every response whenever `id` is absent; or classify notification status only after the object is a valid Request. The specification's mixed-batch example demonstrates the latter for an invalid object. |
| Known peer behavior | [`creachadair/jrpc2`](https://github.com/creachadair/jrpc2#batch-requests-and-error-reporting) also reports validation errors for id-less invalid batch members and documents the notification conflict. |
| Selected behavior | Validate the Request Object first. A valid request without `id` is a notification and produces no response. An invalid object without `id` produces `Invalid Request` with `id: null`, including as a batch member. This is a normative interpretation of Request-before-Notification semantics. |
| Consequences | Invalid notification-shaped input is observable to two-way transports. Valid notifications remain unobservable. The policy prevents malformed input from silently bypassing protocol diagnostics. |
| Executable evidence | `TestDispatcherProtocolErrors`, `TestDispatcherBatch`, `TestDispatcherBatchEdgeCases`, and the mixed-batch official fixture exercised by `TestSpecificationExamples` |
| Public surface | `Dispatcher.Dispatch`, `HTTPHandler.ServeHTTP` |
| Upstream record | No normative erratum is known; the specification's batch example is retained as interoperability evidence. |
| Reconsider when | A JSON-RPC erratum explicitly defines whether an invalid id-less object is a Notification. |

## JSONRPC-DEC-002: Explicit null request IDs

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Request Object, note 1](https://www.jsonrpc.org/specification#request_object) and [Notification](https://www.jsonrpc.org/specification#notification) |
| Classification | Recommended behavior and interoperability policy |
| Issue | The grammar permits `id: null`, while the specification discourages it and notes that `null` is also used when an error cannot identify a request. |
| Credible interpretations | Treat explicit null as equivalent to an absent ID; reject it; or preserve the distinction while discouraging generation. |
| Known peer behavior | No maintained peer result is pinned yet; the official note and local wire tests are the current evidence. |
| Selected behavior | Explicit `id: null` is a request, not a notification. It receives a response with `id: null`. The library accepts and preserves it but does not generate it by default. |
| Consequences | Wire compatibility follows the permitted grammar, but callers cannot use a null ID for unique correlation and should avoid it. |
| Executable evidence | `TestRequestDistinguishesNotificationFromNullID`, `TestIDRoundTripAndEquality` |
| Public surface | `ID`, `NullID`, `Request.IsNotification`, dispatcher response shaping |
| Upstream record | No separate erratum is known. |
| Reconsider when | The base specification removes null IDs or defines them as notifications. |

## JSONRPC-DEC-003: Numeric ID precision and equivalence

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Request Object, note 2](https://www.jsonrpc.org/specification#request_object); RFC 8259 [Numbers](https://www.rfc-editor.org/rfc/rfc8259#section-6) |
| Classification | Omission and defensive interoperability policy |
| Issue | JSON-RPC permits Number IDs but does not define precision, lexical equality, exponent handling, or a maximum representation. |
| Credible interpretations | Compare lexical JSON bytes; decode through binary floating point; or compare exact mathematical values while preserving the original wire token. |
| Known peer behavior | No maintained peer comparison currently proves exact large-number and exponent correlation. This remains an interoperability evidence gap, not an unresolved local behavior. |
| Selected behavior | Preserve the original valid JSON number for echoing and compare numeric IDs by exact decimal value without binary floating-point conversion. Equivalent forms such as `1`, `1.0`, and `1e0` correlate. Canonicalization is bounded and does not expand large exponents. |
| Consequences | Large integers and exponent forms do not lose precision. Lexically different but mathematically equal Number IDs cannot identify distinct outstanding requests. String IDs remain distinct from Number IDs. |
| Executable evidence | `TestIDRoundTripAndEquality`, `TestIDNumberCanonicalizationIsBounded`, `TestIDCanonicalizationAllocationsDoNotScaleWithExponentDigits`, `TestClientMatchesEquivalentNumericID` |
| Public surface | `ID.UnmarshalJSON`, `ID.MarshalJSON`, `ID.Equal`, client correlation |
| Upstream record | No precision profile is defined by JSON-RPC 2.0. |
| Reconsider when | A successor specification defines lexical ID equality or numeric bounds. |

## JSONRPC-DEC-004: Empty batches

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Batch](https://www.jsonrpc.org/specification#batch) |
| Classification | Normative requirement |
| Issue | An empty Array is syntactically valid JSON but not a valid batch and therefore must not be classified as a parse error or an empty response Array. |
| Credible interpretations | Return one `Invalid Request` object; return an array containing that object; or reject it at the transport layer. |
| Known peer behavior | `creachadair/jrpc2` documents a single `-32700` response for an empty batch, which differs from the official JSON-RPC example and this package's `-32600` decision. |
| Selected behavior | Return one `Invalid Request` Response Object with `id: null`, not an Array. |
| Consequences | The response shape differs from a nonempty batch and is stable across transports. |
| Executable evidence | `TestDispatcherBatchEdgeCases`, `TestSpecificationExamples` |
| Public surface | `Dispatcher.Dispatch`, `HTTPHandler.ServeHTTP` |
| Upstream record | The behavior is explicitly shown in the specification examples. |
| Reconsider when | A successor specification changes the required empty-batch response shape. |

## JSONRPC-DEC-005: Invalid members inside nonempty batches

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Batch](https://www.jsonrpc.org/specification#batch) and [Examples](https://www.jsonrpc.org/specification#examples) |
| Classification | Normative interpretation |
| Issue | Batch text says a response corresponds to each Request except notifications, while examples also return errors for members that are not Request Objects. |
| Credible interpretations | Omit invalid members because they are not Requests; abort the whole batch; or include one `Invalid Request` response for each invalid member. |
| Known peer behavior | `creachadair/jrpc2` likewise emits null-ID errors for invalid id-less batch members and preserves their positions. |
| Selected behavior | Process each nonempty batch member independently. Every invalid member contributes an `Invalid Request` response with `id: null`; valid notifications contribute no response. |
| Consequences | One malformed member does not suppress valid work or turn the entire batch into a parse error. Duplicate null IDs mean clients must not attempt to correlate invalid-member errors by ID. |
| Executable evidence | `TestDispatcherBatch`, `TestSpecificationExamples` |
| Public surface | `Dispatcher.Dispatch` |
| Upstream record | The mixed-batch specification example is the governing interoperability fixture. |
| Reconsider when | An erratum defines whole-batch failure or different invalid-member semantics. |

## JSONRPC-DEC-006: Batch execution and response order

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Batch](https://www.jsonrpc.org/specification#batch) |
| Classification | Optional behavior and compatibility policy |
| Issue | Servers may process members concurrently and return responses in any order. Whether an implementation preserves input order is therefore observable but not portable. |
| Credible interpretations | Concurrent unordered execution; sequential input-order execution; or configurable scheduling. |
| Known peer behavior | `creachadair/jrpc2` preserves response order as stable behavior but explicitly warns clients not to depend on it. |
| Selected behavior | The dispatcher currently executes sequentially and emits responses in input order after omitting notifications. This is an implementation detail, not a public guarantee. Clients always correlate by ID and accept any response order. |
| Consequences | Handlers currently avoid package-created batch concurrency, but callers must not rely on order for compatibility or side effects. A future minor release may add opt-in concurrency without changing client semantics. |
| Executable evidence | `TestDispatcherBatch`, `TestClientBatch`, `TestClientBatchValidation` |
| Public surface | `Dispatcher.Dispatch`, `Client.Batch` |
| Upstream record | The base specification explicitly permits any processing and response order. |
| Reconsider when | Parallel dispatch is introduced or a successor specification constrains ordering. |

## JSONRPC-DEC-007: Parse error versus invalid request

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Error Object](https://www.jsonrpc.org/specification#error_object), [Request Object](https://www.jsonrpc.org/specification#request_object), and [Batch](https://www.jsonrpc.org/specification#batch) |
| Classification | Normative interpretation |
| Issue | The specification distinguishes invalid JSON from a valid JSON value that is not a valid Request, but does not enumerate every decoder failure. |
| Credible interpretations | Map every decoding failure to Parse Error; or reserve Parse Error for malformed JSON and map envelope/type violations to Invalid Request. |
| Known peer behavior | `creachadair/jrpc2` describes malformed whole-batch JSON as `-32700`; no broader cross-peer classification matrix is pinned yet. |
| Selected behavior | Empty, syntactically malformed, invalid-UTF-8, or trailing JSON is `Parse Error`. A syntactically valid scalar, array member, or object with an invalid JSON-RPC envelope is `Invalid Request`. Parameter decoding after a valid envelope is `Invalid params`. |
| Consequences | Error codes remain deterministic across malformed syntax, envelope validation, and handler parameter validation. Invalid UTF-8 is rejected rather than replaced. |
| Executable evidence | `TestDispatcherProtocolErrors`, `TestProtocolRejectsInvalidUTF8`, `TestDispatcherClassifiesNestedParameterDuplicatesAsInvalidParams`, `FuzzDispatcher` |
| Public surface | `Dispatcher.Dispatch`, `DecodeParams` |
| Upstream record | No exhaustive error-classification table is published beyond the standard error definitions and examples. |
| Reconsider when | An erratum classifies an input currently covered by this policy differently. |

## JSONRPC-DEC-008: Duplicate JSON object members

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | RFC 8259 [Objects](https://www.rfc-editor.org/rfc/rfc8259#section-4); JSON-RPC 2.0 request and response object definitions |
| Classification | Defensive interoperability policy |
| Issue | RFC 8259 says object member names SHOULD be unique and documents unpredictable peer behavior, while JSON-RPC does not define duplicate handling. |
| Credible interpretations | First value wins, last value wins, collect duplicates, or reject the ambiguous object. |
| Known peer behavior | RFC 8259 records first-value, last-value, all-value, and failure behavior across parsers; no JSON-RPC-specific peer fixture is pinned yet. |
| Selected behavior | Reject duplicate members in protocol envelopes and named parameter objects. A duplicate request-envelope member produces `Invalid Request`; nested duplicate named parameters produce `Invalid params`; malformed responses are rejected by the client. |
| Consequences | Ambiguous input cannot change meaning across parsers. This is intentionally stricter than Go's default last-value-wins decoding. |
| Executable evidence | `TestProtocolDecodersRejectDuplicateMembers`, `TestDispatcherClassifiesNestedParameterDuplicatesAsInvalidParams`, client malformed-response tests |
| Public surface | JSON unmarshaling for `Request`, `Response`, and `Error`; `DecodeParams`; client validation |
| Upstream record | RFC 8259 records the interoperability risk; JSON-RPC has no duplicate-member rule. |
| Reconsider when | A JSON-RPC revision defines duplicate-member behavior or an interoperability profile requires another deterministic policy. |

## JSONRPC-DEC-009: Notification-only HTTP responses

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0 [Notification](https://www.jsonrpc.org/specification#notification); HTTP Semantics [204 No Content](https://www.rfc-editor.org/rfc/rfc9110#name-204-no-content) |
| Classification | Transport-specific policy |
| Issue | JSON-RPC requires no Response Object for a notification but does not standardize an HTTP status code or whether an empty HTTP response is sent. |
| Credible interpretations | HTTP 200 with an empty body, HTTP 202, or HTTP 204. |
| Known peer behavior | No maintained JSON-RPC HTTP peer is pinned because JSON-RPC 2.0 defines no official HTTP binding. |
| Selected behavior | Return HTTP 204 with an empty body for a valid single notification or notification-only batch. `HTTPTransport` exposes any 204 as a nil payload; higher-level call validation rejects a missing response when a response was required. |
| Consequences | No JSON token such as `null` or `[]` is emitted. HTTP clients can distinguish intentional no-content success from a malformed empty 200 response. |
| Executable evidence | `TestHTTPHandlerRequestAndNotification`, `TestHTTPTransportNoContent`, `TestDispatcherBatchEdgeCases` |
| Public surface | `HTTPHandler.ServeHTTP`, `HTTPTransport.RoundTrip` |
| Upstream record | JSON-RPC 2.0 defines no official HTTP binding. |
| Reconsider when | An adopted JSON-RPC HTTP binding standardizes a different notification response. |

## JSONRPC-DEC-010: HTTP methods, media types, and status mapping

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `jsonrpc` maintainers |
| Source | JSON-RPC 2.0; RFC 9110 [Methods](https://www.rfc-editor.org/rfc/rfc9110#name-methods); RFC 6839 [Structured Syntax Suffixes](https://www.rfc-editor.org/rfc/rfc6839) |
| Classification | Transport-specific and defensive policy |
| Issue | JSON-RPC 2.0 is transport agnostic and does not define HTTP methods, accepted media types, or status codes for protocol errors. |
| Credible interpretations | POST-only or GET/POST; `application/json` only or legacy/vendor JSON types; map JSON-RPC errors to HTTP errors or keep protocol replies at HTTP 200. |
| Known peer behavior | No maintained peer matrix is pinned for method, media-type, and status behavior; adopters must treat these rules as this package's binding contract. |
| Selected behavior | Accept POST only. Accept `application/json`, `application/json-rpc`, and `application/*+json` with valid parameters. Validly transported JSON-RPC responses, including JSON-RPC errors, use HTTP 200. Notification-only success uses 204. Method, media-type, body-size, and body-read failures use HTTP 405, 415, 413, and 400 respectively. |
| Consequences | Transport failures remain distinct from JSON-RPC error objects. Vendor JSON media types interoperate without accepting non-JSON content. GET cannot trigger RPC side effects. |
| Executable evidence | `TestHTTPHandlerTransportErrors`, `TestJSONContentTypes`, `TestHTTPHandlerRequestAndNotification`, `TestHTTPTransportRoundTrip` |
| Public surface | `HTTPHandler`, `HTTPTransport`, `IsJSONContentType` |
| Upstream record | JSON-RPC 2.0 publishes no normative HTTP binding. |
| Reconsider when | The project adopts a normative HTTP binding or a security review narrows accepted structured-suffix media types. |

## Unresolved decisions

No known material JSON-RPC 2.0 interpretation is unresolved at this revision.
Independent peer fixtures remain incomplete where noted above and must be
added before repository-wide interoperability completion can be claimed. New
ambiguities remain unresolved until they receive a stable identifier,
normative analysis, executable evidence, and maintainer disposition here.
