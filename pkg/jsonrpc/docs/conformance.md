# JSON-RPC 2.0 conformance matrix

This matrix maps every normative JSON-RPC 2.0 rule to implementation, test, and
documentation evidence. The official
[JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification) is
authoritative. Defensive JSON policies are labeled separately and do not claim
to be additional JSON-RPC requirements.

Status meanings:

- **Verified**: direct automated evidence covers the rule and relevant failure
  shape.
- **Partial**: implementation exists, but adversarial, fixture, or client/server
  evidence is incomplete.
- **Open**: required implementation or direct evidence is missing.
- **Policy**: intentional defensive behavior outside a JSON-RPC MUST or SHOULD.

## Request and notification

| Normative rule | Source | Implementation | Test or fixture | Documentation | Status |
| --- | --- | --- | --- | --- | --- |
| A call is a Request Object. | [Request Object](https://www.jsonrpc.org/specification#request_object) | `Request.UnmarshalJSON`, `Dispatcher.Dispatch` | `TestDispatcherProtocolErrors` | [Architecture](architecture.md#protocol) | Verified |
| `jsonrpc` MUST be the String `"2.0"`. | [Request Object](https://www.jsonrpc.org/specification#request_object) | `Request.Validate` | `TestRequestValidation` | [API](api.md#protocol) | Verified |
| `method` MUST be a String. | [Request Object](https://www.jsonrpc.org/specification#request_object) | `Request.UnmarshalJSON`, `Request.Validate` | `TestRequestValidation`, `TestSpecificationExamples` | [Troubleshooting](troubleshooting.md#invalid-request--32600) | Verified |
| Names beginning with `rpc.` are reserved and MUST NOT be used for application methods. | [Request Object](https://www.jsonrpc.org/specification#request_object) | `Registry.Register`, `validateClientMethod` | `TestRegistryRegistration`, `TestClientOptionsAndDefensivePaths` | [API](api.md#server) | Verified |
| `params`, if present, MUST be an Object or Array. | [Parameter Structures](https://www.jsonrpc.org/specification#parameter_structures) | `Request.Validate`, `encodeParams` | `TestRequestValidation`, `TestRequestBuilders` | [API](api.md#protocol) | Verified |
| Positional params use an Array in expected order. | [Parameter Structures](https://www.jsonrpc.org/specification#parameter_structures) | Handler contract, `DecodeParams` | `testdata/conformance/jsonrpc-2.0-specification.json`, `TestSpecificationExamples` | [Cookbook](cookbook.md) | Verified |
| Named params use an Object and names MUST match exactly, including case. | [Parameter Structures](https://www.jsonrpc.org/specification#parameter_structures) | `DecodeParams`, `namedParameterNamesMatch` | `TestDecodeParams`, `TestDecodeParamsEmbeddedAndTaggedFields` | [Troubleshooting](troubleshooting.md#invalid-params--32602) | Verified |
| `id`, if present, MUST be a String, Number, or Null. | [Request Object](https://www.jsonrpc.org/specification#request_object) | ID constructors and decoders | `TestIDRoundTripAndEquality`, `TestIDRejectsInvalidJSONTypes`, `TestStringIDCorrelationMatchesItsJSONEncoding` | [API](api.md#protocol) | Verified |
| Numeric IDs SHOULD NOT contain fractional parts. | [Request Object note 2](https://www.jsonrpc.org/specification#request_object) | Fractional IDs preserved as allowed interoperability input | `TestIDRoundTripAndEquality` | [FAQ](faq.md) | Verified: accepted and preserved, not generated |
| Null IDs SHOULD normally be avoided. | [Request Object note 1](https://www.jsonrpc.org/specification#request_object) | `NullID`; explicit null remains a request | `TestRequestDistinguishesNotificationFromNullID` | [FAQ](faq.md) | Verified |
| A server MUST echo the same included ID. | [Request Object](https://www.jsonrpc.org/specification#request_object) | `Dispatcher.execute`, raw `ID` preservation | `TestDispatcherSingleRequests`, `TestIDRoundTripAndEquality`, `TestDecimalExponentArithmetic` | [Compatibility](compatibility.md#wire-compatibility) | Verified |
| A Notification is a Request without `id`. | [Notification](https://www.jsonrpc.org/specification#notification) | `Request.IsNotification` | `TestRequestDistinguishesNotificationFromNullID` | [API](api.md#protocol) | Verified |
| A server MUST NOT reply to a Notification, including in a batch. | [Notification](https://www.jsonrpc.org/specification#notification) | `dispatchItem`, `dispatchBatch` | `TestDispatcherBatch`, `TestDispatcherBatchEdgeCases`, panic/error notification coverage | [Architecture](architecture.md#dispatch) | Verified |

## Response and error object

| Normative rule | Source | Implementation | Test or fixture | Documentation | Status |
| --- | --- | --- | --- | --- | --- |
| `jsonrpc` MUST be the String `"2.0"`. | [Response Object](https://www.jsonrpc.org/specification#response_object) | `Response.Validate` | `TestResponseValidation`, client validation tests | [API](api.md#protocol) | Verified |
| `result` MUST exist on success and MUST NOT coexist with `error`. | [Response Object](https://www.jsonrpc.org/specification#response_object) | `Response.Validate`, `Response.MarshalJSON` | `TestResponseValidation` | [Architecture](architecture.md#client-validation) | Verified |
| `error` MUST exist on failure and MUST NOT coexist with `result`. | [Response Object](https://www.jsonrpc.org/specification#response_object) | `Response.Validate`, `errorResponse` | `TestResponseValidation`, `TestDispatcherProtocolErrors` | [API](api.md#errors) | Verified |
| Response `id` is REQUIRED and MUST match the Request ID; unknown IDs use Null. | [Response Object](https://www.jsonrpc.org/specification#response_object) | `Response.Validate`, dispatcher error shaping, client matching | `TestClientCallErrors`, `TestClientMatchesEquivalentNumericID`, protocol error tests | [Troubleshooting](troubleshooting.md#client-says-mismatched-id) | Verified |
| Error `code` MUST be an Integer. | [Error Object](https://www.jsonrpc.org/specification#error_object) | `Error.UnmarshalJSON` | `TestResponseValidation`, `TestProtocolDefensivePaths` | [API](api.md#errors) | Verified |
| Error `message` MUST be a String. | [Error Object](https://www.jsonrpc.org/specification#error_object) | `Error.UnmarshalJSON` | `TestResponseValidation`, `TestProtocolDefensivePaths` | [API](api.md#errors) | Verified |
| Error `data` MAY contain a Primitive or Structured value. | [Error Object](https://www.jsonrpc.org/specification#error_object) | `Error.Data`, `Error.WithData` | `TestRPCErrorModel`, `FuzzErrorUnmarshal` | [API](api.md#errors) | Verified |
| Parse error is `-32700`; invalid request `-32600`; method not found `-32601`; invalid params `-32602`; internal error `-32603`. | [Error Object table](https://www.jsonrpc.org/specification#error_object) | Standard constructors and dispatcher mapping | `TestStandardErrors`, `TestDispatcherProtocolErrors` | [Troubleshooting](troubleshooting.md) | Verified |
| `-32000` through `-32099` are reserved for implementation-defined server errors. | [Error Object table](https://www.jsonrpc.org/specification#error_object) | `CodeRequestLimitExceeded` uses `-32000`; `NewError` preserves caller-selected codes | `TestRPCErrorModel` | [API](api.md#errors), [Cookbook](cookbook.md#custom-application-errors) | Verified; applications are guided outside the full reserved range |

## Batch

| Normative rule | Source | Implementation | Test or fixture | Documentation | Status |
| --- | --- | --- | --- | --- | --- |
| A batch is an Array of Request Objects. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchBatch` | `testdata/conformance/jsonrpc-2.0-specification.json`, `TestSpecificationExamples` | [Architecture](architecture.md#dispatch) | Verified |
| The server SHOULD return an Array after processing all members. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchBatch` | `TestDispatcherBatch` | [Architecture](architecture.md#dispatch) | Verified |
| One response SHOULD exist per non-notification request. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchBatch`; client membership checks | `TestDispatcherBatch`, `TestClientBatchValidation` | [Troubleshooting](troubleshooting.md#missing-or-duplicate-batch-response) | Verified |
| Batch members MAY execute concurrently and in any order. | [Batch](https://www.jsonrpc.org/specification#batch) | Sequential execution is an allowed policy | `TestDispatcherBatch` | [Compatibility](compatibility.md#out-of-contract) | Verified |
| Response objects MAY appear in any order; clients SHOULD correlate by ID. | [Batch](https://www.jsonrpc.org/specification#batch) | `Client.Batch` pending map | `TestClientBatch`, duplicate/missing/mismatched tests | [Architecture](architecture.md#client-validation) | Verified |
| Invalid JSON for the entire batch returns one parse-error Response Object. | [Batch](https://www.jsonrpc.org/specification#batch) | `Dispatcher.Dispatch` pre-validation | `TestDispatcherProtocolErrors`, official example | [Troubleshooting](troubleshooting.md#parse-error--32700) | Verified |
| An empty Array returns one invalid-request Response Object. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchBatch` | `TestDispatcherBatchEdgeCases` | [Architecture](architecture.md#protocol) | Verified |
| Invalid nonempty members each return an invalid-request response in the response Array. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchItem` | `TestDispatcherBatchEdgeCases`, mixed official fixture | [Architecture](architecture.md#protocol) | Verified |
| A notification-only batch MUST NOT return an empty Array and should return nothing. | [Batch](https://www.jsonrpc.org/specification#batch) | `dispatchBatch` | `TestDispatcherBatchEdgeCases`, `TestHTTPHandlerRequestAndNotification` | [Architecture](architecture.md#transport) | Verified |
| Processing must be bounded against hostile batch size. | Defensive runtime policy | Dispatcher byte and member limits | `TestDispatcherRejectsResourceLimitViolationsBeforeDispatch` | [Hardening report](hardening.md#findings) | Policy verified |

## Defensive JSON and client policy

| Policy | Authority or rationale | Implementation | Evidence | Status |
| --- | --- | --- | --- | --- |
| Reject duplicate protocol member names. | [RFC 8259 section 4](https://www.rfc-editor.org/rfc/rfc8259#section-4) warns that duplicate handling is unpredictable. | `rejectDuplicateMembers` | `TestProtocolDecodersRejectDuplicateMembers` | Policy verified |
| Match reserved names case-sensitively. | [JSON-RPC conventions](https://www.jsonrpc.org/specification#conventions) | `rejectDuplicateMembers` reserved-name check | `TestProtocolDecodersRejectCaseVariantsOfReservedMembers` | Verified |
| Reject invalid UTF-8 without replacement. | [RFC 8259 section 8.1](https://www.rfc-editor.org/rfc/rfc8259#section-8.1) | Protocol decoders and dispatcher pre-validation | `TestProtocolRejectsInvalidUTF8`, fuzz seeds | Verified |
| Reject malformed, unsolicited, duplicate, or missing client responses. | Correlation integrity required by request/response and batch rules | `Response.Validate`, `Client.Call`, `Client.Batch` | `TestClientCallErrors`, `TestClientBatchValidation` | Verified |
| Reject trailing JSON after a directly decoded ID. | A JSON unmarshaler consumes exactly one complete value. | `ID.UnmarshalJSON` EOF check | `TestProtocolDefensivePaths`, `FuzzIDRoundTrip` | Policy verified |
| Reject duplicate generated IDs within one client batch. | Duplicate IDs cannot identify one corresponding request. | `Client.Batch` | `TestClientBatchRejectsDuplicateRequestIDsBeforeTransport` | Policy verified |
| Bound client parsing for every transport. | Malicious replies must not control unbounded protocol allocation. | `Client.roundTrip`, `WithMaxClientResponseBytes` | `TestClientRejectsOversizedGenericTransportResponse` | Policy verified |

## Remaining evidence work

- Re-run every baseline command after all fixes and record the final platform,
  tool versions, benchmark results, and any environmental limitation.
