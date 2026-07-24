# Compatibility policy

The project follows [Semantic Versioning](https://semver.org/) for tagged
releases. Protocol behavior is compatibility-sensitive even where a Go type or
function signature does not change.

## Stable releases

As of `v1.0.0`:

- patch releases fix defects without intentionally changing valid public
  behavior;
- minor releases add backward-compatible APIs and optional behavior;
- major releases may remove or change exported APIs or documented semantics.

Supported Go versions follow the Go release policy: the latest two stable Go
release families are tested when practical. Raising the minimum Go version is
documented in the changelog and normally occurs in a minor release.

## Wire compatibility

The following are treated as public compatibility commitments:

- JSON-RPC version, request, response, notification, and batch semantics;
- standard error codes and messages;
- exact ID type preservation and echoing;
- whether a response is omitted;
- response result/error exclusivity;
- client validation of mismatched, missing, duplicate, or malformed responses;
- rejection of duplicate request, response, and error-envelope members;
- case-sensitive matching of reserved protocol member names;
- rejection of invalid UTF-8 without replacement;
- HTTP methods, accepted media types, status behavior, and default limits;
- dispatcher byte and batch-member limits and their whole-request error shape;
- transport-neutral client response limits and oversized-response errors;
- middleware order and request-context availability;
- exported sentinel identities recognized through `errors.Is`.

A change that causes a previously valid exchange to become invalid, changes an
error classification, or changes an observable wire value is breaking unless
it corrects a documented specification violation. Compliance fixes are called
out prominently and receive migration guidance even when released before a new
major version.

## Deprecation

An exported API is normally deprecated in at least one minor release before
removal in a major release. Deprecation comments name the replacement and
migration path. Security or correctness issues may require faster removal;
those exceptions are documented with their risk.

## Out of contract

Map key order, allocation counts, exact internal error causes, benchmark timing,
and sequential batch execution are implementation details unless explicitly
promoted to guarantees. Batch response order must never be relied upon because
the JSON-RPC specification allows any order.
