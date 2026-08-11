# OpenSearch conformance

The supported runtime matrix is exactly OpenSearch 2.19.6 and 3.8.0 with
`opensearch-go/v4` 4.7.3. The source archives and their reviewed SHA-256
digests are pinned in the
[specification manifest](../specification/manifest.tsv). Observable REST and
adapter choices are governed by the
[specification decision register](specification-decisions.md).

## Required gates

`make specification-sources` downloads every pinned source under a finite
per-source bound and fails on missing rows, download errors, or digest drift.
`make conformance` runs that source check and the disposable real-server matrix.
`make interoperability` additionally exercises the supported rolling-upgrade
path. These gates fail closed when Docker, images, source archives, credentials,
or expected results are unavailable.

The real-server matrix covers typed search, pagination, external-version
writes, backend delete-tombstone expiry and guarded stale-replay rejection,
bulk attribution, lifecycle operations, semantic migration verification,
deployment-owned snapshot restore, failure handling, and the shared search
contract. Unit and fuzz evidence covers hostile inputs and failure paths that
are impractical to force through a live cluster.

## Claim boundary

Conformance does not imply equivalent ranking, analyzers, mappings, plugins,
managed-service extensions, or behavior on unlisted patch versions. Every
additional server, plugin, API, or profile requires a source pin, a decision
record, executable evidence, and compatibility review before it enters the
supported matrix.
