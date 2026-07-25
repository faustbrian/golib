package wire

import "bytes"

// Format identifies a supported structured wire format.
type Format string

const (
	FormatJSON Format = "json"
	FormatXML  Format = "xml"
	FormatSOAP Format = "soap"
	FormatYAML Format = "yaml"
	FormatTOML Format = "toml"
	// FormatMessagePack names the MessagePack binary format.
	FormatMessagePack Format = "msgpack"
	FormatCBOR        Format = "cbor"
	FormatBSON        Format = "bson"
)

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// DetectFormat distinguishes JSON from XML using the first significant byte.
// SOAP is returned as XML because reliably identifying a SOAP envelope requires
// parsing its namespace; use the soap package when SOAP is expected.
func DetectFormat(payload []byte) (Format, error) {
	payload = bytes.TrimSpace(payload)
	payload = bytes.TrimPrefix(payload, utf8BOM)
	payload = bytes.TrimSpace(payload)

	if len(payload) == 0 {
		return "", unsupportedError("detect format", payload)
	}

	switch payload[0] {
	case '{', '[':
		return FormatJSON, nil
	case '<':
		return FormatXML, nil
	default:
		return "", unsupportedError("detect format", payload)
	}
}
