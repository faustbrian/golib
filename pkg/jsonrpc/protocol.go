package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Version is the JSON-RPC protocol version implemented by this package.
const Version = "2.0"

// IDKind identifies the JSON representation of an ID.
type IDKind uint8

const (
	// IDMissing represents an absent ID member and therefore a notification.
	IDMissing IDKind = iota
	// IDString represents a JSON string ID.
	IDString
	// IDNumber represents a JSON number ID.
	IDNumber
	// IDNull represents an explicit JSON null ID.
	IDNull
)

// ID preserves the exact JSON representation of a string, number, or null ID.
// See https://www.jsonrpc.org/specification#request_object.
type ID struct {
	kind      IDKind
	raw       json.RawMessage
	canonical string
}

// StringID constructs a string ID. Invalid UTF-8 is replaced according to
// encoding/json rules so correlation matches the transmitted value.
func StringID(value string) ID {
	raw, _ := json.Marshal(value)
	var canonical string
	_ = json.Unmarshal(raw, &canonical)
	return ID{kind: IDString, raw: raw, canonical: canonical}
}

// NumberID constructs a numeric ID while preserving value's wire spelling.
func NumberID(value json.Number) ID {
	return ID{kind: IDNumber, raw: json.RawMessage(value.String()), canonical: canonicalNumber(value.String())}
}

// NullID constructs an explicit null ID.
func NullID() ID { return ID{kind: IDNull, raw: json.RawMessage("null")} }

// Kind returns the ID's representation kind.
func (id ID) Kind() IDKind { return id.kind }

// Equal reports whether IDs have the same kind and value. Mathematically
// equivalent numeric spellings compare equal.
func (id ID) Equal(other ID) bool {
	return id.kind == other.kind && id.canonical == other.canonical
}

func (id ID) valid() bool {
	trimmed := bytes.TrimSpace(id.raw)
	if !json.Valid(trimmed) {
		return false
	}
	switch id.kind {
	case IDString:
		return len(trimmed) > 0 && trimmed[0] == '"'
	case IDNumber:
		return len(trimmed) > 0 && (trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9'))
	case IDNull:
		return bytes.Equal(trimmed, []byte("null"))
	default:
		return false
	}
}

// MarshalJSON preserves the ID's original JSON spelling. A missing ID marshals
// as null when encoded outside a Request.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.kind == IDMissing {
		return []byte("null"), nil
	}
	return append([]byte(nil), id.raw...), nil
}

// UnmarshalJSON decodes a string, number, or null ID without numeric precision
// loss and rejects invalid UTF-8.
func (id *ID) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("jsonrpc: invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("jsonrpc: trailing data after id")
		}
		return err
	}
	switch typed := value.(type) {
	case string:
		id.kind = IDString
		id.canonical = typed
	case json.Number:
		id.kind = IDNumber
		id.canonical = canonicalNumber(typed.String())
	case nil:
		id.kind = IDNull
		id.canonical = ""
	default:
		return errors.New("jsonrpc: id must be a string, number, or null")
	}
	id.raw = append(id.raw[:0], data...)
	return nil
}

func canonicalNumber(value string) string {
	index := 0
	numberSign := ""
	if index < len(value) && value[index] == '-' {
		numberSign = "-"
		index++
	}
	integerStart := index
	for index < len(value) && value[index] >= '0' && value[index] <= '9' {
		index++
	}
	if integerStart == index {
		return value
	}
	coefficient := value[integerStart:index]
	fractionDigits := 0
	if index < len(value) && value[index] == '.' {
		index++
		fractionStart := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if fractionStart == index {
			return value
		}
		fractionDigits = index - fractionStart
		coefficient += value[fractionStart:index]
	}
	exponentSign := 0
	exponentDigits := "0"
	if index < len(value) && (value[index] == 'e' || value[index] == 'E') {
		index++
		exponentSign = 1
		if index < len(value) && (value[index] == '+' || value[index] == '-') {
			if value[index] == '-' {
				exponentSign = -1
			}
			index++
		}
		digitsStart := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if digitsStart == index || index != len(value) {
			return value
		}
		exponentDigits = strings.TrimLeft(value[digitsStart:index], "0")
		if exponentDigits == "" {
			exponentSign = 0
			exponentDigits = "0"
		}
	} else if index != len(value) {
		return value
	}
	coefficient = strings.TrimLeft(coefficient, "0")
	if coefficient == "" {
		return "0"
	}
	trimmed := strings.TrimRight(coefficient, "0")
	adjustment := len(coefficient) - len(trimmed) - fractionDigits
	exponentSign, exponentDigits = addSignedDecimalInt(exponentSign, exponentDigits, adjustment)
	if exponentSign == 0 {
		return numberSign + trimmed
	}
	if exponentSign < 0 {
		exponentDigits = "-" + exponentDigits
	}
	return numberSign + trimmed + "e" + exponentDigits
}

