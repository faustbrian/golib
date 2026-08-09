package cloudevents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// ContentMode identifies a CloudEvents protocol-binding content mode.
type ContentMode uint8

const (
	_ ContentMode = iota
	BinaryMode
	StructuredMode
	BatchMode
)

// ErrUnsupportedMode identifies a content mode or event format this module
// does not implement.
var ErrUnsupportedMode = errors.New("cloudevents: unsupported content mode")

// HTTPMessage is a decoded CloudEvents HTTP message. Binary and structured
// messages contain one Event; batch messages may contain zero or more.
type HTTPMessage struct {
	Mode   ContentMode
	Events []Event
}

// EncodeHTTP maps events to HTTP headers and an owned body. Binary and
// structured modes require exactly one event; batch mode accepts zero or more.
// The caller owns the returned values.
func EncodeHTTP(events []Event, mode ContentMode) (http.Header, []byte, error) {
	switch mode {
	case BinaryMode:
		if len(events) != 1 {
			return nil, nil, fmt.Errorf("%w: binary event count", ErrUnsupportedMode)
		}
		if err := events[0].Validate(); err != nil {
			return nil, nil, err
		}
		return encodeHTTPBinary(events[0])
	case StructuredMode:
		if len(events) != 1 {
			return nil, nil, fmt.Errorf("%w: structured event count", ErrUnsupportedMode)
		}
		body, err := EncodeJSON(events[0])
		if err != nil {
			return nil, nil, err
		}
		header := make(http.Header)
		header.Set("Content-Type", JSONMediaType)
		return header, body, nil
	case BatchMode:
		body, err := EncodeJSONBatch(events)
		if err != nil {
			return nil, nil, err
		}
		header := make(http.Header)
		header.Set("Content-Type", JSONBatchMediaType)
		return header, body, nil
	default:
		return nil, nil, ErrUnsupportedMode
	}
}

func encodeHTTPBinary(event Event) (http.Header, []byte, error) {
	header := make(http.Header)
	setAttribute := func(name, value string) {
		header.Set("Ce-"+name, encodeHTTPAttribute(value))
	}
	setAttribute("specversion", event.SpecVersion())
	setAttribute("id", event.id)
	setAttribute("source", event.source)
	setAttribute("type", event.eventType)
	if event.dataSchema != "" {
		setAttribute("dataschema", event.dataSchema)
	}
	if event.subject != "" {
		setAttribute("subject", event.subject)
	}
	if event.hasTime {
		setAttribute("time", event.time.UTC().Format(time.RFC3339Nano))
	}
	for name, attribute := range event.extensions {
		setAttribute(name, attribute.String())
	}
	if event.dataContentType != "" {
		header.Set("Content-Type", event.dataContentType)
	} else if event.data.present {
		if event.data.kind == DataJSON {
			header.Set("Content-Type", "application/json")
		}
	}
	if !event.data.present {
		return header, nil, nil
	}
	return header, event.data.Bytes(), nil
}

func encodeHTTPAttribute(value string) string {
	encoded := make([]byte, 0, len(value))
	const hexadecimal = "0123456789ABCDEF"
	for _, character := range []byte(value) {
		if character >= 0x21 && character <= 0x7e && character != '"' && character != '%' {
			encoded = append(encoded, character)
			continue
		}
		encoded = append(encoded, '%', hexadecimal[character>>4], hexadecimal[character&0x0f])
	}
	return string(encoded)
}

// DecodeHTTP maps an HTTP header and body to CloudEvents. The caller retains
// ownership of body; DecodeHTTP never closes it. Cancellation can interrupt
// cancellation-aware readers and is checked before and after the bounded read.
func DecodeHTTP(ctx context.Context, header http.Header, body io.Reader, limits Limits) (HTTPMessage, error) {
	if ctx == nil {
		return HTTPMessage{}, fmt.Errorf("%w: nil context", ErrInvalidEvent)
	}
	if err := ctx.Err(); err != nil {
		return HTTPMessage{}, err
	}
	mode, contentType, err := detectHTTPMode(header)
	if err != nil {
		return HTTPMessage{}, err
	}
	limit := limits.MaxEventBytes
	if mode == BinaryMode {
		limit = limits.MaxDataBytes
	}
	value, err := readHTTPBody(ctx, body, limit)
	if err != nil {
		return HTTPMessage{}, err
	}

	if mode == StructuredMode {
		event, decodeErr := DecodeJSON(value, limits)
		if decodeErr != nil {
			return HTTPMessage{}, decodeErr
		}
		if conflictErr := validateStructuredHTTPMetadata(header, event, limits); conflictErr != nil {
			return HTTPMessage{}, conflictErr
		}
		return HTTPMessage{Mode: mode, Events: []Event{event}}, nil
	}
	if mode == BatchMode {
		if hasCloudEventsHTTPHeader(header) {
			return HTTPMessage{}, fmt.Errorf("%w: batch metadata headers", ErrInvalidEvent)
		}
		events, decodeErr := DecodeJSONBatch(value, limits)
		if decodeErr != nil {
			return HTTPMessage{}, decodeErr
		}
		return HTTPMessage{Mode: mode, Events: events}, nil
	}
	event, decodeErr := decodeHTTPBinary(header, contentType, value, limits)
	if decodeErr != nil {
		return HTTPMessage{}, decodeErr
	}
	return HTTPMessage{Mode: mode, Events: []Event{event}}, nil
}

