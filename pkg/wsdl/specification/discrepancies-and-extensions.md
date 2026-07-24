# Schema discrepancies, errata, and extension points

## Reviewed discrepancies

- WSDL 1.1 operation child order determines one-way, request-response,
  solicit-response, or notification semantics. The model preserves the order
  rather than relying on a schema-shaped input/output pair.
- WSDL 1.1 SOAP `encodingStyle` is a whitespace-separated URI list. The typed
  model retains the complete list and does not collapse it to one URI.
- The WSDL 1.1 and 2.0 schemas admit documents whose cross-component QNames,
  binding operation identities, message labels, MEPs, and protocol properties
  are invalid. `Validate` and `compile` therefore enforce semantic constraints
  after structural parsing.
- WSDL 2.0 Adjuncts defines `wsdlx:safe`, while the dated schema also declares
  an unqualified `safe`. Parsing accepts either spelling, rejects both at once,
  and canonical serialization emits the Adjuncts spelling.
- Defaultable SOAP and HTTP values retain presence flags. This distinguishes
  an absent value from an explicitly supplied schema default during semantic
  comparison and round trips.

The published WSDL 2.0 errata page records no substantive or editorial
errata. WSDL 1.1 is a W3C Note and has no published W3C errata document.

## Extension-point inventory

WSDL 1.1 foreign attributes and elements are preserved at definitions,
import, types, message, part, port type, operation, operation message, binding,
binding operation, binding message, service, and port boundaries.

WSDL 2.0 foreign attributes and elements are preserved at description,
import, include, types, interface, interface fault, interface operation,
interface message and fault references, binding, binding fault, binding
operation, binding message and fault references, SOAP module and header, HTTP
header, service, and endpoint boundaries.

Unknown optional extensions remain opaque data. A required extension is valid
only when its expanded QName is explicitly listed in validation options.
Nested required extensions are traversed at every boundary above.
