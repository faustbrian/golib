package measurement

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/math/decimal"
)

// MaxSerializedBytes bounds direct JSON and SQL decoding. Use measurementwire
// for streaming codecs and caller-selected tighter limits.
const MaxSerializedBytes = 64 << 10

type encodedQuantity struct {
	Value string `json:"value" xml:"value"`
	Unit  Unit   `json:"unit" xml:"unit"`
}

type encodedDimensions struct {
	Length   Quantity `json:"length" xml:"length"`
	Width    Quantity `json:"width" xml:"width"`
	Height   Quantity `json:"height" xml:"height"`
	Quantity uint64   `json:"quantity" xml:"quantity"`
}

// MarshalJSON encodes value and unit metadata with the decimal as a string.
func (q Quantity) MarshalJSON() ([]byte, error) {
	if _, err := definitionFor(q.unit); err != nil {
		return nil, err
	}

	return json.Marshal(encodedQuantity{Value: q.amount.String(), Unit: q.unit})
}

// UnmarshalJSON decodes one bounded strict quantity object.
func (q *Quantity) UnmarshalJSON(data []byte) error {
	if q == nil {
		return ErrInvalidQuantity
	}
	if len(data) > MaxSerializedBytes {
		return fmt.Errorf("%w: JSON exceeds %d bytes", ErrInvalidQuantity, MaxSerializedBytes)
	}
	var encoded encodedQuantity
	if err := decodeJSONObject(data, map[string]struct{}{"value": {}, "unit": {}}, &encoded); err != nil {
		return err
	}

	return q.decode(encoded)
}

// MarshalXML encodes value and unit metadata without numeric narrowing.
func (q Quantity) MarshalXML(encoder *xml.Encoder, start xml.StartElement) error {
	if _, err := definitionFor(q.unit); err != nil {
		return err
	}
	if start.Name.Local == "" || start.Name.Local == "Quantity" {
		start.Name.Local = "quantity"
	}

	return encoder.EncodeElement(encodedQuantity{Value: q.amount.String(), Unit: q.unit}, start)
}

// UnmarshalXML decodes and validates value and unit metadata.
func (q *Quantity) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	if q == nil {
		return ErrInvalidQuantity
	}
	encoded, err := decodeQuantityXML(decoder, start)
	if err != nil {
		return err
	}

	return q.decode(encoded)
}

// MarshalText returns canonical amount-space-symbol text.
func (q Quantity) MarshalText() ([]byte, error) {
	if _, err := definitionFor(q.unit); err != nil {
		return nil, err
	}

	return []byte(q.String()), nil
}

// UnmarshalText parses canonical text with SymbolProfile.
func (q *Quantity) UnmarshalText(text []byte) error {
	if q == nil {
		return ErrInvalidQuantity
	}
	parsed, err := Parse(string(text), SymbolProfile())
	if err != nil {
		return err
	}
	*q = parsed

	return nil
}

// Value stores a quantity as lossless JSON text suitable for SQL text or JSON
// columns.
func (q Quantity) Value() (driver.Value, error) {
	data, err := q.MarshalJSON()
	if err != nil {
		return nil, err
	}

	return string(data), nil
}

// Scan reads lossless JSON text from a SQL driver.
func (q *Quantity) Scan(source any) error {
	var data []byte
	switch value := source.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = append([]byte(nil), value...)
	default:
		return fmt.Errorf("%w: unsupported SQL value %T", ErrInvalidQuantity, source)
	}

	return q.UnmarshalJSON(data)
}

// FormatOptions contains every conversion and display decision. No locale or
// preferred unit is inferred.
type FormatOptions struct {
	Unit       Unit
	Conversion ConversionContext
	Scale      int32
	Rounding   decimal.RoundingMode
	Separator  string
}

// Format converts, rounds, and formats a quantity under explicit options.
func (q Quantity) Format(options FormatOptions) (string, error) {
	if options.Unit == "" || len(options.Separator) > 16 || !utf8.ValidString(options.Separator) ||
		strings.ContainsAny(options.Separator, "\r\n\x00") {
		return "", ErrInvalidQuantity
	}
	converted, err := q.Convert(options.Unit, options.Conversion)
	if err != nil {
		return "", err
	}
	rounded, err := converted.Round(options.Scale, options.Rounding)
	if err != nil {
		return "", err
	}

	return rounded.amount.String() + options.Separator + string(options.Unit), nil
}

func (q *Quantity) decode(encoded encodedQuantity) error {
	amount, err := decimal.Parse(encoded.Value)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	decoded, err := New(amount, encoded.Unit)
	if err != nil {
		return err
	}
	*q = decoded

	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("%w: trailing JSON value", ErrInvalidQuantity)
	}

	return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
}

func decodeJSONObject(data []byte, allowed map[string]struct{}, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("%w: expected JSON object", ErrInvalidQuantity)
	}

	fields := make(map[string]struct{}, len(allowed))
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
		}
		name := token.(string)
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%w: unknown JSON field %q", ErrInvalidQuantity, name)
		}
		if _, duplicate := fields[name]; duplicate {
			return fmt.Errorf("%w: duplicate JSON field %q", ErrInvalidQuantity, name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
		}
		fields[name] = struct{}{}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
	}

	return nil
}

