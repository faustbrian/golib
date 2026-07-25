# Interoperability corpus

Every file in this directory is pinned by byte length, SHA-256 digest, source
revision, upstream URL, and license in
[`specification/interoperability.tsv`](../../specification/interoperability.tsv).
The files are unmodified upstream bytes.

The executable corpus in `interop_test.go` exercises these WSDL 1.1 sources:

- `soapui/geocoder.wsdl` is from SmartBear SoapUI's system-test corpus.
- `dotnet/simple.wsdl` is from Microsoft `dotnet-svcutil`'s test corpus.
- `java/customer-service.wsdl` is Apache CXF's WSDL-first Java sample.
- `carrier/dhl-bcs-3.3.2` is a DHL Business Customer Shipping description and
  its two referenced schemas from the MIT-licensed Netresearch SDK.

Each WSDL must parse, validate, expose inspectable service components,
serialize, reparse, and serialize to the same canonical bytes. The SoapUI,
`dotnet-svcutil`, and Apache CXF descriptions must also compile into service
graphs. SoapUI's location-less SOAP encoding import is resolved only through
the explicitly injected W3C schema in `official`; no test performs network or
filesystem resolution through the library.

The DHL WSDL itself is part of the executable round-trip corpus. Its external
schemas remain pinned as exact provenance artifacts. The CIS schema declares
ISO-8859-1, so compiling those external schemas is a `xsd` concern and is
not claimed by the WSDL interoperability test.

`woden/WodenProbe.java` is the repository-owned probe used by
`make interoperability`. It enables validation in Apache Woden 1.0M10 and
requires both W3C WSDL 2.0 fixtures to match the checked-in interface,
operation, message-pattern, and style summary in `woden/expected.tsv`.
Downloaded tool artifacts are pinned in `specification/tooling.tsv` and are
never checked into the repository.

Redistribution notices are preserved in `licenses`. The W3C SOAP encoding
schema carries its complete software notice in the file itself.