func validateStructuredHTTPMetadata(header http.Header, event Event, limits Limits) error {
	expected := map[string]string{
		"specversion": event.SpecVersion(),
		"id":          event.id,
		"source":      event.source,
		"type":        event.eventType,
	}
	if event.dataContentType != "" {
		expected["datacontenttype"] = event.dataContentType
	}
	if event.dataSchema != "" {
		expected["dataschema"] = event.dataSchema
	}
	if event.subject != "" {
		expected["subject"] = event.subject
	}
	if event.hasTime {
		expected["time"] = event.time.UTC().Format(time.RFC3339Nano)
	}
	for name, attribute := range event.extensions {
		expected[name] = attribute.String()
	}
	seen := make(map[string]struct{})
	for headerName, values := range header {
		lowerName := strings.ToLower(headerName)
		if strings.HasPrefix(lowerName, "ce-") {
			if err := validateStructuredHTTPHeader(lowerName, values, seen, expected, limits); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStructuredHTTPHeader(
	lowerName string,
	values []string,
	seen map[string]struct{},
	expected map[string]string,
	limits Limits,
) error {
	name := strings.TrimPrefix(lowerName, "ce-")
	if limits.MaxAttributeNameBytes <= 0 || len(name) > limits.MaxAttributeNameBytes {
		return ErrLimitExceeded
	}
	if _, duplicate := seen[name]; duplicate || len(values) != 1 {
		return fmt.Errorf("%w: duplicate ce-%s", ErrInvalidEvent, name)
	}
	seen[name] = struct{}{}
	if exceedsHTTPEncodedAttributeLimit(values[0], limits.MaxAttributeValueBytes) {
		return ErrLimitExceeded
	}
	decoded, err := decodeHTTPAttribute(values[0])
	if err != nil {
		return fmt.Errorf("%w: ce-%s", ErrInvalidEvent, name)
	}
	want, present := expected[name]
	if !present || decoded != want {
		return fmt.Errorf("%w: conflicting ce-%s", ErrInvalidEvent, name)
	}
	return nil
}

func hasCloudEventsHTTPHeader(header http.Header) bool {
	for name := range header {
		if strings.HasPrefix(strings.ToLower(name), "ce-") {
			return true
		}
	}
	return false
}

func detectHTTPMode(header http.Header) (ContentMode, string, error) {
	values := headerValues(header, "Content-Type")
	if len(values) > 1 {
		return 0, "", fmt.Errorf("%w: duplicate content-type", ErrInvalidEvent)
	}
	if len(values) == 0 || values[0] == "" {
		return BinaryMode, "", nil
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil {
		return 0, "", fmt.Errorf("%w: content-type", ErrInvalidEvent)
	}
	switch strings.ToLower(mediaType) {
	case JSONMediaType:
		return StructuredMode, values[0], nil
	case JSONBatchMediaType:
		return BatchMode, values[0], nil
	default:
		if strings.HasPrefix(strings.ToLower(mediaType), "application/cloudevents") {
			return 0, "", ErrUnsupportedMode
		}
		return BinaryMode, values[0], nil
	}
}

func readHTTPBody(ctx context.Context, body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 || limit == int64(^uint64(0)>>1) {
		return nil, ErrLimitExceeded
	}
	if body == nil {
		return nil, nil
	}
	limited := &io.LimitedReader{R: body, N: limit + 1}
	value, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, ErrLimitExceeded
	}
	return value, nil
}

func decodeHTTPBinary(header http.Header, contentType string, body []byte, limits Limits) (Event, error) {
	encodedAttributes := make(map[string][]string)
	for name, values := range header {
		lowerName := strings.ToLower(name)
		if !strings.HasPrefix(lowerName, "ce-") {
			continue
		}
		attributeName := strings.TrimPrefix(lowerName, "ce-")
		encodedAttributes[attributeName] = append(encodedAttributes[attributeName], values...)
	}
	if _, present := encodedAttributes["datacontenttype"]; present {
		return Event{}, fmt.Errorf("%w: ce-datacontenttype", ErrInvalidEvent)
	}
	decodedAttributes := make(map[string]string, len(encodedAttributes))
	for name, values := range encodedAttributes {
		if limits.MaxAttributeNameBytes <= 0 || len(name) > limits.MaxAttributeNameBytes {
			return Event{}, ErrLimitExceeded
		}
		if len(values) != 1 {
			return Event{}, fmt.Errorf("%w: duplicate ce-%s", ErrInvalidEvent, name)
		}
		if exceedsHTTPEncodedAttributeLimit(values[0], limits.MaxAttributeValueBytes) {
			return Event{}, ErrLimitExceeded
		}
		decoded, err := decodeHTTPAttribute(values[0])
		if err != nil {
			return Event{}, fmt.Errorf("%w: ce-%s", ErrInvalidEvent, name)
		}
		if len(decoded) > limits.MaxAttributeValueBytes {
			return Event{}, ErrLimitExceeded
		}
		decodedAttributes[name] = decoded
	}
	if len(decodedAttributes) > limits.MaxAttributes {
		return Event{}, ErrLimitExceeded
	}
	if decodedAttributes["specversion"] != specVersion {
		return Event{}, fmt.Errorf("%w: specversion", ErrInvalidEvent)
	}
	var occurredAt *time.Time
	if value, present := decodedAttributes["time"]; present {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return Event{}, fmt.Errorf("%w: time", ErrInvalidEvent)
		}
		occurredAt = &parsed
	}
	extensions := make(map[string]Attribute)
	for name, value := range decodedAttributes {
		if _, reserved := reservedAttributeNames[name]; reserved {
			continue
		}
		attribute, err := NewStringAttribute(value)
		if err != nil {
			return Event{}, fmt.Errorf("%w: ce-%s", ErrInvalidEvent, name)
		}
		extensions[name] = attribute
	}
	data := Data{}
	if len(body) > 0 || contentType != "" {
		if isJSONMediaType(contentType) {
			jsonData, err := NewJSONData(body)
			if err != nil {
				return Event{}, err
			}
			data = jsonData
		} else {
			data = NewBinaryData(body)
		}
	}
	return NewEvent(Attributes{
		ID:              decodedAttributes["id"],
		Source:          decodedAttributes["source"],
		Type:            decodedAttributes["type"],
		DataContentType: contentType,
		DataSchema:      decodedAttributes["dataschema"],
		Subject:         decodedAttributes["subject"],
		Time:            occurredAt,
		Extensions:      extensions,
	}, data)
}

func exceedsHTTPEncodedAttributeLimit(value string, limit int) bool {
	if limit <= 0 {
		return true
	}
	high, maximum := bits.Mul(uint(limit), 3)
	return high == 0 && uint(len(value)) > maximum
}

func headerValues(header http.Header, name string) []string {
	var values []string
	for candidate, candidateValues := range header {
		if strings.EqualFold(candidate, name) {
			values = append(values, candidateValues...)
		}
	}
	return values
}

func decodeHTTPAttribute(value string) (string, error) {
	unquoted, err := unquoteHTTPAttribute(value)
	if err != nil {
		return "", err
	}
	decoded, err := url.PathUnescape(unquoted)
	if err != nil || !utf8.ValidString(decoded) {
		return "", ErrInvalidAttribute
	}
	return decoded, nil
}

func unquoteHTTPAttribute(value string) (string, error) {
	if value == "" || value[0] != '"' {
		return value, nil
	}
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", ErrInvalidAttribute
	}
	value = value[1 : len(value)-1]
	unquoted := make([]byte, 0, len(value))
	escaped := false
	for _, character := range []byte(value) {
		if escaped {
			escaped = false
		} else if character == '\\' {
			escaped = true
			continue
		} else if character == '"' {
			return "", ErrInvalidAttribute
		}
		if character < 0x20 || character == 0x7f {
			return "", ErrInvalidAttribute
		}
		unquoted = append(unquoted, character)
	}
	if escaped {
		return "", ErrInvalidAttribute
	}
	return string(unquoted), nil
}