func decodeQuantityXML(decoder *xml.Decoder, start xml.StartElement) (encodedQuantity, error) {
	var encoded encodedQuantity
	seen := make(map[string]struct{}, 2)
	for {
		token, err := decoder.Token()
		if err != nil {
			return encodedQuantity{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			name := value.Name.Local
			if name != "value" && name != "unit" {
				return encodedQuantity{}, fmt.Errorf("%w: unknown XML field %q", ErrInvalidQuantity, name)
			}
			if _, duplicate := seen[name]; duplicate {
				return encodedQuantity{}, fmt.Errorf("%w: duplicate XML field %q", ErrInvalidQuantity, name)
			}
			seen[name] = struct{}{}
			if name == "value" {
				if err := decoder.DecodeElement(&encoded.Value, &value); err != nil {
					return encodedQuantity{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
				}
			} else if err := decoder.DecodeElement(&encoded.Unit, &value); err != nil {
				return encodedQuantity{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return encoded, nil
			}
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return encodedQuantity{}, fmt.Errorf("%w: unexpected XML text", ErrInvalidQuantity)
			}
		case xml.Comment:
		default:
			return encodedQuantity{}, fmt.Errorf("%w: unsupported XML token", ErrInvalidQuantity)
		}
	}
}

// MarshalJSON encodes every side's value and unit plus package quantity.
func (d Dimensions) MarshalJSON() ([]byte, error) {
	if _, err := NewDimensions(d.length, d.width, d.height, d.quantity); err != nil {
		return nil, err
	}

	return json.Marshal(encodedDimensions{
		Length: d.length, Width: d.width, Height: d.height, Quantity: d.quantity,
	})
}

// UnmarshalJSON decodes and validates a complete dimension triple.
func (d *Dimensions) UnmarshalJSON(data []byte) error {
	if d == nil {
		return ErrInvalidQuantity
	}
	if len(data) > MaxSerializedBytes {
		return fmt.Errorf("%w: JSON exceeds %d bytes", ErrInvalidQuantity, MaxSerializedBytes)
	}
	var encoded encodedDimensions
	allowed := map[string]struct{}{"length": {}, "width": {}, "height": {}, "quantity": {}}
	if err := decodeJSONObject(data, allowed, &encoded); err != nil {
		return err
	}
	decoded, err := NewDimensions(encoded.Length, encoded.Width, encoded.Height, encoded.Quantity)
	if err != nil {
		return err
	}
	*d = decoded

	return nil
}

// MarshalXML encodes every side's value and unit plus package quantity.
func (d Dimensions) MarshalXML(encoder *xml.Encoder, start xml.StartElement) error {
	if _, err := NewDimensions(d.length, d.width, d.height, d.quantity); err != nil {
		return err
	}
	if start.Name.Local == "" || start.Name.Local == "Dimensions" {
		start.Name.Local = "dimensions"
	}

	return encoder.EncodeElement(encodedDimensions{
		Length: d.length, Width: d.width, Height: d.height, Quantity: d.quantity,
	}, start)
}

// UnmarshalXML decodes and validates a complete dimension triple.
func (d *Dimensions) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	if d == nil {
		return ErrInvalidQuantity
	}
	encoded, err := decodeDimensionsXML(decoder, start)
	if err != nil {
		return err
	}
	decoded, err := NewDimensions(encoded.Length, encoded.Width, encoded.Height, encoded.Quantity)
	if err != nil {
		return err
	}
	*d = decoded

	return nil
}

func decodeDimensionsXML(decoder *xml.Decoder, start xml.StartElement) (encodedDimensions, error) {
	var encoded encodedDimensions
	seen := make(map[string]struct{}, 4)
	for {
		token, err := decoder.Token()
		if err != nil {
			return encodedDimensions{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			name := value.Name.Local
			if name != "length" && name != "width" && name != "height" && name != "quantity" {
				return encodedDimensions{}, fmt.Errorf("%w: unknown XML field %q", ErrInvalidQuantity, name)
			}
			if _, duplicate := seen[name]; duplicate {
				return encodedDimensions{}, fmt.Errorf("%w: duplicate XML field %q", ErrInvalidQuantity, name)
			}
			seen[name] = struct{}{}
			switch name {
			case "length":
				err = decoder.DecodeElement(&encoded.Length, &value)
			case "width":
				err = decoder.DecodeElement(&encoded.Width, &value)
			case "height":
				err = decoder.DecodeElement(&encoded.Height, &value)
			case "quantity":
				err = decoder.DecodeElement(&encoded.Quantity, &value)
			}
			if err != nil {
				return encodedDimensions{}, fmt.Errorf("%w: %w", ErrInvalidQuantity, err)
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return encoded, nil
			}
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return encodedDimensions{}, fmt.Errorf("%w: unexpected XML text", ErrInvalidQuantity)
			}
		case xml.Comment:
		default:
			return encodedDimensions{}, fmt.Errorf("%w: unsupported XML token", ErrInvalidQuantity)
		}
	}
}
