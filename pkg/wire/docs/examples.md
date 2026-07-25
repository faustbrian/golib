# End-to-end examples

These examples keep transport and domain policy outside `wire`. Replace the
placeholder URLs and DTOs with the peer's documented contract.

## JSON HTTP response

```go
type RateResponse struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func fetchRate(ctx context.Context, client *http.Client, endpoint string) (RateResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RateResponse{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return RateResponse{}, err
	}
	defer response.Body.Close()

	var rate RateResponse
	if err := jsonwire.DecodeReader(response.Body, &rate, jsonwire.DecodeOptions{
		MaxBytes: 256 << 10,
	}); err != nil {
		return RateResponse{}, fmt.Errorf("decode rate response: %w", err)
	}
	return rate, nil
}
```

HTTP status validation remains the caller's responsibility and should normally
happen before body decoding. A service may intentionally decode a structured
error body for non-2xx statuses.

## XML webhook with namespace routing

```go
func handleShipment(body io.Reader) error {
	payload, err := io.ReadAll(io.LimitReader(body, (512<<10)+1))
	if err != nil {
		return err
	}
	if len(payload) > 512<<10 {
		return errors.New("shipment webhook is too large")
	}

	root, err := xmlwire.Root(payload, xmlwire.DecodeOptions{})
	if err != nil {
		return err
	}
	if root != (xml.Name{Space: "urn:carrier:shipment:v2", Local: "Shipment"}) {
		return fmt.Errorf("unexpected shipment document {%s}%s", root.Space, root.Local)
	}

	var shipment ShipmentV2
	return xmlwire.Decode(payload, &shipment, xmlwire.DecodeOptions{
		MaxBytes:     512 << 10,
		ExpectedRoot: root,
	})
}
```

When a caller needs both `Root` and `Decode`, it owns the bounded byte slice so
the stream is read once. `Root` validates the complete document, not only the
opening tag.

## SOAP request and response

```go
func callRateService(ctx context.Context, client *http.Client, endpoint string, request RateRequest) (RateResponse, error) {
	envelopeXML, err := soap.Encode(
		soap.Version12,
		nil,
		request,
		soap.EncodeOptions{},
	)
	if err != nil {
		return RateResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(envelopeXML))
	if err != nil {
		return RateResponse{}, err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	response, err := client.Do(req)
	if err != nil {
		return RateResponse{}, err
	}
	defer response.Body.Close()

	envelope, parseErr := soap.ParseReader(response.Body, soap.ParseOptions{MaxBytes: 2 << 20})
	if errors.Is(parseErr, wire.ErrSOAPFault) {
		var faultError *soap.FaultError
		if errors.As(parseErr, &faultError) {
			return RateResponse{}, mapCarrierFault(faultError.Fault)
		}
	}
	if parseErr != nil {
		return RateResponse{}, parseErr
	}

	var rate RateResponse
	if err := envelope.DecodeBody(&rate); err != nil {
		return RateResponse{}, err
	}
	return rate, nil
}
```

The application chooses HTTP headers, authenticates, maps statuses, and decides
whether a fault is retryable. `wire` owns only XML/SOAP structure.

## Safe diagnostic logging

```go
func logWireError(logger *slog.Logger, peer string, err error) {
	var wireError *wire.Error
	if !errors.As(err, &wireError) {
		logger.Error("integration failed", "peer", peer, "error", err)
		return
	}
	logger.Error("wire payload rejected",
		"peer", peer,
		"format", wireError.Format,
		"kind", wireError.Kind,
		"operation", wireError.Op,
		"cause", wireError.Err,
	)
}
```

Do not log `RawXML`, `BodyXML`, or vendor payloads unless the application has a
redaction and retention policy.

## YAML configuration round trip

```go
var config ServiceConfig
if err := yamlwire.DecodeReader(input, &config, yamlwire.DecodeOptions{
	DisallowUnknownFields: true,
	MaxAliases:            32,
	MaxDepth:              64,
}); err != nil {
	return err
}
return yamlwire.EncodeWriter(output, config, yamlwire.EncodeOptions{})
```

Use `AllowMultipleDocuments` only with `*[]ServiceConfig`. Duplicate keys are
rejected unless the integration explicitly chooses last-key-wins behavior.

## TOML configuration round trip

```go
var config ServiceConfig
if err := tomlwire.DecodeReader(input, &config, tomlwire.DecodeOptions{
	DisallowUnknownFields: true,
}); err != nil {
	return err
}
return tomlwire.EncodeWriter(output, config, tomlwire.EncodeOptions{})
```

TOML datetime fields should use `time.Time` or underlying TOML local-time
types. Numeric overflow is rejected rather than narrowed.

## MessagePack request round trip

```go
var request Request
if err := msgpackwire.DecodeReader(input, &request, msgpackwire.DecodeOptions{}); err != nil {
	return err
}
return msgpackwire.EncodeWriter(output, request, msgpackwire.EncodeOptions{
	CompactIntegers: true,
})
```

The input must contain one object. Unknown extensions and non-string keys in
untyped maps are rejected; use a typed map target when integer keys are part of
the protocol.

## Deterministic CBOR exchange

```go
var request Request
if err := cborwire.DecodeReader(input, &request, cborwire.DecodeOptions{
	MaxNestedLevels: 64,
	MaxArrayElements: 4096,
	MaxMapPairs:      4096,
}); err != nil {
	return err
}
return cborwire.EncodeWriter(output, request, cborwire.EncodeOptions{
	Profile: cborwire.CoreDeterministic,
})
```

Tags and indefinite-length items stay disabled unless the peer's CBOR profile
requires them.

## Ordered BSON document exchange

```go
request := bsonwire.D{
	{Key: "shipment_id", Value: shipmentID},
	{Key: "created_at", Value: createdAt},
}
if err := bsonwire.EncodeWriter(output, request, bsonwire.EncodeOptions{}); err != nil {
	return err
}

var response bsonwire.Raw
return bsonwire.DecodeReader(input, &response, bsonwire.DecodeOptions{})
```

Use `D`, structs, or `Raw` when byte order matters. `M` is a Go map and does
not have stable output order.
