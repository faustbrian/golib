# Security

## Trust Boundary

Treat all JSON, XML, SOAP, YAML, TOML, MessagePack, CBOR, and BSON input as
untrusted. Choose explicit decode limits and reject unsupported data shapes
before application processing.

## Format Risks

XML and SOAP require entity and nesting discipline. YAML aliases and recursive
structures require limits. Binary formats require byte, depth, collection, and
allocation bounds. Never assume equivalent semantics across formats.

## Application Responsibilities

Apply transport body limits, deadlines, authentication, authorization, and
rate limits before decoding. Do not expose raw parser errors when they may
contain sensitive input.

See [dependencies](dependencies.md), [formats](formats.md), and
[hardening](hardening.md) for the maintained boundary evidence. Report
vulnerabilities through [SECURITY.md](../SECURITY.md).
