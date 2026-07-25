# WSDL 1.1 decisions

## WSDL11-DEC-001: The 15 March 2001 Note is the baseline

- Status: accepted
- Date: 2026-07-20

WSDL 1.1 is a W3C Note rather than a Recommendation. The dated Note and its
published schemas are the stable baseline. SOAP 1.2 binding support is an
explicit adjunct capability and does not change core WSDL 1.1 semantics.

## WSDL11-DEC-002: Operation order is preserved as typed style

- Status: accepted
- Date: 2026-07-20

The parser records one-way, request-response, solicit-response, and
notification style because the order of input and output is semantic. The
serializer uses that style rather than normalizing every operation to input
then output.

## WSDL11-DEC-003: Binding extensions remain data, not runtime clients

- Status: accepted
- Date: 2026-07-20

SOAP, HTTP, and MIME extensions are parsed and validated. They never perform
requests, encode SOAP envelopes, acquire credentials, or invoke endpoints.
