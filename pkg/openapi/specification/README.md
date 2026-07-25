# Authoritative OpenAPI inputs

This directory contains byte-identical, checksum-pinned inputs used to build
and test the package. `manifest.json` records the repository revisions,
source paths, destinations, and SHA-256 checksums.
Each artifact group also records an SPDX license expression, an authoritative
license source, and either an immutable repository revision or retrieval date.

The IANA HTTP Status Code, HTTP Authentication Scheme, and IPv4 and IPv6
Special-Purpose Address registries are pinned as the exact CSV snapshots used
by validation and resolver security tests, with their retrieval date and
checksums recorded in the manifest.

Run `./scripts/sync-spec.sh` to reproduce or verify the checked-in copies.
The command performs network access only when explicitly invoked.

The Markdown specifications are normative. Published schemas and registries
are supporting evidence and never override the applicable specification text.
The 3.0 and 3.1 directories include every published patch document supported
by the package because patch releases can contain behaviorally relevant
clarifications even when the feature set is shared.

Accepted upstream errata are pinned separately in `manifest.json` and explained
in [`../docs/specification-decisions.md`](../docs/specification-decisions.md).
This keeps released source artifacts byte-identical while making intentional
post-publication corrections auditable.

`independent/` contains checksum-pinned, license-preserved public descriptions
used only as interoperability and scale evidence. Their popularity and current
behavior are not normative authority, and validation findings are classified
in `docs/interoperability.md` rather than patched out of source files.

OpenAPI Overlay and Arazzo artifacts are intentionally absent because they are
separate specifications and are not part of this package's compliance claim.
