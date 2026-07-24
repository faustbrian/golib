# SOAP bindings

WSDL 1.1 models SOAP 1.1 and 1.2 binding, operation, body, header,
header-fault, fault, and address extensions. Presence of action, style, use,
namespace, encoding styles, body parts, and SOAP 1.2 action-required is
preserved. Validation checks style/use vocabularies, transport URI, part and
message references, header faults, and bound fault names.

WSDL 2.0 models SOAP version, protocol, default and operation MEP, action,
modules, headers, fault codes/subcodes, `#any`, and required/must-understand
presence. Protocol, MEP, action, and module references must be absolute IRIs.

These structures describe bindings only. SOAP envelopes and faults belong to
`wire`; no operation executes a request.
