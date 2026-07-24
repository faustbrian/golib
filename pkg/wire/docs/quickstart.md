# Quickstart

## Install

The project is currently unreleased. Until the first tag exists, pin a commit:

```sh
go get github.com/faustbrian/golib/pkg/wire@<commit>
```

The module supports Go 1.25.8 and newer.

## Decode JSON

```go
var response struct {
	Status string `json:"status"`
}

err := jsonwire.DecodeReader(responseBody, &response, jsonwire.DecodeOptions{
	MaxBytes:              2 << 20,
	DisallowUnknownFields: true,
})
```

`MaxBytes: 0` selects the safe 1 MiB default. Decoding accepts exactly one JSON
value. Syntax errors classify as `wire.ErrParse`; unknown fields, type
mismatches, and invalid decoded values classify as `wire.ErrValidation`.
Invalid targets use `wire.ErrTarget`; size failures use `wire.ErrSizeLimit`.

## Encode JSON

```go
payload, err := jsonwire.Encode(response, jsonwire.EncodeOptions{})

err = jsonwire.EncodeWriter(destination, response, jsonwire.EncodeOptions{
	MaxBytes: 256 << 10,
	Indent: "  ",
})
```

`Encode` returns a complete JSON document as bytes. `EncodeWriter` writes the
same deterministic document without adding a trailing newline. Every format's
encode options use `MaxBytes: 0` for the safe 1 MiB output default; exceeding
the bound returns `wire.ErrSizeLimit` before a writer receives any bytes.

## Normalize JSON

```go
canonical, err := jsonwire.Normalize(vendorPayload, jsonwire.NormalizeOptions{})
```

Normalization strips leading/trailing whitespace and a leading UTF-8 BOM,
validates one JSON value, orders object keys through `encoding/json`, compacts
the document, and preserves number lexemes such as `1.20`. It does not reject
duplicate object keys.

## Decode namespace-aware XML

```go
var shipment struct {
	XMLName xml.Name `xml:"urn:vendor Shipment"`
	ID      int      `xml:"urn:vendor ID"`
}

err := xmlwire.Decode(payload, &shipment, xmlwire.DecodeOptions{
	MaxDepth:    64,
	ExpectedRoot: xml.Name{Space: "urn:vendor", Local: "Shipment"},
})
```

Strict XML is the default. Set `AllowNonStrict` only for a known peer whose
malformation is intentionally accepted. Prefixes are not identities;
`ExpectedRoot` compares the resolved namespace URL and local name.

## Encode XML

```go
payload, err := xmlwire.Encode(shipment, xmlwire.EncodeOptions{
	IncludeHeader: true,
})

err = xmlwire.EncodeWriter(destination, shipment, xmlwire.EncodeOptions{})
```

Both APIs serialize typed values through `encoding/xml`. The writer receives a
complete document, including the XML declaration when requested.

## Encode SOAP

```go
payload, err := soap.Encode(
	soap.Version12,
	header,
	request,
	soap.EncodeOptions{MaxBytes: 256 << 10},
)

err = soap.EncodeWriter(
	destination,
	soap.Version12,
	header,
	request,
	soap.EncodeOptions{MaxBytes: 256 << 10},
)
```

Pass `nil` for an omitted Header or an empty Body. For already encoded XML
fragments, use `Marshal` or `MarshalWriter`. `MarshalFault` and
`MarshalFaultWriter` provide the corresponding SOAP fault output paths. Use
the corresponding `WithOptions` APIs with `soap.MarshalOptions` to configure
raw-envelope and fault output limits.

## Parse SOAP

```go
envelope, err := soap.ParseReader(responseBody, soap.ParseOptions{})
if errors.Is(err, wire.ErrSOAPFault) {
	var faultError *soap.FaultError
	if errors.As(err, &faultError) {
		return fmt.Errorf("carrier %s: %s", faultError.Fault.Code, faultError.Fault.Reason)
	}
}
if err != nil {
	return err
}

var response RateResponse
if err := envelope.DecodeBody(&response); err != nil {
	return err
}
```

`Parse` and `ParseReader` return a non-nil envelope together with a
`*soap.FaultError` for a valid SOAP fault. Do not discard the envelope before
checking the error classification.

## Classify errors

```go
switch {
case errors.Is(err, wire.ErrSOAPFault):
	// The peer returned a valid fault response.
case errors.Is(err, wire.ErrEnvelope):
	// SOAP transport-envelope structure was invalid.
case errors.Is(err, wire.ErrParse):
	// Input was not valid syntax for the selected format.
case errors.Is(err, wire.ErrValidation):
	// Input syntax was valid but did not meet the requested shape or policy.
case errors.Is(err, wire.ErrUnsupportedFormat):
	// Explicit detection did not recognize JSON or XML.
case errors.Is(err, wire.ErrWrite):
	// Serialization succeeded but the destination rejected the output.
}
```

## Read and write YAML

```go
var config struct {
	Service string `yaml:"service"`
}
err := yamlwire.DecodeReader(source, &config, yamlwire.DecodeOptions{
	DisallowUnknownFields: true,
	MaxAliases:            32,
	MaxDepth:              64,
})
err = yamlwire.EncodeWriter(destination, config, yamlwire.EncodeOptions{})
```

One document and unique keys are the defaults. Set
`AllowMultipleDocuments` only with a pointer to a slice. Aliases and merge keys
can be rejected independently.

## Read and write TOML

```go
var config struct {
	DeployedAt time.Time `toml:"deployed_at"`
}
err := tomlwire.DecodeReader(source, &config, tomlwire.DecodeOptions{
	DisallowUnknownFields: true,
})
err = tomlwire.EncodeWriter(destination, config, tomlwire.EncodeOptions{})
```

Duplicate keys, malformed trailing text, datetime mismatches, and numeric
overflow are rejected.

## Read and write MessagePack

```go
var message struct {
	ID uint64 `msgpack:"id"`
}
err := msgpackwire.DecodeReader(source, &message, msgpackwire.DecodeOptions{})
err = msgpackwire.EncodeWriter(destination, message, msgpackwire.EncodeOptions{
	CompactIntegers: true,
})
```

The decoder accepts one object. Untyped maps require string keys; typed map
targets may declare another comparable key type.

## Read and write CBOR

```go
var message struct {
	ID uint64 `cbor:"id"`
}
err := cborwire.DecodeReader(source, &message, cborwire.DecodeOptions{
	MaxNestedLevels: 64,
})
err = cborwire.EncodeWriter(destination, message, cborwire.EncodeOptions{
	Profile: cborwire.CoreDeterministic,
})
```

Canonical encoding is the default. Tags and indefinite-length values are
rejected unless explicitly enabled.

## Read and write BSON

```go
document := bsonwire.D{{Key: "status", Value: "ok"}}
err := bsonwire.EncodeWriter(destination, document, bsonwire.EncodeOptions{})

var decoded bsonwire.M
err = bsonwire.DecodeReader(source, &decoded, bsonwire.DecodeOptions{})
```

BSON APIs require a complete document. Use `D`, structs, or `Raw` for stable
field order; `M` map order is intentionally unspecified.

Use `errors.As(err, &wireError)` to inspect `Kind`, `Format`, `Op`, and the
underlying cause.
