# Goal: Make JSON-RPC Ambiguity Decisions Explicit

## Objective

Create an auditable JSON-RPC decision register and peer interoperability suite
without modifying the original implementation and hardening goals.

## Required Work

- Implement `docs/specification-decisions.md` under the root specification
  decision contract.
- Cover malformed no-ID batch members, valid notifications, explicit null IDs,
  numeric IDs and precision, empty batches, invalid members,
  notification-only batches, response membership and ordering, parse error
  versus invalid request, duplicate JSON members, reserved methods, content
  types, HTTP statuses, and no-content transport behavior.
- Record whether each behavior is normative, recommended, defensive,
  transport-specific, extension-specific, or application policy.
- Differential-test jrpc2, filecoin-project/jsonrpc, and other maintained
  peers where behavior overlaps.
- Minimize disagreements and classify local defect, peer defect, fixture defect,
  harness defect, specification ambiguity, or intentional policy difference.
- Do not copy jrpc2 or majority behavior without normative justification.
- Link decisions to conformance rows, tests, fixtures, docs, and changelog.

## Completion Criteria

- Batch, notification, ID, error, and HTTP choices are explicit and tested.
- Clients and servers have deterministic documented interoperability behavior.
- No compliance claim depends on undocumented interpretation.
