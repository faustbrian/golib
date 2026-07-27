package eventsourcing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	// JSONContentType is the canonical media type emitted by JSONCodec.
	JSONContentType = "application/json"
	// MaxJSONDepth bounds nested arrays and objects accepted by JSONCodec.
	MaxJSONDepth = 100
	// MaxJSONContainerEntries bounds members in one object or array.
	MaxJSONContainerEntries = 10_000
)

// PayloadCodec encodes and decodes explicitly identified application events.
//
// Implementations must be deterministic and safe for concurrent use.
type PayloadCodec interface {
	Encode(DecodedEvent) (EncodedEvent, error)
	Decode(EncodedEvent) (DecodedEvent, error)
}

// ContextPayloadCodec optionally exposes caller context to a payload codec.
//
// Repository operations prefer this extension when implemented. Codecs must
// not retain the context or use it to make serialization nondeterministic.
type ContextPayloadCodec interface {
	PayloadCodec
	EncodeContext(context.Context, DecodedEvent) (EncodedEvent, error)
	DecodeContext(context.Context, EncodedEvent) (DecodedEvent, error)
}

// MessageCodec encodes and decodes complete persisted message envelopes.
//
// Implementations must be deterministic, safe for concurrent use, enforce the
// core envelope field limits, and treat decoded bytes as untrusted input. Encode
// returns caller-owned bytes. Decode must not retain input bytes and returns an
// independently owned Message. Stored or external input must return an error,
// never panic.
type MessageCodec interface {
	Encode(Message) ([]byte, error)
	Decode([]byte) (Message, error)
}

type eventKey struct {
	name    string
	version SchemaVersion
}

type jsonEventRegistration struct {
	key    eventKey
	encode func(any) ([]byte, error)
	decode func([]byte, bool) (any, error)
}

type jsonAliasRegistration struct {
	key    eventKey
	target eventKey
}

// JSONRegistration is one immutable event type or alias declaration.
//
// Values are created with JSONEvent or JSONAlias.
type JSONRegistration struct {
	event *jsonEventRegistration
	alias *jsonAliasRegistration
}

// JSONCodecOption is a validated immutable JSON codec declaration.
//
// JSONEvent, JSONAlias, and WithJSONStrictDecoding return supported options.
type JSONCodecOption interface {
	configureJSONCodec(*jsonCodecBuilder) error
}

// JSONEvent registers one stable event identity and Go value type.
func JSONEvent[T any](name string, version SchemaVersion) JSONRegistration {
	return JSONRegistration{
		event: &jsonEventRegistration{
			key: eventKey{name: name, version: version},
			encode: func(input any) ([]byte, error) {
				value, ok := input.(T)
				if !ok {
					return nil, ErrEventTypeMismatch
				}

				payload, err := json.Marshal(value)
				if err != nil {
					return nil, fmt.Errorf("%w: encode JSON", ErrMalformedEvent)
				}

				return payload, nil
			},
			decode: func(payload []byte, strict bool) (any, error) {
				if err := validateJSONPayload(payload); err != nil {
					return nil, err
				}

				var value T
				decoder := json.NewDecoder(bytes.NewReader(payload))
				decoder.UseNumber()
				if strict {
					decoder.DisallowUnknownFields()
				}
				if err := decoder.Decode(&value); err != nil {
					return nil, fmt.Errorf("%w: decode JSON", ErrMalformedEvent)
				}

				return value, nil
			},
		},
	}
}

// JSONAlias maps one historical identity to a registered canonical identity.
//
// Aliases affect decoding only and never rewrite stored event data.
func JSONAlias(
	name string,
	version SchemaVersion,
	targetName string,
	targetVersion SchemaVersion,
) JSONRegistration {
	return JSONRegistration{
		alias: &jsonAliasRegistration{
			key:    eventKey{name: name, version: version},
			target: eventKey{name: targetName, version: targetVersion},
		},
	}
}

type strictJSONDecoding struct{}

// WithJSONStrictDecoding rejects object fields unknown to the registered Go
// event type.
func WithJSONStrictDecoding() JSONCodecOption {
	return strictJSONDecoding{}
}

