# Wire formats

Wire framing is versioned independently from schema content and codecs.

Confluent classic Avro and JSON frames are magic byte `0`, a four-byte
big-endian schema ID, then payload. Confluent Protobuf adds a zig-zag varint
message-index vector after the ID; the common `[0]` vector is encoded as one
zero byte. Select `ClassicFramer` or `ProtobufFramer` explicitly. The newer
Confluent GUID header version 1 is not represented by these version-0 framers
and is therefore explicitly unsupported. Header-version support is a protocol
choice, not something inferred from payload bytes.

AWS Glue frames use header version byte `3`, compression byte, a 16-byte schema
version UUID, then payload. `UncompressedFramer` supports compression byte `0`
and explicitly rejects ZLIB byte `5`; applications requiring compression must
select a separate implementation.

All framers validate provider scope, identifiers, payload limits, truncation,
and cancellation before allocation. `CodecIntegration.Parse` returns identity
and bytes only; schema resolution is a separate caller action before `Decode`.

The JSON Schema adapter owns bounded JSON value encoding and decoding. Its
Confluent integration fixture compares the encoded payload with Go's JSON
implementation and the complete registered-ID frame with franz-go. The Avro
and Protobuf format adapters canonicalize schemas but deliberately do not own
business-value codecs; callers compose an explicitly selected codec through
`CodecIntegration`. Framing or canonicalization evidence does not imply an
Avro or Protobuf payload-codec claim.
