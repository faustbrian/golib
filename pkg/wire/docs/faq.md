# Frequently asked questions

## Why not call `encoding/json` or `encoding/xml` directly?

Do so when their defaults already satisfy the boundary. `wire` adds value
when a service needs consistent bounds, single-document enforcement, structured
errors, explicit normalization, namespace validation, charset policy, or SOAP
semantics. It is not a replacement wrapper for every standard-library call.

## Why separate packages instead of one `Codec` interface?

The formats have different failure modes and guarantees. A universal interface
would hide namespace, fault, alias, datetime, map-key, tag, canonicalization,
extension, and document semantics. Explicit imports make those choices visible.

## Does format detection identify SOAP?

No. `wire.DetectFormat` returns `wire.FormatXML` for XML-looking bytes. Use
`soap.Parse` to validate an Envelope namespace and determine SOAP 1.1 or 1.2.

## Is JSON normalization canonical JSON?

No. It provides deterministic key ordering through `encoding/json`, compact
whitespace, BOM removal, and number-lexeme preservation. It does not implement
RFC 8785 or another signing canonicalization standard.

## Are duplicate JSON keys rejected?

No. They follow `encoding/json` behavior. Validate duplicates separately if a
security or protocol contract requires rejection.

## Does XML parsing resolve external entities?

No network or filesystem entity resolver is installed. The package does not
provide DTD or XSD processing.

## Why is non-strict XML opt-in?

Recovery changes interpretation and can hide upstream breakage. The option
exists for known, fixture-backed vendor defects and is never inferred.

## Why only four built-in charsets?

They cover common vendor boundaries while keeping the dependency and audit
surface small. Inject a charset reader for a known additional encoding.

## Does SOAP parsing depend on HTTP headers?

No. The envelope namespace determines the version. HTTP status, Content-Type,
SOAPAction, authentication, retries, and timeouts belong to the caller.

## Why does parsing a SOAP fault return an envelope and an error?

A fault is valid SOAP but represents a failed remote operation. Returning both
preserves raw diagnostics and typed fault data while making error handling hard
to skip accidentally.

## Is `Fault.Detail` trusted or schema-validated?

No. It is raw inner XML from the fault detail element. Decode it into a
peer-specific DTO only after identifying the expected fault code and schema.

## Can SOAP encode headers and bodies from structs?

Yes. Use `soap.Encode` for bytes or `soap.EncodeWriter` for an `io.Writer`; both
accept typed Header and Body values. Use `Marshal` or `MarshalWriter` when the
application already owns XML fragments and needs them preserved byte-for-byte.

## Is the package safe for concurrent use?

Functions allocate per call and do not use mutable package state. An `Envelope`
is safe for concurrent reads if the caller does not mutate exported fields such
as `Fault`. Raw accessors return copies.

## What is stable before v1?

Nothing is covered by a v1 compatibility promise until v1.0.0 is tagged. The
current docs describe the candidate contract so changes can be reviewed
explicitly.

## Why are YAML, TOML, and binary formats not auto-detected?

Ordinary text can be valid under multiple grammars, and arbitrary bytes can
look like valid MessagePack, CBOR, or a BSON prefix by accident. Call the
package named by the protocol contract. `DetectFormat` remains limited to its
documented JSON/XML heuristic.

## Are YAML aliases and merge keys safe?

They are bounded by YAML v4's alias and depth protections. Callers can set
tighter limits or reject aliases and merge keys entirely. Duplicate keys and
multiple documents are rejected by default.

## Is all CBOR output canonical?

No. The default is the named canonical profile. Core Deterministic and CTAP2
are separate explicit profiles with different rules. Tags also require
explicit opt-in.

## Why can BSON map output change order?

`bsonwire.M` is a Go map. Use a struct, ordered `bsonwire.D`, or validated
`bsonwire.Raw` when stable field order or exact bytes matter.
