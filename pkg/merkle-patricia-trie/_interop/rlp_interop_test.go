//go:build interoperability

package interop_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"testing"

	gethrlp "github.com/ethereum/go-ethereum/rlp"

	localrlp "github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

type ethereumJSRLPValue struct {
	Kind     string               `json:"kind"`
	Bytes    string               `json:"bytes,omitempty"`
	Elements []ethereumJSRLPValue `json:"elements,omitempty"`
}

type ethereumJSRLPRequest struct {
	Mode      string               `json:"mode"`
	Values    []ethereumJSRLPValue `json:"values"`
	Encodings []string             `json:"encodings"`
}

type ethereumJSRLPResult struct {
	Encodings []string `json:"encodings"`
	Accepted  []bool   `json:"accepted"`
}

func TestGethAndEthereumJSRLPEncodingDifferential(t *testing.T) {
	t.Parallel()

	values := rlpDifferentialValues()
	localEncodings := make([][]byte, len(values))
	request := ethereumJSRLPRequest{
		Mode:      "rlp",
		Values:    make([]ethereumJSRLPValue, len(values)),
		Encodings: make([]string, len(values)),
	}
	for index, value := range values {
		localEncoding, err := localrlp.Encode(value, localrlp.DefaultLimits())
		if err != nil {
			t.Fatalf("local Encode(%d) error = %v", index, err)
		}
		gethEncoding, err := encodeGethRLP(value)
		if err != nil {
			t.Fatalf("geth Encode(%d) error = %v", index, err)
		}
		if !slices.Equal(localEncoding, gethEncoding) {
			t.Fatalf(
				"local Encode(%d) = %x, geth = %x",
				index,
				localEncoding,
				gethEncoding,
			)
		}

		decoded, err := localrlp.Decode(localEncoding, localrlp.DefaultLimits())
		if err != nil {
			t.Fatalf("local Decode(%d) error = %v", index, err)
		}
		if !equalRLPValues(decoded, value) {
			t.Fatalf("local Decode(%d) did not preserve the value", index)
		}
		if err := validateGethRLP(localEncoding); err != nil {
			t.Fatalf("geth Decode(%d) error = %v", index, err)
		}

		localEncodings[index] = localEncoding
		request.Values[index] = ethereumJSValue(value)
		request.Encodings[index] = hex.EncodeToString(localEncoding)
	}

	result := runEthereumJSRLPOracle(t, request)
	if len(result.Encodings) != len(values) {
		t.Fatalf(
			"ethereumjs encoding count = %d, want %d",
			len(result.Encodings),
			len(values),
		)
	}
	if len(result.Accepted) != len(values) {
		t.Fatalf(
			"ethereumjs acceptance count = %d, want %d",
			len(result.Accepted),
			len(values),
		)
	}
	for index, encodedHex := range result.Encodings {
		encoded, err := hex.DecodeString(encodedHex)
		if err != nil {
			t.Fatalf("ethereumjs Encode(%d) = %q: %v", index, encodedHex, err)
		}
		if !slices.Equal(encoded, localEncodings[index]) {
			t.Fatalf(
				"ethereumjs Encode(%d) = %x, local = %x",
				index,
				encoded,
				localEncodings[index],
			)
		}
		if !result.Accepted[index] {
			t.Fatalf("ethereumjs rejected canonical local encoding %d", index)
		}
	}
}