// JSONCodec is an immutable explicit JSON event registry.
type JSONCodec struct {
	events  map[eventKey]jsonEventRegistration
	aliases map[eventKey]eventKey
	strict  bool
}

type jsonCodecBuilder struct {
	codec      *JSONCodec
	claimed    map[eventKey]struct{}
	aliasOrder []eventKey
}

// NewJSONCodec validates declarations and returns a concurrency-safe codec.
func NewJSONCodec(options ...JSONCodecOption) (*JSONCodec, error) {
	builder := jsonCodecBuilder{
		codec: &JSONCodec{
			events:  make(map[eventKey]jsonEventRegistration),
			aliases: make(map[eventKey]eventKey),
		},
		claimed: make(map[eventKey]struct{}, len(options)),
	}
	for _, option := range options {
		if option == nil {
			return nil, invalid("option", "must be assigned")
		}
		if err := option.configureJSONCodec(&builder); err != nil {
			return nil, fmt.Errorf("configure JSON codec: %w", err)
		}
	}
	if len(builder.codec.events) == 0 {
		return nil, invalid("registrations", "must contain at least one event")
	}

	for _, alias := range builder.aliasOrder {
		target := builder.codec.aliases[alias]
		if _, exists := builder.codec.events[target]; !exists {
			return nil, fmt.Errorf(
				"%w: alias %s@%d targets an unregistered event",
				ErrUnknownEvent,
				alias.name,
				alias.version,
			)
		}
	}

	return builder.codec, nil
}

// Encode produces deterministic canonical JSON for a registered event.
func (codec *JSONCodec) Encode(event DecodedEvent) (EncodedEvent, error) {
	if event.IsZero() {
		return EncodedEvent{}, invalid("event", "must be assigned")
	}

	key := eventKey{name: event.name.value, version: event.version}
	registration, exists := codec.events[key]
	if !exists {
		return EncodedEvent{}, fmt.Errorf(
			"%w: %s@%d",
			ErrUnknownEvent,
			key.name,
			key.version,
		)
	}

	payload, err := registration.encode(event.value)
	if err != nil {
		return EncodedEvent{}, fmt.Errorf("encode %s@%d: %w", key.name, key.version, err)
	}

	encoded, err := NewEncodedEvent(EncodedEventInput{
		Name:        key.name,
		Version:     key.version,
		ContentType: JSONContentType,
		Payload:     payload,
	})
	if err != nil {
		return EncodedEvent{}, fmt.Errorf("construct encoded event: %w", err)
	}

	return encoded, nil
}

// Decode returns the registered canonical event without modifying stored data.
func (codec *JSONCodec) Decode(event EncodedEvent) (DecodedEvent, error) {
	if event.IsZero() {
		return DecodedEvent{}, invalid("event", "must be assigned")
	}
	if event.contentType != JSONContentType {
		return DecodedEvent{}, fmt.Errorf(
			"%w: %s",
			ErrUnsupportedContentType,
			event.contentType,
		)
	}

	key := eventKey{name: event.name.value, version: event.version}
	if target, exists := codec.aliases[key]; exists {
		key = target
	}
	registration, exists := codec.events[key]
	if !exists {
		return DecodedEvent{}, fmt.Errorf(
			"%w: %s@%d",
			ErrUnknownEvent,
			event.name.value,
			event.version,
		)
	}

	value, err := registration.decode(event.payload, codec.strict)
	if err != nil {
		return DecodedEvent{}, fmt.Errorf(
			"decode %s@%d: %w",
			event.name.value,
			event.version,
			err,
		)
	}

	return DecodedEvent{
		name:    EventName{value: key.name},
		version: key.version,
		value:   value,
	}, nil
}

func encodePayload(
	ctx context.Context,
	codec PayloadCodec,
	event DecodedEvent,
) (EncodedEvent, error) {
	if err := ctx.Err(); err != nil {
		return EncodedEvent{}, err
	}
	if contextual, ok := codec.(ContextPayloadCodec); ok {
		return contextual.EncodeContext(ctx, event)
	}

	return codec.Encode(event)
}

