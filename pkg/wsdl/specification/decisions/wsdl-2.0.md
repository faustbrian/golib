# WSDL 2.0 decisions

## WSDL20-DEC-001: The 26 June 2007 Recommendation is the baseline

- Status: accepted
- Date: 2026-07-20

Core and Adjuncts dated Recommendations are normative. Additional MEPs are a
W3C Note and are supported as named patterns without upgrading that Note's
normative status.

## WSDL20-DEC-002: Absent and defaulted values remain distinguishable

- Status: accepted
- Date: 2026-07-20

Presence flags are retained for defaultable SOAP and HTTP adjunct properties.
Message content models retain whether the `element` attribute was absent even
when the effective value is `#other`.

## WSDL20-DEC-003: Unknown MEPs are extensible

- Status: accepted
- Date: 2026-07-20

Every MEP identifier must be absolute. Direction and label rules are enforced
for the eight recognized patterns. Unknown absolute identifiers remain valid
because WSDL permits independently defined message exchange patterns.

## WSDL20-DEC-004: RPC validation spans WSDL and compiled XSD

- Status: accepted
- Date: 2026-07-20

The core validator owns RPC style, MEP, wrapper-reference, and signature
rules that require only WSDL components. The compiler owns wrapper sequence,
particle, attribute, named-type, and parameter mapping rules because those
require the immutable `xsd` component graph.

## WSDL20-DEC-005: Safety accepts the Recommendation and schema spellings

- Status: accepted
- Date: 2026-07-20

Adjuncts defines `wsdlx:safe`, while the dated WSDL 2.0 schema also declares
an unqualified `safe` operation attribute. Parsing accepts either spelling,
rejects simultaneous use, and deterministic serialization emits the normative
Adjuncts spelling.

## WSDL20-DEC-006: Operation style validation spans WSDL and XSD

- Status: accepted
- Date: 2026-07-20

Local validation enforces the IRI and multipart initial-message content model
and wrapper name. Compilation enforces the remaining XML Schema shape rules:
complex sequences, local children, attribute restrictions, allowed IRI simple
types, and multipart occurrence and uniqueness constraints. This preserves the
boundary where `xsd` owns schema parsing and compilation.
