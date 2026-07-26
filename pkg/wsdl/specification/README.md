# Specification provenance

`manifest.tsv` pins the stable WSDL 1.1 Note, WSDL 2.0 Recommendation,
adjuncts, additional message exchange patterns, and published schemas used by
this project. Dated W3C URLs are preferred. Namespace documents are snapshots:
their recorded digest and size define the reviewed input.

The two requirement matrices are independent because WSDL 1.1 and WSDL 2.0
have different component models and normative status. A row is `implemented`
only when its evidence names an executable test or gate. `partial` and
`missing` rows are not conformance claims.

`assertions/wsdl-2.0.tsv` inventories all 84 Core and 110 Adjuncts assertion
identifiers from the Recommendations and maps each to a requirement row.
`assertions/wsdl-1.1.tsv` inventories the 23 distinct normative-keyword
statements in the 2001 Note by section and normalized-text digest because the
Note does not assign assertion identifiers. The provenance gate rejects
duplicates, missing rows, count drift, malformed sources, and unknown groups.

The published WSDL 2.0 errata page reports no substantive or editorial errata.
The WSDL 1.1 Note has no W3C errata document; implementation decisions record
the reviewed prose/schema discrepancies instead of implying corrections that
were never published.

`discrepancies-and-extensions.md` records those discrepancies and enumerates
every modeled foreign-attribute and foreign-element extension boundary.

Run `make provenance` for offline structural checks. Set `VERIFY_REMOTE=1` to
download and hash every pinned resource. Parsing and compilation never use
these URLs implicitly.

`interoperability.tsv` pins every external corpus file by producer, license,
source revision, local path, digest, size, and URL. Offline provenance checks
verify local bytes; remote verification checks the upstream objects too.
`tooling.tsv` pins downloaded interoperability tools and their dependencies by
version, license, digest, size, and immutable artifact URL.
