# FAQ

## Does parsing download imports?

No. Parsing is byte-local. Compilation resolves only through explicitly
injected WSDL and schema resolvers; both defaults deny.

## Does this package send SOAP or HTTP requests?

No. It describes and validates bindings. Envelope primitives belong to
`wire`; transport belongs to a client package.

## Why are WSDL 1.1 and 2.0 separate models?

Their component models, operation semantics, and binding adjuncts differ.
Combining them would erase meaningful presence and direction information.

## Is a successful parse a conformance result?

No. Run validation and, for imports or schemas, compilation. Consult the
version matrix for the exact supported claim.

## Can unknown extensions round-trip?

Yes, within limits. A required extension still fails validation unless the
caller explicitly declares that QName understood.
