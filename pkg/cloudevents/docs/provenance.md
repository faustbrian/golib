# Normative source provenance

The [specification decision register](specification-decisions.md) maps these
pinned inputs to the package's observable interpretations and executable
evidence.

The following SHA-256 digests bind the reviewed files at
`cloudevents/spec@fc1f6f31f5f011a72183f1bcea20c987cb683ade`:

| File | SHA-256 |
| --- | --- |
| `cloudevents/spec.md` | `e327435c858d19fd171e4ab9781a01fc22dfa949d23c4220976529ebd16a1aa3` |
| `cloudevents/formats/json-format.md` | `30778f995a2c82f3ac6c4f7cd56415d675111990151c9ab67bd0a2de1d0e1ce5` |
| `cloudevents/formats/cloudevents.json` | `e28a6d252d7b7238d176618f6bbf6cde570b26a867bc5241563aed34c9dd1d83` |
| `cloudevents/formats/avro-format.md` | `31b9804cb27e2278925662c14619859c59ed6367573068a62f95b5c0bf598af3` |
| `cloudevents/formats/cloudevents.avsc` | `e00355124197530cdc2ab9b506890f2157cfa1bba4112e7d9c06e83d424d0ab6` |
| `cloudevents/formats/protobuf-format.md` | `40613c262319a0fe7942120f2a1fe86632f3a3271064fb3c6862c91625850877` |
| `cloudevents/formats/cloudevents.proto` | `2bc1e82754cc8b7abb08fa8329e50a7643f6da18f6d60479ecbe403ae6e5fecc` |
| `cloudevents/bindings/http-protocol-binding.md` | `35b0bc7949dd95e1134867c0f3854d97f7d7f3adcfe7a9b378705489ccc65e83` |
| `cloudevents/bindings/kafka-protocol-binding.md` | `2935a0d3c01f61b66580a15cd2a7ebc7663919bfe0a9e97b7f0428cbe9fa2d8c` |
| `cloudevents/bindings/amqp-protocol-binding.md` | `e1a7974f25f813c85656426ccdaf35c4545fc8bc70ec605d3c23c31569ef8029` |
| `cloudevents/bindings/mqtt-protocol-binding.md` | `fec5b15a6fbb2019393aa970e5bed466e3bd83832c6fc87813d2dd042e177c9d` |
| `cloudevents/bindings/nats-protocol-binding.md` | `42b82b99acf0b3e90e069cb640c805e19c3ea83d3605292def487c5ad1d48961` |
| `cloudevents/bindings/websockets-protocol-binding.md` | `b401503e43f07f8ae45cf4aaf3faeb180d1191653d5e6742e741a2d7ae8109a9` |
| `cloudevents/extensions/distributed-tracing.md` | `bf6cef7bc1a72c59eee126f5f5a3047f58d9961899fbfa9625c3ade3f28980a5` |
| `cloudevents/extensions/partitioning.md` | `38b5bf2b9e46a52917710f5ca2bba3cc3b5d91649f7a463293b6cd67634fa94e` |
| `cloudevents/extensions/dataref.md` | `bc5533cd155115f3bf96cece126d7e2d2d7e675bc11aaadc643d12e55163548b` |
| `cloudevents/extensions/sampledrate.md` | `81df6bed82ab43a6818c573382025db8fd6578d1601141ef824e28cd0836f55c` |
| `cloudevents/extensions/sequence.md` | `3813806343d2f35d3cef5a355e9d2840d2e7d041cd8e4fdd330a1f48b2836f12` |
| `cloudevents/documented-extensions.md` | `349f5ce2eaac0415cb9ba8e92ca59813831ccf9638f79560e460b372d6b88c90` |

Pinning a document does not claim support. The complete supported and
unsupported inventory is in the [specification matrix](specification-matrix.md).

Official conformance feature digests at
`cloudevents/conformance@7a8ee0ac0e782bba1ba30e58c62d24d2e6c337e5`:

| File | SHA-256 |
| --- | --- |
| `features/http-protocol-binding.feature` | `d24dc8f28cb529b77377216a4f2015dbb93644980a09dfd8f3bdd1705a23c40c` |
| `features/kafka-protocol-binding.feature` | `a3e6f08bde13e4b219f13036ef4da168f2e797e525c05f244c80767939251865` |
| `README.md` | `2fa1a478509c4b22bd8e5f8160317485df81a5d3c0760e977755250ea6e5cd54` |

The two feature files are vendored under `testdata/conformance`. The vendored
copies differ only by an added final newline; their normalized digests are
recorded in `specification/manifest.json`. `make conformance` validates the
manifest, both vendored hashes, and the JavaScript lockfile hash before the
transcribed scenarios are exercised. Update fixtures from the pinned raw GitHub
paths, review the diff, recompute both source and vendored SHA-256 digests, and
rerun `make conformance`.

Independent interoperability is pinned separately to `cloudevents/sdk-go/v2`
v2.16.2 at `af3e8599b3316ab6b4b73ff69aa8ec0efddbb5bb`, the independently tagged
`cloudevents/sdk-go/protocol/kafka_sarama/v2` v2.16.2 module at
`bee0ebe38fde4cecb92dc51aab7acddc951cbd70`, and
`cloudevents/sdk-javascript` v10.0.0 at
`c23895145a2f055e8e90714401fc67252a0edf21`. The JavaScript gate runs on
Node.js 24.19.0, installs from the checked lockfile with lifecycle scripts
disabled, and uses a disposable npm cache. When the exact runtime is not
installed, the gate downloads the pinned platform archive into its temporary
directory and verifies its manifest checksum before extraction. Selected JSON,
batch, HTTP, Kafka, tracing, partitioning, and opaque-extension fixtures flow in
both implemented directions. The JavaScript SDK's binary Kafka output is kept
as an explicit incompatibility because it redundantly emits
`ce_datacontenttype`; Golib does not normalize that conflict away. Exact
consumer assertions cover every declared context field, selected and opaque
extensions, and semantic payload. The gate also records the SDK HTTP helper's
timestamp, null, empty-data, and default-content-type normalizations explicitly
rather than treating an ID-only round trip as payload-fidelity evidence. Exact
directions, normalizations, and omissions are in the
[conformance report](conformance-report.md); package and lockfile integrity
values are in `specification/manifest.json`.

`testdata/interoperability/javascript-edge-batch.json` is generated from fixed
contexts by the pinned JavaScript SDK through
`scripts/javascript-interop.cjs`; the gate regenerates the same objects and
requires exact serialized equality. Its SHA-256 digest is
`622bba22412b35d695b0517515dcefcdd3770953b347d9d7616e5c7cfdf51163`.
The fixture covers absent data, explicit JSON null, empty text, empty binary,
and parameterized JSON. Update it only by changing that generator with the SDK
or runtime pin, reviewing the behavioral difference, recomputing this digest,
and rerunning the interoperability gate.

Upstream material is Apache-2.0 licensed. Any copied fixture must retain its
source path, revision, digest, license, generation or extraction command, and
update procedure alongside the fixture.
