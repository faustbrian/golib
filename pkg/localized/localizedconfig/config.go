// Package localizedconfig adapts Text to config's value-hook contracts.
package localizedconfig

import (
	"reflect"

	localized "github.com/faustbrian/golib/pkg/localized"
)

// Error is a stable privacy-safe configuration error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

// ErrInvalidValue reports a non-string-map configuration value.
const ErrInvalidValue Error = "localized config: invalid value"

// Text is a presence-aware config value hook.
type Text struct {
	Localized localized.Text
	Valid     bool
}

// NewText creates a present configuration wrapper.
func NewText(value localized.Text) Text {
	return Text{Localized: value, Valid: true}
}

// ConfigTextTarget asks textual sources to decode a string map before the hook.
func (Text) ConfigTextTarget() reflect.Type {
	return reflect.TypeFor[map[string]string]()
}

// UnmarshalConfigValue implements config's ValueUnmarshaler contract.
func (t *Text) UnmarshalConfigValue(input any) error {
	if t == nil {
		return ErrInvalidValue
	}
	if input == nil {
		*t = Text{}
		return nil
	}
	values := make(map[string]string)
	switch typed := input.(type) {
	case map[string]string:
		for key, value := range typed {
			values[key] = value
		}
	case map[string]any:
		for key, value := range typed {
			text, ok := value.(string)
			if !ok {
				return ErrInvalidValue
			}
			values[key] = text
		}
	default:
		return ErrInvalidValue
	}
	decoded, err := localized.TextFromMap(values)
	if err != nil {
		return err
	}
	*t = NewText(decoded)
	return nil
}
