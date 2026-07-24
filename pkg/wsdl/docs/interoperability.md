# Interoperability

Round-trip tests cover WSDL 1.1 SOAP 1.1/1.2, HTTP, MIME, imports, embedded
schemas, and WSDL 2.0 Core/SOAP/HTTP adjunct forms. The requirement matrices
link each current executable claim.

The WSDL 1.1 external corpus contains unmodified, provenance-pinned fixtures
from SmartBear SoapUI, Microsoft `dotnet-svcutil`, Apache CXF, and DHL Business
Customer Shipping. `interop_test.go` parses and validates each description,
inspects its service components, and proves canonical serialize-parse-
serialize stability. The three tool fixtures also compile into immutable
service graphs. Location-less SOAP encoding resolution uses an explicitly
injected copy of the official schema.

The DHL WSDL and both referenced schemas are pinned exactly. The WSDL itself
is executable interoperability evidence. Its CIS schema declares ISO-8859-1;
parsing that external schema remains a `xsd` responsibility and is not a
WSDL-layer claim.

The repository includes the W3C WSDL 2.0 test suite's accepted `IRI-1G` and
`Multipart-1G` documents. Their upstream bytes are pinned in the provenance
manifest; checked-in copies differ only by a final newline. Tests parse,
compile, deterministically serialize, and reparse every checked-in W3C fixture.

Mainstream SoapUI, `dotnet-svcutil`, and Apache CXF WSDL-to-code tooling targets
WSDL 1.1. For WSDL 2.0, `make interoperability` downloads the SHA-256-pinned
Apache Woden 1.0M10 Java implementation and its pinned dependencies, enables
Woden validation, and compares its interface, operation, message-pattern, and
style summaries with the toolkit's compiled graphs for both checked-in W3C
descriptions. Compilation and execution use a digest-pinned Eclipse Temurin 25
container, so the gate requires Docker rather than an untracked host Java
installation. The repository does not claim nonexistent .NET or CXF WSDL 2.0
support.

`specification/interoperability.tsv` records every corpus file's producer,
license, immutable source revision, path, SHA-256 digest, byte count, and URL.
`make provenance` verifies local bytes, while `VERIFY_REMOTE=1 make provenance`
also verifies every upstream object. `specification/tooling.tsv` applies the
same digest and size discipline to the downloaded Woden gate artifacts.

Never add customer WSDLs containing credentials, internal hosts, or contractual
data. Reduce them to licensed structural fixtures first.
