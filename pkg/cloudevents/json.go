package cloudevents

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	JSONMediaType      = "application/cloudevents+json"
	JSONBatchMediaType = "application/cloudevents-batch+json"
)

type jsonMember struct {
	name  string
	value []byte
}

// EncodeJSON serializes event using the CloudEvents JSON event format. Member
// names are sorted lexicographically as a package determinism policy; that
// ordering is not required by CloudEvents conformance.
func EncodeJSON(event Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	members := make([]jsonMember, 0)
	appendString := func(name, value string) {
		encoded, _ := json.Marshal(value)
		members = append(members, jsonMember{name: name, value: encoded})
	}

	appendString("specversion", event.SpecVersion())
	appendString("id", event.id)
	appendString("source", event.source)
	appendString("type", event.eventType)
	if event.dataContentType != "" {
		appendString("datacontenttype", event.dataContentType)
	}
	if event.dataSchema != "" {
		appendString("dataschema", event.dataSchema)
	}
	if event.subject != "" {
		appendString("subject", event.subject)
	}
	if event.hasTime {
		appendString("time", event.time.UTC().Format(time.RFC3339Nano))
	}
	for name, attribute := range event.extensions {
		switch attribute.kind {
		case AttributeBoolean, AttributeInteger:
			members = append(members, jsonMember{name: name, value: []byte(attribute.text)})
		case AttributeString, AttributeBinary, AttributeURI, AttributeURIReference, AttributeTimestamp:
			appendString(name, attribute.text)
		}
	}
	if event.data.present {
		switch event.data.kind {
		case DataJSON:
			var compact bytes.Buffer
			_ = json.Compact(&compact, event.data.bytes)
			members = append(members, jsonMember{name: "data", value: compact.Bytes()})
		case DataText:
			encoded, _ := json.Marshal(string(event.data.bytes))
			members = append(members, jsonMember{name: "data", value: encoded})
		case DataBinary:
			encoded, _ := json.Marshal(newBinaryString(event.data.bytes))
			members = append(members, jsonMember{name: "data_base64", value: encoded})
		}
	}

	slices.SortFunc(members, func(left, right jsonMember) int {
		return strings.Compare(left.name, right.name)
	})
	var encoded bytes.Buffer
	encoded.WriteByte('{')
	for index, member := range members {
		if index > 0 {
			encoded.WriteByte(',')
		}
		name, _ := json.Marshal(member.name)
		encoded.Write(name)
		encoded.WriteByte(':')
		encoded.Write(member.value)
	}
	encoded.WriteByte('}')
	return encoded.Bytes(), nil
}

// EncodeJSONBatch serializes events using the normative JSON batch format.
func EncodeJSONBatch(events []Event) ([]byte, error) {
	var encoded bytes.Buffer
	encoded.WriteByte('[')
	for index, event := range events {
		if index > 0 {
			encoded.WriteByte(',')
		}
		value, err := EncodeJSON(event)
		if err != nil {
			return nil, err
		}
		encoded.Write(value)
	}
	encoded.WriteByte(']')
	return encoded.Bytes(), nil
}

func newBinaryString(value []byte) string {
	return NewBinaryAttribute(value).String()
}

