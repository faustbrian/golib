// Package localizedwire adapts Text to bounded wire format packages.
package localizedwire

import (
	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

func stringMap(value localized.Text) map[string]string {
	result := make(map[string]string, value.Len())
	for _, entry := range value.Entries() {
		result[entry.Locale.String()] = entry.Text
	}
	return result
}

// EncodeJSON encodes canonical keys through wire's bounded JSON encoder.
func EncodeJSON(value localized.Text, options jsonwire.EncodeOptions) ([]byte, error) {
	return jsonwire.Encode(stringMap(value), options)
}

// DecodeJSON validates wire boundaries, then applies canonical duplicate and
// locale semantics from the localized JSON decoder.
func DecodeJSON(data []byte, options jsonwire.DecodeOptions) (localized.Text, error) {
	var shape map[string]string
	if err := jsonwire.Decode(data, &shape, options); err != nil {
		return localized.Text{}, err
	}
	maxBytes := int(options.MaxBytes)
	return localized.DecodeJSON(data, localized.DecodeOptions{MaxInputBytes: maxBytes})
}

// EncodeYAML encodes canonical keys through wire's bounded YAML encoder.
func EncodeYAML(value localized.Text, options yamlwire.EncodeOptions) ([]byte, error) {
	return yamlwire.Encode(stringMap(value), options)
}

// DecodeYAML decodes a strict string map and validates canonical locale keys.
func DecodeYAML(data []byte, options yamlwire.DecodeOptions) (localized.Text, error) {
	var values map[string]string
	if err := yamlwire.Decode(data, &values, options); err != nil {
		return localized.Text{}, err
	}
	return localized.TextFromMap(values)
}

// EncodeTOML encodes canonical keys through wire's bounded TOML encoder.
func EncodeTOML(value localized.Text, options tomlwire.EncodeOptions) ([]byte, error) {
	return tomlwire.Encode(stringMap(value), options)
}

// DecodeTOML decodes a string map and validates canonical locale keys.
func DecodeTOML(data []byte, options tomlwire.DecodeOptions) (localized.Text, error) {
	var values map[string]string
	if err := tomlwire.Decode(data, &values, options); err != nil {
		return localized.Text{}, err
	}
	return localized.TextFromMap(values)
}

// EncodeMessagePack encodes canonical keys through wire's deterministic
// bounded MessagePack encoder.
func EncodeMessagePack(value localized.Text, options msgpackwire.EncodeOptions) ([]byte, error) {
	return msgpackwire.Encode(stringMap(value), options)
}

// DecodeMessagePack decodes a strict string map and validates locale keys.
func DecodeMessagePack(data []byte, options msgpackwire.DecodeOptions) (localized.Text, error) {
	var values map[string]string
	if err := msgpackwire.Decode(data, &values, options); err != nil {
		return localized.Text{}, err
	}
	return localized.TextFromMap(values)
}