func addSignedDecimalInt(sign int, digits string, adjustment int) (int, string) {
	if adjustment == 0 {
		return sign, digits
	}
	adjustmentSign := 1
	if adjustment < 0 {
		adjustmentSign = -1
		adjustment = -adjustment
	}
	adjustmentDigits := strconv.Itoa(adjustment)
	if sign == 0 {
		return adjustmentSign, adjustmentDigits
	}
	if sign == adjustmentSign {
		return sign, addDecimalMagnitudes(digits, adjustmentDigits)
	}
	comparison := compareDecimalMagnitudes(digits, adjustmentDigits)
	if comparison == 0 {
		return 0, "0"
	}
	if comparison > 0 {
		return sign, subtractDecimalMagnitudes(digits, adjustmentDigits)
	}
	return adjustmentSign, subtractDecimalMagnitudes(adjustmentDigits, digits)
}

func addDecimalMagnitudes(left, right string) string {
	length := max(len(left), len(right))
	result := make([]byte, length+1)
	leftIndex, rightIndex := len(left)-1, len(right)-1
	carry := byte(0)
	for resultIndex := length; resultIndex > 0; resultIndex-- {
		digit := carry
		if leftIndex >= 0 {
			digit += left[leftIndex] - '0'
			leftIndex--
		}
		if rightIndex >= 0 {
			digit += right[rightIndex] - '0'
			rightIndex--
		}
		result[resultIndex] = '0' + digit%10
		carry = digit / 10
	}
	if carry == 0 {
		return string(result[1:])
	}
	result[0] = '0' + carry
	return string(result)
}

