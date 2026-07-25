// Package strictjson provides bounded JSON decoding with duplicate and unknown
// object member rejection.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxDepth = 64

// Decode validates size, syntax, duplicate members, unknown fields, and trailing
// data before populating target.
func Decode(data []byte, maxBytes int, target any) error {
	if maxBytes <= 0 || len(data) == 0 || len(data) > maxBytes {
		return errors.New("JSON size is outside its bounds")
	}
	validator := json.NewDecoder(bytes.NewReader(data))
	validator.UseNumber()
	if err := validateValue(validator, 1); err != nil {
		return err
	}
	if err := requireEnd(validator); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return requireEnd(decoder)
}

func validateValue(decoder *json.Decoder, depth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting depth exceeds the limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			member, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("read JSON member: %w", err)
			}
			name := member.(string)
			if _, duplicate := seen[name]; duplicate {
				return errors.New("JSON object member is duplicated")
			}
			seen[name] = struct{}{}
			if err := validateValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default: // A successful opening delimiter here can only be an array.
		for decoder.More() {
			if err := validateValue(decoder, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("close JSON value: %w", err)
	}
	return nil
}

func requireEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing data")
		}
		return fmt.Errorf("finish JSON: %w", err)
	}
	return nil
}