// DecodeJSON parses one CloudEvent in the JSON event format without performing
// I/O. It takes ownership of all retained input.
func DecodeJSON(value []byte, limits Limits) (Event, error) {
	if limits.MaxEventBytes <= 0 || int64(len(value)) > limits.MaxEventBytes {
		return Event{}, ErrLimitExceeded
	}
	if err := inspectJSON(value, limits.MaxDepth); err != nil {
		return Event{}, err
	}

	var members map[string]json.RawMessage
	if err := json.Unmarshal(value, &members); err != nil {
		return Event{}, fmt.Errorf("%w: json", ErrInvalidEvent)
	}
	if limits.MaxAttributes <= 0 || countJSONAttributes(members) > limits.MaxAttributes {
		return Event{}, ErrLimitExceeded
	}

	stringMember := func(name string, required bool) (string, bool, error) {
		raw, present := members[name]
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if required {
				return "", false, fmt.Errorf("%w: %s", ErrInvalidEvent, name)
			}
			return "", false, nil
		}
		if exceedsJSONEncodedStringLimit(raw, int64(limits.MaxAttributeValueBytes)) {
			return "", false, ErrLimitExceeded
		}
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", false, fmt.Errorf("%w: %s", ErrInvalidEvent, name)
		}
		if len(decoded) > limits.MaxAttributeValueBytes {
			return "", false, ErrLimitExceeded
		}
		return decoded, true, nil
	}

	specVersionValue, _, err := stringMember("specversion", true)
	if err != nil {
		return Event{}, err
	}
	if specVersionValue != specVersion {
		return Event{}, fmt.Errorf("%w: specversion", ErrInvalidEvent)
	}
	id, _, err := stringMember("id", true)
	if err != nil {
		return Event{}, err
	}
	source, _, err := stringMember("source", true)
	if err != nil {
		return Event{}, err
	}
	eventType, _, err := stringMember("type", true)
	if err != nil {
		return Event{}, err
	}
	dataContentType, _, err := stringMember("datacontenttype", false)
	if err != nil {
		return Event{}, err
	}
	dataSchema, _, err := stringMember("dataschema", false)
	if err != nil {
		return Event{}, err
	}
	subject, _, err := stringMember("subject", false)
	if err != nil {
		return Event{}, err
	}
	timeValue, hasTime, err := stringMember("time", false)
	if err != nil {
		return Event{}, err
	}
	var occurredAt *time.Time
	if hasTime {
		parsed, parseErr := time.Parse(time.RFC3339Nano, timeValue)
		if parseErr != nil {
			return Event{}, fmt.Errorf("%w: time", ErrInvalidEvent)
		}
		occurredAt = &parsed
	}

	extensions := make(map[string]Attribute)
	for name, raw := range members {
		if _, reserved := reservedAttributeNames[name]; reserved {
			continue
		}
		if limits.MaxAttributeNameBytes <= 0 || len(name) > limits.MaxAttributeNameBytes {
			return Event{}, ErrLimitExceeded
		}
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			attribute, parseErr := decodeJSONAttribute(raw)
			if parseErr != nil {
				return Event{}, fmt.Errorf("%w: extension %s", ErrInvalidEvent, name)
			}
			extensions[name] = attribute
		}
	}

	data, err := decodeJSONData(members, dataContentType, limits)
	if err != nil {
		return Event{}, err
	}
	return NewEvent(Attributes{
		ID:              id,
		Source:          source,
		Type:            eventType,
		DataContentType: dataContentType,
		DataSchema:      dataSchema,
		Subject:         subject,
		Time:            occurredAt,
		Extensions:      extensions,
	}, data)
}

func countJSONAttributes(members map[string]json.RawMessage) int {
	count := 0
	for name := range members {
		switch name {
		case "data", "data_base64":
		default:
			count++
		}
	}
	return count
}

func exceedsJSONEncodedStringLimit(raw []byte, limit int64) bool {
	if limit <= 0 {
		return true
	}
	units, remainder := int64(len(raw)/6), len(raw)%6
	return units > limit || (units == limit && remainder > 2)
}