func TestGethAndEthereumJSRLPMalformedInputInventory(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		encoded                 []byte
		ethereumJSCompatibility bool
	}{
		{encoded: nil, ethereumJSCompatibility: true},
		{encoded: []byte{0x80, 0x80}},
		{encoded: []byte{0x82, 0x01}},
		{encoded: []byte{0x81, 0x01}},
		{encoded: []byte{0xb8, 0x01, 0x80}},
		{encoded: []byte{0xb9, 0x00, 0x38}},
		{encoded: []byte{0xb9, 0x01}},
		{encoded: []byte{0xb8, 0x38, 0x01}},
		{encoded: []byte{0xc1, 0x81}},
		{encoded: []byte{0xc2, 0x81, 0x01}},
		{encoded: []byte{0xf8, 0x01, 0x80}},
		{encoded: []byte{0xf9, 0x00, 0x38}},
		{encoded: append([]byte{0xb9, 0x00, 0x38}, make([]byte, 56)...)},
		{
			encoded: append(
				[]byte{0xf9, 0x00, 0x38},
				append([]byte{0xb7}, make([]byte, 55)...)...,
			),
		},
		{encoded: []byte{0xbf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{encoded: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	}
	request := ethereumJSRLPRequest{
		Mode:      "rlp",
		Encodings: make([]string, len(invalid)),
	}
	for index, test := range invalid {
		if _, err := localrlp.Decode(
			test.encoded,
			localrlp.DefaultLimits(),
		); err == nil {
			t.Fatalf("local Decode(%d, %x) accepted invalid input", index, test.encoded)
		}
		if err := validateGethRLP(test.encoded); err == nil {
			t.Fatalf("geth Decode(%d, %x) accepted invalid input", index, test.encoded)
		}
		request.Encodings[index] = hex.EncodeToString(test.encoded)
	}

	result := runEthereumJSRLPOracle(t, request)
	if len(result.Accepted) != len(invalid) {
		t.Fatalf(
			"ethereumjs acceptance count = %d, want %d",
			len(result.Accepted),
			len(invalid),
		)
	}
	for index, accepted := range result.Accepted {
		if accepted != invalid[index].ethereumJSCompatibility {
			t.Fatalf(
				"ethereumjs Decode(%d, %x) acceptance = %t, want %t",
				index,
				invalid[index].encoded,
				accepted,
				invalid[index].ethereumJSCompatibility,
			)
		}
	}
}

func rlpDifferentialValues() []localrlp.Value {
	branch := make([]localrlp.Value, 17)
	for index := range branch {
		branch[index] = localrlp.String(nil)
	}
	values := []localrlp.Value{
		localrlp.String(nil),
		localrlp.String([]byte{0x00}),
		localrlp.String([]byte{0x7f}),
		localrlp.String([]byte{0x80}),
		localrlp.String([]byte{0xff}),
		localrlp.List(),
		localrlp.List(localrlp.String([]byte("cat")), localrlp.String([]byte("dog"))),
		localrlp.List(localrlp.List(), localrlp.List(localrlp.List())),
		localrlp.List(
			localrlp.String([]byte{0x20, 0x12, 0x34}),
			localrlp.String([]byte("leaf value")),
		),
		localrlp.List(
			localrlp.String([]byte{0x00, 0x12}),
			localrlp.String(make([]byte, 32)),
		),
		localrlp.List(branch...),
	}
	for _, length := range []int{
		2, 54, 55, 56, 57, 253, 254, 255, 256, 1024, 65535, 65536,
	} {
		payload := make([]byte, length)
		for index := range payload {
			payload[index] = byte(index*31 + length)
		}
		values = append(values, localrlp.String(payload))
	}

	for _, length := range []int{54, 55, 253, 254, 65532, 65533} {
		values = append(values, localrlp.List(localrlp.String(make([]byte, length))))
	}
	nested := localrlp.String([]byte("terminal"))
	for range 32 {
		nested = localrlp.List(nested)
	}
	return append(values, nested)
}

func encodeGethRLP(value localrlp.Value) ([]byte, error) {
	switch value.Kind() {
	case localrlp.KindString:
		return gethrlp.EncodeToBytes(value.Bytes())
	case localrlp.KindList:
		elements := value.Elements()
		raw := make([]gethrlp.RawValue, len(elements))
		for index, element := range elements {
			encoded, err := encodeGethRLP(element)
			if err != nil {
				return nil, err
			}
			raw[index] = encoded
		}
		return gethrlp.EncodeToBytes(raw)
	default:
		return nil, fmt.Errorf("unsupported local RLP kind %d", value.Kind())
	}
}

func validateGethRLP(encoded []byte) error {
	kind, _, rest, err := gethrlp.Split(encoded)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return gethrlp.ErrMoreThanOneValue
	}
	switch kind {
	case gethrlp.Byte, gethrlp.String:
		var decoded []byte
		return gethrlp.DecodeBytes(encoded, &decoded)
	case gethrlp.List:
		payload, rest, err := gethrlp.SplitList(encoded)
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return gethrlp.ErrMoreThanOneValue
		}
		for len(payload) != 0 {
			_, _, remaining, splitErr := gethrlp.Split(payload)
			if splitErr != nil {
				return splitErr
			}
			consumed := len(payload) - len(remaining)
			if err := validateGethRLP(payload[:consumed]); err != nil {
				return err
			}
			payload = remaining
		}
		return nil
	default:
		return fmt.Errorf("unsupported geth RLP kind %d", kind)
	}
}

func ethereumJSValue(value localrlp.Value) ethereumJSRLPValue {
	if value.Kind() == localrlp.KindString {
		return ethereumJSRLPValue{
			Kind:  "string",
			Bytes: hex.EncodeToString(value.Bytes()),
		}
	}
	elements := value.Elements()
	result := ethereumJSRLPValue{
		Kind:     "list",
		Elements: make([]ethereumJSRLPValue, len(elements)),
	}
	for index, element := range elements {
		result.Elements[index] = ethereumJSValue(element)
	}
	return result
}

func equalRLPValues(left, right localrlp.Value) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	if left.Kind() == localrlp.KindString {
		return slices.Equal(left.Bytes(), right.Bytes())
	}
	leftElements := left.Elements()
	rightElements := right.Elements()
	if len(leftElements) != len(rightElements) {
		return false
	}
	for index := range leftElements {
		if !equalRLPValues(leftElements[index], rightElements[index]) {
			return false
		}
	}
	return true
}

func runEthereumJSRLPOracle(
	t *testing.T,
	request ethereumJSRLPRequest,
) ethereumJSRLPResult {
	t.Helper()

	input, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	command := exec.CommandContext(
		context.Background(),
		"node",
		"../scripts/ethereumjs-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs RLP oracle error = %v: %s", err, output)
	}
	var result ethereumJSRLPResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	return result
}
