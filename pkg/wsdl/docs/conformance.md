# Versions and conformance

WSDL 1.1 targets the W3C Note dated 15 March 2001. WSDL 2.0 targets the Core
and Adjuncts Recommendations dated 26 June 2007; the Additional MEPs Note is
tracked separately. Exact bytes and URLs are in `specification/manifest.tsv`.

Conformance is evidence-based, not inferred from accepting an XML element.
The independent matrices under `specification/requirements` identify each
claim, source, status, and executable evidence. A foreign extension is
preserved but is not understood unless the caller explicitly lists its QName
during validation.

The toolkit does not claim WS-Security, WS-Addressing, WS-Policy, transport,
credentials, retries, or SOAP execution. Each would require a separate
support matrix and explicit runtime boundary.