func decodePayload(
	ctx context.Context,
	codec PayloadCodec,
	event EncodedEvent,
) (DecodedEvent, error) {
	if err := ctx.Err(); err != nil {
		return DecodedEvent{}, err
	}
	if contextual, ok := codec.(ContextPayloadCodec); ok {
		return contextual.DecodeContext(ctx, event)
	}

	return codec.Decode(event)
}

func (codec *JSONCodec) addEvent(
	registration jsonEventRegistration,
	claimed map[eventKey]struct{},
) error {
	if err := validateEventKey(registration.key); err != nil {
		return err
	}
	if _, exists := claimed[registration.key]; exists {
		return fmt.Errorf(
			"%w: %s@%d",
			ErrDuplicateRegistration,
			registration.key.name,
			registration.key.version,
		)
	}

	claimed[registration.key] = struct{}{}
	codec.events[registration.key] = registration

	return nil
}

func (registration JSONRegistration) configureJSONCodec(
	builder *jsonCodecBuilder,
) error {
	switch {
	case registration.event != nil && registration.alias == nil:
		return builder.codec.addEvent(*registration.event, builder.claimed)
	case registration.alias != nil && registration.event == nil:
		if err := builder.codec.addAlias(*registration.alias, builder.claimed); err != nil {
			return err
		}
		builder.aliasOrder = append(builder.aliasOrder, registration.alias.key)

		return nil
	default:
		return invalid("registration", "must be an event or alias declaration")
	}
}

func (strictJSONDecoding) configureJSONCodec(builder *jsonCodecBuilder) error {
	if builder.codec.strict {
		return invalid("strict_decoding", "must not be configured more than once")
	}
	builder.codec.strict = true

	return nil
}

func (codec *JSONCodec) addAlias(
	registration jsonAliasRegistration,
	claimed map[eventKey]struct{},
) error {
	if err := validateEventKey(registration.key); err != nil {
		return err
	}
	if err := validateEventKey(registration.target); err != nil {
		return fmt.Errorf("alias target: %w", err)
	}
	if registration.key == registration.target {
		return invalid("alias", "must target a different event identity")
	}
	if registration.key.version != registration.target.version {
		return invalid("alias", "must preserve the event schema version")
	}
	if _, exists := claimed[registration.key]; exists {
		return fmt.Errorf(
			"%w: %s@%d",
			ErrDuplicateRegistration,
			registration.key.name,
			registration.key.version,
		)
	}

	claimed[registration.key] = struct{}{}
	codec.aliases[registration.key] = registration.target

	return nil
}

func validateEventKey(key eventKey) error {
	if !validName(key.name, MaxEventNameBytes) {
		return invalid("event_name", "must be a non-empty canonical name")
	}
	if key.version == 0 {
		return invalid("event_schema_version", "must be greater than zero")
	}

	return nil
}

func validateJSONPayload(payload []byte) error {
	if !utf8.Valid(payload) {
		return fmt.Errorf("%w: invalid UTF-8", ErrMalformedEvent)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON data", ErrMalformedEvent)
	}

	return nil
}

func validateJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: invalid JSON", ErrMalformedEvent)
	}

	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if depth >= MaxJSONDepth {
		return fmt.Errorf("%w: nesting limit exceeded", ErrMalformedEvent)
	}

	if delimiter == '{' {
		keys := make(map[string]struct{})
		entries := 0
		for decoder.More() {
			entries++
			if entries > MaxJSONContainerEntries {
				return fmt.Errorf("%w: container entry limit exceeded", ErrMalformedEvent)
			}
			token, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("%w: invalid object key", ErrMalformedEvent)
			}
			key, _ := token.(string)
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("%w: duplicate object key", ErrMalformedEvent)
			}
			keys[key] = struct{}{}
			if valueErr := validateJSONValue(decoder, depth+1); valueErr != nil {
				return valueErr
			}
		}
	} else {
		entries := 0
		for decoder.More() {
			entries++
			if entries > MaxJSONContainerEntries {
				return fmt.Errorf("%w: container entry limit exceeded", ErrMalformedEvent)
			}
			if valueErr := validateJSONValue(decoder, depth+1); valueErr != nil {
				return valueErr
			}
		}
	}

	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: unterminated JSON container", ErrMalformedEvent)
	}

	return nil
}
