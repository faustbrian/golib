package search

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidCursorCodec = errors.New("search: cursor codec requires a 256-bit key, clock, and positive token bound")
	ErrInvalidCursor      = errors.New("search: invalid cursor")
	ErrCursorBinding      = errors.New("search: cursor does not belong to this tenant, index, or query")
	ErrIndexChanged       = errors.New("search: index definition changed during pagination")
	ErrCursorExpired      = errors.New("search: cursor expired")
)

// CursorBinding prevents a cursor from being replayed across tenants, aliases,
// queries, or index-definition generations.
type CursorBinding struct {
	Tenant, Index, QueryFingerprint, IndexFingerprint string
}

// CursorState is the bounded continuation state for point-in-time
// search-after pagination. PointInTime is opaque backend state and must not be
// logged or exposed separately from the signed cursor.
type CursorState struct {
	PointInTime string
	SortValues  []json.RawMessage
	Page        int
	Items       int
	Bytes       int64
	ExpiresAt   time.Time
}

type cursorEnvelope struct {
	Version          uint8             `json:"v"`
	Tenant           string            `json:"t"`
	Index            string            `json:"i"`
	QueryFingerprint string            `json:"q"`
	IndexFingerprint string            `json:"f"`
	PointInTime      string            `json:"p"`
	SortValues       []json.RawMessage `json:"s"`
	Page             int               `json:"n"`
	Items            int               `json:"c"`
	Bytes            int64             `json:"b"`
	ExpiresUnixNano  int64             `json:"e"`
}

// CursorCodec signs and verifies opaque cursor state. It is safe for concurrent
// use when the supplied clock is safe for concurrent use.
type CursorCodec struct {
	key      []byte
	now      func() time.Time
	maxBytes int
}

// NewCursorCodec constructs a bounded HMAC-SHA256 cursor codec.
func NewCursorCodec(key []byte, now func() time.Time, maxBytes int) (*CursorCodec, error) {
	if len(key) < sha256.Size || now == nil || maxBytes <= 0 {
		return nil, ErrInvalidCursorCodec
	}

	return &CursorCodec{key: append([]byte(nil), key...), now: now, maxBytes: maxBytes}, nil
}

// Encode validates, copies, and signs cursor state.
func (c *CursorCodec) Encode(binding CursorBinding, state CursorState) (string, error) {
	if !validCursorBinding(binding) || state.PointInTime == "" || len(state.SortValues) == 0 ||
		state.Page < 0 || state.Items < 0 || state.Bytes < 0 || !state.ExpiresAt.After(c.now()) ||
		!cursorInputsWithinBudget(binding, state, c.maxBytes) {
		return "", ErrInvalidCursor
	}
	envelope := cursorEnvelope{
		Version: 1, Tenant: binding.Tenant, Index: binding.Index,
		QueryFingerprint: binding.QueryFingerprint, IndexFingerprint: binding.IndexFingerprint,
		PointInTime: state.PointInTime, SortValues: cloneRawMessages(state.SortValues),
		Page: state.Page, Items: state.Items, Bytes: state.Bytes,
		ExpiresUnixNano: state.ExpiresAt.UTC().UnixNano(),
	}
	payload, _ := json.Marshal(envelope)
	signature := c.sign(payload)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature)
	if len(token) > c.maxBytes {
		return "", ErrInvalidCursor
	}

	return token, nil
}

func cursorInputsWithinBudget(binding CursorBinding, state CursorState, maximum int) bool {
	remaining := maximum
	for _, size := range []int{len(binding.Tenant), len(binding.Index), len(binding.QueryFingerprint), len(binding.IndexFingerprint), len(state.PointInTime), len(state.SortValues)} {
		if size > remaining {
			return false
		}
		remaining -= size
	}
	for _, value := range state.SortValues {
		if len(value) > remaining || !json.Valid(value) {
			return false
		}
		remaining -= len(value)
	}
	return true
}

// Decode verifies a cursor and enforces binding, expiry, and total traversal
// budgets before returning caller-owned continuation state.
func (c *CursorCodec) Decode(token string, binding CursorBinding, limits Limits) (CursorState, error) {
	if limits.Validate() != nil {
		return CursorState{}, ErrPageLimit
	}
	if len(token) == 0 {
		return CursorState{}, ErrInvalidCursor
	}
	if len(token) > c.maxBytes {
		return CursorState{}, ErrInvalidCursor
	}
	if strings.Count(token, ".") != 1 {
		return CursorState{}, ErrInvalidCursor
	}
	payloadText, signatureText, _ := strings.Cut(token, ".")
	if payloadText == "" {
		return CursorState{}, ErrInvalidCursor
	}
	if signatureText == "" {
		return CursorState{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadText)
	if err != nil {
		return CursorState{}, ErrInvalidCursor
	}
	if base64.RawURLEncoding.EncodeToString(payload) != payloadText {
		return CursorState{}, ErrInvalidCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	if err != nil {
		return CursorState{}, ErrInvalidCursor
	}
	if base64.RawURLEncoding.EncodeToString(signature) != signatureText {
		return CursorState{}, ErrInvalidCursor
	}
	if !hmac.Equal(signature, c.sign(payload)) {
		return CursorState{}, ErrInvalidCursor
	}
	var envelope cursorEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Version != 1 ||
		envelope.PointInTime == "" || len(envelope.SortValues) == 0 {
		return CursorState{}, ErrInvalidCursor
	}
	if envelope.Tenant != binding.Tenant {
		return CursorState{}, ErrCursorBinding
	}
	if envelope.Index != binding.Index {
		return CursorState{}, ErrCursorBinding
	}
	if envelope.QueryFingerprint != binding.QueryFingerprint {
		return CursorState{}, ErrCursorBinding
	}
	if envelope.IndexFingerprint != binding.IndexFingerprint {
		return CursorState{}, ErrIndexChanged
	}
	now := c.now()
	expiresAt := time.Unix(0, envelope.ExpiresUnixNano)
	if !expiresAt.After(now) {
		return CursorState{}, ErrCursorExpired
	}
	if expiresAt.Sub(now) > limits.MaxCursorDuration {
		return CursorState{}, ErrPageLimit
	}
	if envelope.Page < 0 || envelope.Items < 0 || envelope.Bytes < 0 ||
		envelope.Page > limits.MaxPages || envelope.Items > limits.MaxPages*limits.MaxPageItems ||
		envelope.Bytes > limits.MaxResultBytes {
		return CursorState{}, ErrPageLimit
	}

	return CursorState{
		PointInTime: envelope.PointInTime,
		SortValues:  cloneRawMessages(envelope.SortValues),
		Page:        envelope.Page, Items: envelope.Items, Bytes: envelope.Bytes,
		ExpiresAt: expiresAt.UTC(),
	}, nil
}

func (c *CursorCodec) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func validCursorBinding(binding CursorBinding) bool {
	return binding.Tenant != "" && binding.Index != "" && binding.QueryFingerprint != "" && binding.IndexFingerprint != ""
}

func cloneRawMessages(values []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(values))
	for index, value := range values {
		result[index] = append(json.RawMessage(nil), value...)
	}
	return result
}
