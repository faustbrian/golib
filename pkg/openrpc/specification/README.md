# Authoritative specification inputs

OpenRPC 1.4.1 is the authoritative stable specification for this module. The
release is pinned to its immutable Git commit and each copied input is verified
by SHA-256 in `manifest.json`. The upstream changelog tracks published errata,
including the 1.4.1 version-pattern fix. The Apache 2.0 license is copied beside
the inputs.

The official example corpus is pinned independently to its immutable repository
commit and copied with the same checksum policy. Some examples predate the 1.4
release; they are interoperability fixtures, not evidence of 1.4 conformance by
themselves.

Run `scripts/sync-spec.sh` from this module directory to reproduce the local
copies. The command performs explicit network access; package parsing and
validation never invoke it.

The 1.4 schema accepts the `1.4.x` compatibility line. This implementation will
reject other minor or major lines until their semantics are separately
inventoried and tested. JSON Schema values use Draft 7, as required by the
OpenRPC specification.

The published OpenRPC meta-schema references `https://meta.json-schema.tools/`
as its companion Draft 7 schema dialect. That response is pinned, normalized
with `jq -S`, checksummed in the manifest, and embedded for offline validation.
The validator rewrites the companion dialect declaration to the canonical
Draft 7 URI before compilation. It also removes the Server Object `url` format
assertion because that generic URI check contradicts the normative support for
relative URLs and server-variable templates; dedicated semantic validation
still validates those forms. No validation path fetches the live URL.

The normative and object-field matrices are generated from these pinned inputs
and then reviewed against prose requirements that JSON Schema cannot express.
