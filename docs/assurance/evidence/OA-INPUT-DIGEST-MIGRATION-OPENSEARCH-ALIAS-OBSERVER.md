# OA OpenSearch Alias Observer Input-Digest Migration

The OpenSearch operational-assurance input digest changed from
`ba38d91565359079d8b16264d386002e0a18f9b4abfb2cdf9b70600926db5690`
to
`410c977b9a7bca12f86af19a0a3c0a1b2c52b43554b55d63f90cfcd65ee7b967`.

The only changed OpenSearch module input corrected a real-cluster test
observer: after the final alias is removed, OpenSearch returns HTTP 200 with
an empty alias map rather than HTTP 404. Production code, supported engine
versions, service images, lifecycle behavior, and the exercised operational
contracts did not change.

The complete pinned OpenSearch 2.19.6 and 3.8.0 conformance and comparative
benchmark matrix was rerun successfully through `2026-08-13T14:32:49Z` after
the observer correction. Interoperability and the standalone benchmark gate
also passed against the replacement input. This reviewed migration therefore
preserves earlier operational observations without relabeling their execution
times or claiming that they were rerun.
