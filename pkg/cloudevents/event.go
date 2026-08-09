package cloudevents

import (
	"encoding/json"
	"maps"
	"time"
	"unicode/utf8"
)

const specVersion = "1.0"

// DataKind distinguishes absent data from JSON, textual, and binary runtime
// values. The distinction controls normative event-format serialization.
type DataKind uint8

const (
	DataAbsent DataKind = iota
	DataJSON
	DataText
	DataBinary
)

// Data is an immutable CloudEvents data value.
type Data struct {
	kind    DataKind
	present bool
	bytes   []byte
}

// NewJSONData constructs JSON-valued event data and takes a copy of value.
func NewJSONData(value []byte) (Data, error) {
	if !json.Valid(value) {
		return Data{}, ErrInvalidData
	}
	return Data{kind: DataJSON, present: true, bytes: append([]byte(nil), value...)}, nil
}

// NewTextData constructs textual event data.
func NewTextData(value string) (Data, error) {
	if !utf8.ValidString(value) {
		return Data{}, ErrInvalidData
	}
	return Data{kind: DataText, present: true, bytes: []byte(value)}, nil
}

// NewBinaryData constructs binary event data and takes a copy of value.
func NewBinaryData(value []byte) Data {
	return Data{kind: DataBinary, present: true, bytes: append([]byte(nil), value...)}
}

// Present reports whether the event has a data value. Present empty or null
// values return true.
func (d Data) Present() bool { return d.present }

// Kind returns the runtime data kind.
func (d Data) Kind() DataKind { return d.kind }

// Bytes returns a copy of the event data's wire representation.
func (d Data) Bytes() []byte {
	if !d.present {
		return nil
	}
	return cloneBytesPreservingEmpty(d.bytes)
}

// Attributes contains the standard and extension context attributes used to
// construct an Event. NewEvent takes ownership by copying mutable inputs.
type Attributes struct {
	ID              string
	Source          string
	Type            string
	DataContentType string
	DataSchema      string
	Subject         string
	Time            *time.Time
	Extensions      map[string]Attribute
}

// Event is an immutable CloudEvent using the stable 1.0 specification.
type Event struct {
	id              string
	source          string
	eventType       string
	dataContentType string
	dataSchema      string
	subject         string
	time            time.Time
	hasTime         bool
	extensions      map[string]Attribute
	data            Data
}

// NewEvent validates and constructs an immutable CloudEvent.
func NewEvent(attributes Attributes, data Data) (Event, error) {
	if err := validateAttributes(attributes); err != nil {
		return Event{}, err
	}

	event := Event{
		id:              attributes.ID,
		source:          attributes.Source,
		eventType:       attributes.Type,
		dataContentType: attributes.DataContentType,
		dataSchema:      attributes.DataSchema,
		subject:         attributes.Subject,
		extensions:      make(map[string]Attribute, len(attributes.Extensions)),
		data:            Data{kind: data.kind, present: data.present, bytes: data.Bytes()},
	}
	if attributes.Time != nil {
		event.time = attributes.Time.UTC()
		event.hasTime = true
	}
	for name, value := range attributes.Extensions {
		event.extensions[name] = value
	}

	return event, nil
}

// Validate reports whether Event is assigned and satisfies the supported
// stable CloudEvents contract.
func (e Event) Validate() error {
	var occurredAt *time.Time
	if e.hasTime {
		value := e.time
		occurredAt = &value
	}
	if err := validateAttributes(Attributes{
		ID:              e.id,
		Source:          e.source,
		Type:            e.eventType,
		DataContentType: e.dataContentType,
		DataSchema:      e.dataSchema,
		Subject:         e.subject,
		Time:            occurredAt,
		Extensions:      e.extensions,
	}); err != nil {
		return err
	}
	if !e.data.present && e.data.kind != DataAbsent {
		return ErrInvalidEvent
	}
	if e.data.present {
		switch e.data.kind {
		case DataJSON:
			if !json.Valid(e.data.bytes) {
				return ErrInvalidEvent
			}
		case DataText:
			if !utf8.Valid(e.data.bytes) {
				return ErrInvalidEvent
			}
		case DataBinary:
		default:
			return ErrInvalidEvent
		}
	}
	return nil
}

// SpecVersion returns the stable CloudEvents specification version.
func (e Event) SpecVersion() string { return specVersion }

// ID returns the producer-scoped event identifier.
func (e Event) ID() string { return e.id }

// Source returns the event source URI-reference.
func (e Event) Source() string { return e.source }

// Type returns the producer-defined event type.
func (e Event) Type() string { return e.eventType }

// DataContentType returns the data media type and whether it is present.
func (e Event) DataContentType() (string, bool) {
	return e.dataContentType, e.dataContentType != ""
}

// DataSchema returns the absolute schema URI and whether it is present.
func (e Event) DataSchema() (string, bool) {
	return e.dataSchema, e.dataSchema != ""
}

// Subject returns the event subject and whether it is present.
func (e Event) Subject() (string, bool) {
	return e.subject, e.subject != ""
}

// Time returns the occurrence timestamp and whether it is present.
func (e Event) Time() (time.Time, bool) { return e.time, e.hasTime }

// Extension returns an extension context attribute by name.
func (e Event) Extension(name string) (Attribute, bool) {
	attribute, ok := e.extensions[name]
	return attribute, ok
}

// Extensions returns an independently owned copy of all extension context
// attributes, including attributes unknown to this package.
func (e Event) Extensions() map[string]Attribute {
	return maps.Clone(e.extensions)
}

// Data returns an immutable copy of the event data.
func (e Event) Data() Data {
	return Data{kind: e.data.kind, present: e.data.present, bytes: e.data.Bytes()}
}
