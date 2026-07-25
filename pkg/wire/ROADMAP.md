# Roadmap

The roadmap protects scope as much as it describes future work. Items are not
promises or compatibility commitments until released.

## v1.0: production wire-format foundation

- stabilize the current explicit package layout;
- validate APIs against multiple real, redacted vendor fixture families;
- maintain meaningful 100% production-code coverage;
- exercise fuzz targets over longer scheduled runs;
- publish benchmark baselines without promising fixed performance numbers;
- complete external review of error and normalization contracts;
- release only after the documented support matrices match verified behavior.

The v1 surface includes explicit JSON, XML, SOAP, YAML, TOML, MessagePack,
CBOR, and BSON packages. Each format keeps its own semantics, fixtures, fuzz
targets, benchmarks, and compatibility contract.

## After core stabilization

Potential work, ordered by demonstrated user need:

- additional XML charsets through optional adapters;
- WSDL integration guidance or a separate adapter package;
- helpers for common transport patterns that do not turn the core into an HTTP
  client framework;
- schema-generation guidance where output can remain explicit and auditable.

## Further formats

Any additional format proposal must show repeated production demand,
format-specific semantics that belong here, a dedicated API that preserves
existing package boundaries, and verification and documentation equal to the
current formats.

## Explicit non-goals

- generic all-codec interfaces;
- HTTP clients, retries, authentication, or service discovery;
- generic RPC frameworks;
- queues, persistence, or business mapping;
- hidden schema inference or normalization;
- WSDL client generation inside the core runtime package.
