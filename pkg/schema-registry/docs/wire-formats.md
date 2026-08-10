# Wire formats

Wire framing is versioned independently from schema content and codecs.

Confluent classic Avro and JSON frames are magic byte `0`, a four-byte
big-endian schema ID, then payload. Confluent Protobuf adds a zig-zag varint
message-index vector after the ID; the common `[0]` vector is encoded as one
zero byte. Select `ClassicFramer` or `ProtobufFramer` explicitly. The newer
Confluent GUID header is not represented by these version-0 framers.

AWS Glue frames use header version byte `3`, compression byte, a 16-byte schema
version UUID, then payload. `UncompressedFramer` supports compression byte `0`
and explicitly rejects ZLIB byte `5`; applications requiring compression must
select a separate implementation.

All framers validate provider scope, identifiers, payload limits, truncation,
and cancellation before allocation. `CodecIntegration.Parse` returns identity
and bytes only; schema resolution is a separate caller action before `Decode`.
