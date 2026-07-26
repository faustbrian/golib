# XML Schema integration

Embedded schemas are parsed by `xsd` and remain `*xsd.Document` values.
`wsdl` never implements a competing schema type system. During compilation,
inline schemas receive stable synthetic URIs, direct WSDL 2.0 schema imports
are included, and `xsd/compile` builds the schema component set.

WSDL validation then checks message elements and types, SOAP header elements,
and HTTP header types against that set. Built-in XML Schema types are accepted
without a local declaration.

The compiler also uses the immutable schema set for WSDL 2.0 RPC, IRI, and
multipart operation-style constraints. These checks inspect compiled element,
simple-type, complex-type, sequence, particle, and attribute components; they
do not duplicate XML Schema parsing or instance validation.

Schema resolution is configured separately from WSDL resolution. This keeps
trust policy explicit and prevents a WSDL resolver from becoming an accidental
general-purpose file or network loader.
