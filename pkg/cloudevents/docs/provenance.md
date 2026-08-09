# Normative source provenance

The following SHA-256 digests bind the reviewed files at
`cloudevents/spec@fc1f6f31f5f011a72183f1bcea20c987cb683ade`:

| File | SHA-256 |
| --- | --- |
| `cloudevents/spec.md` | `e327435c858d19fd171e4ab9781a01fc22dfa949d23c4220976529ebd16a1aa3` |
| `cloudevents/formats/json-format.md` | `30778f995a2c82f3ac6c4f7cd56415d675111990151c9ab67bd0a2de1d0e1ce5` |
| `cloudevents/formats/cloudevents.json` | `e28a6d252d7b7238d176618f6bbf6cde570b26a867bc5241563aed34c9dd1d83` |
| `cloudevents/bindings/http-protocol-binding.md` | `35b0bc7949dd95e1134867c0f3854d97f7d7f3adcfe7a9b378705489ccc65e83` |
| `cloudevents/bindings/kafka-protocol-binding.md` | `2935a0d3c01f61b66580a15cd2a7ebc7663919bfe0a9e97b7f0428cbe9fa2d8c` |
| `cloudevents/extensions/distributed-tracing.md` | `bf6cef7bc1a72c59eee126f5f5a3047f58d9961899fbfa9625c3ade3f28980a5` |
| `cloudevents/extensions/partitioning.md` | `38b5bf2b9e46a52917710f5ca2bba3cc3b5d91649f7a463293b6cd67634fa94e` |
| `cloudevents/documented-extensions.md` | `349f5ce2eaac0415cb9ba8e92ca59813831ccf9638f79560e460b372d6b88c90` |

Official conformance feature digests at
`cloudevents/conformance@7a8ee0ac0e782bba1ba30e58c62d24d2e6c337e5`:

| File | SHA-256 |
| --- | --- |
| `features/http-protocol-binding.feature` | `d24dc8f28cb529b77377216a4f2015dbb93644980a09dfd8f3bdd1705a23c40c` |
| `features/kafka-protocol-binding.feature` | `a3e6f08bde13e4b219f13036ef4da168f2e797e525c05f244c80767939251865` |
| `README.md` | `2fa1a478509c4b22bd8e5f8160317485df81a5d3c0760e977755250ea6e5cd54` |

The two feature files are vendored under `testdata/conformance`. The vendored
copies differ only by an added final newline; their normalized digests are
recorded in `specification/manifest.json` and checked before their scenarios
are exercised. Update them from the pinned raw GitHub paths, review the diff,
recompute both source and vendored SHA-256 digests, and rerun `make conformance`.

Independent interoperability is pinned to `cloudevents/sdk-go` v2.16.2 at
`af3e8599b3316ab6b4b73ff69aa8ec0efddbb5bb` and
`cloudevents/sdk-javascript` v10.0.0 at
`c23895145a2f055e8e90714401fc67252a0edf21`. The JavaScript gate runs on
Node.js 24.13.0, installs from the checked lockfile with lifecycle scripts
disabled, uses a disposable npm cache, and validates JSON, HTTP, Kafka, batch,
unknown-extension, distributed-tracing, and partition-key fixtures in both
directions. Exact package and lockfile integrity values are in
`specification/manifest.json`.

Upstream material is Apache-2.0 licensed. Any copied fixture must retain its
source path, revision, digest, license, generation or extraction command, and
update procedure alongside the fixture.