func compareDecimalMagnitudes(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func subtractDecimalMagnitudes(larger, smaller string) string {
	result := make([]byte, len(larger))
	smallerIndex := len(smaller) - 1
	borrow := 0
	for index := len(larger) - 1; index >= 0; index-- {
		digit := int(larger[index]-'0') - borrow
		borrow = 0
		smallerDigit := 0
		if smallerIndex >= 0 {
			smallerDigit = int(smaller[smallerIndex] - '0')
			smallerIndex--
		}
		if digit < smallerDigit {
			digit += 10
			borrow = 1
		}
		digit -= smallerDigit
		result[index] = '0' + byte(digit)
	}
	return strings.TrimLeft(string(result), "0")
}

// Request represents the protocol's request object. Params, when present, must
// follow https://www.jsonrpc.org/specification#parameter_structures.
// See https://www.jsonrpc.org/specification#request_object.
type Request struct {
	// JSONRPC must equal Version.
	JSONRPC string `json:"jsonrpc"`
	// Method is the case-sensitive method name.
	Method string `json:"method"`
	// Params contains an optional JSON object or array.
	Params json.RawMessage `json:"params,omitempty"`
	// ID identifies a request; it is missing for notifications.
	ID        ID `json:"-"`
	idSet     bool
	methodSet bool
}

// UnmarshalJSON decodes a request while preserving whether ID and method were
// present and rejecting ambiguous protocol members.
func (r *Request) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateMembers(data, "jsonrpc", "method", "params", "id"); err != nil {
		return err
	}
	type wireRequest struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  json.RawMessage `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      json.RawMessage `json:"id"`
	}
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Method = ""
	r.JSONRPC, r.Params = wire.JSONRPC, wire.Params
	r.methodSet = wire.Method != nil
	if r.methodSet {
		trimmed := bytes.TrimSpace(wire.Method)
		if len(trimmed) == 0 || trimmed[0] != '"' {
			return errors.New("jsonrpc: method must be a string")
		}
		_ = json.Unmarshal(wire.Method, &r.Method)
	}
	r.idSet = wire.ID != nil
	if r.idSet {
		return json.Unmarshal(wire.ID, &r.ID)
	}
	r.ID = ID{}
	return nil
}

// MarshalJSON encodes a request while preserving notification ID omission.
func (r Request) MarshalJSON() ([]byte, error) {
	object := map[string]any{"jsonrpc": r.JSONRPC, "method": r.Method}
	if r.Params != nil {
		object["params"] = r.Params
	}
	if r.idSet || r.ID.Kind() != IDMissing {
		object["id"] = r.ID
	}
	return json.Marshal(object)
}

// IsNotification reports whether the request omitted its ID member as defined
// by https://www.jsonrpc.org/specification#notification.
func (r Request) IsNotification() bool { return !r.idSet && r.ID.Kind() == IDMissing }

// Validate returns an InvalidRequest error when the request envelope violates
// JSON-RPC 2.0.
func (r Request) Validate() *Error {
	if r.JSONRPC != Version || (!r.methodSet && r.Method == "") {
		return InvalidRequest()
	}
	if r.Params != nil {
		trimmed := bytes.TrimSpace(r.Params)
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid(trimmed) {
			return InvalidRequest()
		}
	}
	return nil
}

// Response represents the object defined by
// https://www.jsonrpc.org/specification#response_object.
type Response struct {
	// JSONRPC must equal Version.
	JSONRPC string `json:"jsonrpc"`
	// Result contains the success value.
	Result json.RawMessage `json:"result,omitempty"`
	// Error contains the failure object.
	Error *Error `json:"error,omitempty"`
	// ID correlates the response with its request.
	ID        ID `json:"id"`
	resultSet bool
	errorSet  bool
	idSet     bool
}

// UnmarshalJSON decodes a response and records the presence of result, error,
// and ID members for later validation.
func (r *Response) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateMembers(data, "jsonrpc", "result", "error", "id"); err != nil {
		return err
	}
	type wireResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
		ID      json.RawMessage `json:"id"`
	}
	var wire wireResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Result, r.Error, r.ID = nil, nil, ID{}
	r.JSONRPC, r.Result = wire.JSONRPC, wire.Result
	r.resultSet, r.errorSet, r.idSet = wire.Result != nil, wire.Error != nil, wire.ID != nil
	if r.errorSet {
		if bytes.Equal(bytes.TrimSpace(wire.Error), []byte("null")) {
			return errors.New("jsonrpc: error must be an object")
		}
		if err := json.Unmarshal(wire.Error, &r.Error); err != nil {
			return err
		}
	}
	if r.idSet {
		if err := json.Unmarshal(wire.ID, &r.ID); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON encodes exactly one result or error member.
func (r Response) MarshalJSON() ([]byte, error) {
	object := map[string]any{"jsonrpc": r.JSONRPC, "id": r.ID}
	if r.Error != nil || r.errorSet {
		object["error"] = r.Error
	} else {
		result := r.Result
		if result == nil {
			result = json.RawMessage("null")
		}
		object["result"] = result
	}
	return json.Marshal(object)
}

// Validate checks the version, ID, result/error exclusivity, and error shape.
func (r Response) Validate() error {
	if r.JSONRPC != Version || !r.idSet || r.ID.Kind() == IDMissing {
		return errors.New("jsonrpc: invalid response envelope")
	}
	if r.resultSet == r.errorSet {
		return errors.New("jsonrpc: response must contain exactly one of result or error")
	}
	if r.errorSet && (r.Error == nil || !r.Error.valid()) {
		return errors.New("jsonrpc: invalid error object")
	}
	return nil
}

func rejectDuplicateMembers(data []byte, reservedNames ...string) error {
	if !utf8.Valid(data) {
		return errors.New("jsonrpc: invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, object := token.(json.Delim)
	if !object || delimiter != '{' {
		return nil
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		name := token.(string)
		for _, reservedName := range reservedNames {
			if name != reservedName && strings.EqualFold(name, reservedName) {
				return fmt.Errorf("jsonrpc: protocol member %q is case-sensitive", reservedName)
			}
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("jsonrpc: duplicate object member %q", name)
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
