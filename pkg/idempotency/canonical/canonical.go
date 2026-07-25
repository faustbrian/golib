// Package canonical provides bounded, explicit request fingerprint policies.
package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/deszhou/jcs"
	"github.com/faustbrian/golib/pkg/idempotency"
)

// Limits bounds JSON canonicalization before parsing, encoding, and hashing.
type Limits struct {
	// MaxInputBytes is the largest encoded JSON input accepted.
	MaxInputBytes int
	// MaxOutputBytes is the largest canonical JSON result accepted.
	MaxOutputBytes int
	// MaxDepth is the largest object or array nesting depth accepted.
	MaxDepth int
}

// JSON validates one JSON value under limits and returns its JCS encoding.
func JSON(input []byte, limits Limits) ([]byte, error) {
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	if len(input) > limits.MaxInputBytes {
		return nil, limitError("input")
	}
	if !utf8.Valid(input) {
		return nil, payloadError("json", errors.New("invalid UTF-8"))
	}
	if err := validateUnicodeEscapes(input); err != nil {
		return nil, payloadError("json", err)
	}
	if err := validateTokens(input, limits.MaxDepth); err != nil {
		return nil, err
	}

	canonical, err := jcs.Transform(input)
	if err != nil {
		return nil, payloadError("json", err)
	}
	if len(canonical) > limits.MaxOutputBytes {
		return nil, limitError("output")
	}
	return canonical, nil
}

// JSONFingerprint canonicalizes JSON and hashes it under a policy version.
func JSONFingerprint(version string, input []byte, limits Limits) (idempotency.Fingerprint, error) {
	canonical, err := JSON(input, limits)
	if err != nil {
		return idempotency.Fingerprint{}, err
	}
	return idempotency.NewFingerprint(version, canonical)
}

// BytesFingerprint hashes bounded bytes whose encoding is already canonical.
func BytesFingerprint(version string, input []byte, maxBytes int) (idempotency.Fingerprint, error) {
	if maxBytes <= 0 {
		return idempotency.Fingerprint{}, &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "max_bytes",
		}
	}
	if len(input) > maxBytes {
		return idempotency.Fingerprint{}, limitError("input")
	}
	return idempotency.NewFingerprint(version, input)
}

func validateLimits(limits Limits) error {
	if limits.MaxInputBytes <= 0 {
		return &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "input",
		}
	}
	if limits.MaxOutputBytes <= 0 {
		return &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "output",
		}
	}
	if limits.MaxDepth <= 0 {
		return &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "depth",
		}
	}
	return nil
}

func validateTokens(input []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return payloadError("json", err)
		}
		switch value := token.(type) {
		case json.Delim:
			if value == '{' || value == '[' {
				depth++
				if depth > maxDepth {
					return limitError("depth")
				}
			} else {
				depth--
			}
		case json.Number:
			number, err := strconv.ParseFloat(string(value), 64)
			if err != nil {
				return payloadError("json", err)
			}
			if number == 0 && math.Signbit(number) {
				return payloadError("json", errors.New("negative zero"))
			}
		}
	}
}

func validateUnicodeEscapes(input []byte) error {
	inString := false
	for index := 0; index < len(input); index++ {
		switch input[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			index++
			if index >= len(input) || input[index] != 'u' {
				continue
			}
			first, err := parseEscape(input, index+1)
			if err != nil {
				return err
			}
			index += 4
			switch {
			case first >= 0xd800 && first <= 0xdbff:
				if index+6 >= len(input) || input[index+1] != '\\' || input[index+2] != 'u' {
					return errors.New("missing low surrogate")
				}
				second, err := parseEscape(input, index+3)
				if err != nil {
					return err
				}
				if second < 0xdc00 || second > 0xdfff {
					return errors.New("invalid low surrogate")
				}
				index += 6
			case first >= 0xdc00 && first <= 0xdfff:
				return errors.New("unexpected low surrogate")
			}
		}
	}
	return nil
}

func parseEscape(input []byte, start int) (uint64, error) {
	if start+4 > len(input) {
		return 0, errors.New("incomplete Unicode escape")
	}
	value, err := strconv.ParseUint(string(input[start:start+4]), 16, 16)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func limitError(field string) error {
	return &idempotency.Error{
		Reason: idempotency.ReasonLimitExceeded,
		Field:  field,
	}
}

func payloadError(field string, cause error) error {
	return &idempotency.Error{
		Reason: idempotency.ReasonInvalidPayload,
		Field:  field,
		Cause:  cause,
	}
}