// DecodeJSONBatch parses the normative JSON batch format. Empty batches are
// valid. Every returned Event owns its storage.
func DecodeJSONBatch(value []byte, limits Limits) ([]Event, error) {
	if limits.MaxEventBytes <= 0 || int64(len(value)) > limits.MaxEventBytes {
		return nil, ErrLimitExceeded
	}
	if limits.MaxDepth <= 0 || limits.MaxDepth == int(^uint(0)>>1) {
		return nil, ErrLimitExceeded
	}
	if err := inspectJSON(value, limits.MaxDepth+1); err != nil {
		return nil, err
	}
	var rawEvents []json.RawMessage
	if err := json.Unmarshal(value, &rawEvents); err != nil {
		return nil, fmt.Errorf("%w: json batch", ErrInvalidEvent)
	}
	if limits.MaxBatchEvents <= 0 || len(rawEvents) > limits.MaxBatchEvents {
		return nil, ErrLimitExceeded
	}
	events := make([]Event, 0, len(rawEvents))
	for _, rawEvent := range rawEvents {
		event, err := DecodeJSON(rawEvent, limits)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func inspectJSON(value []byte, maxDepth int) error {
	if maxDepth <= 0 {
		return ErrLimitExceeded
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder, 0, maxDepth, true); err != nil {
		return err
	}
	if _, err := decoder.Token(); err == nil {
		return fmt.Errorf("%w: trailing json", ErrInvalidEvent)
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, depth, maxDepth int, topLevel bool) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: json", ErrInvalidEvent)
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	depth++
	if depth > maxDepth {
		return ErrLimitExceeded
	}
	if delimiter == '{' {
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, nameErr := decoder.Token()
			if nameErr != nil {
				return fmt.Errorf("%w: json", ErrInvalidEvent)
			}
			name := nameToken.(string)
			if topLevel {
				if _, duplicate := seen[name]; duplicate {
					return fmt.Errorf("%w: duplicate member", ErrInvalidEvent)
				}
				seen[name] = struct{}{}
			}
			if err := inspectJSONValue(decoder, depth, maxDepth, false); err != nil {
				return err
			}
		}
	} else {
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth, maxDepth, false); err != nil {
				return err
			}
		}
	}
	_, err = decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: json", ErrInvalidEvent)
	}
	return nil
}

func decodeJSONAttribute(raw json.RawMessage) (Attribute, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return Attribute{}, err
	}
	switch typed := value.(type) {
	case string:
		return NewStringAttribute(typed)
	case bool:
		return NewBooleanAttribute(typed), nil
	case json.Number:
		integer, err := strconv.ParseInt(string(typed), 10, 32)
		if err != nil {
			return Attribute{}, err
		}
		return NewIntegerAttribute(integer)
	default:
		return Attribute{}, ErrInvalidAttribute
	}
}

func decodeJSONData(members map[string]json.RawMessage, dataContentType string, limits Limits) (Data, error) {
	rawData, hasData := members["data"]
	rawBase64, hasBase64 := members["data_base64"]
	if hasData && hasBase64 {
		return Data{}, fmt.Errorf("%w: data and data_base64", ErrInvalidEvent)
	}
	if hasBase64 {
		var encoded string
		if err := json.Unmarshal(rawBase64, &encoded); err != nil {
			return Data{}, fmt.Errorf("%w: data_base64", ErrInvalidEvent)
		}
		if limits.MaxDataBytes <= 0 || int64(base64DecodedLength(encoded)) > limits.MaxDataBytes {
			return Data{}, ErrLimitExceeded
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return Data{}, fmt.Errorf("%w: data_base64", ErrInvalidEvent)
		}
		return NewBinaryData(decoded), nil
	}
	if !hasData {
		return Data{}, nil
	}
	if isJSONMediaType(dataContentType) {
		if limits.MaxDataBytes <= 0 || int64(len(rawData)) > limits.MaxDataBytes {
			return Data{}, ErrLimitExceeded
		}
		return NewJSONData(rawData)
	}
	if exceedsJSONEncodedStringLimit(rawData, limits.MaxDataBytes) {
		return Data{}, ErrLimitExceeded
	}
	var text string
	if err := json.Unmarshal(rawData, &text); err != nil {
		return Data{}, fmt.Errorf("%w: data", ErrInvalidEvent)
	}
	if int64(len(text)) > limits.MaxDataBytes {
		return Data{}, ErrLimitExceeded
	}
	return NewTextData(text)
}

func base64DecodedLength(encoded string) int {
	length := base64.StdEncoding.DecodedLen(len(encoded))
	if strings.HasSuffix(encoded, "=") {
		length--
	}
	if strings.HasSuffix(encoded, "==") {
		length--
	}
	return length
}

func isJSONMediaType(value string) bool {
	if value == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	parts := strings.SplitN(strings.ToLower(mediaType), "/", 2)
	return len(parts) == 2 && (parts[1] == "json" || strings.HasSuffix(parts[1], "+json"))
}
