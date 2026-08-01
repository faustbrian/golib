package secretenvelope

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"unicode"
	"unicode/utf8"
)

const (
	DataKeySize                  = 32
	NonceSize                    = 12
	MaxPlaintextSize             = 4 << 20
	maxKeyReferenceSize          = 2048
	maxEncryptedDataKeySize      = 64 << 10
	envelopeHeaderSize           = 17
	minimumEnvelopeSize          = envelopeHeaderSize + 1 + 1 + NonceSize + 16
	envelopeVersion         byte = 1
	algorithmAES256GCM      byte = 1
	envelopeMagic                = "SEV1"
	redacted                     = "[REDACTED]"
	MaxEnvelopeSize              = envelopeHeaderSize +
		maxKeyReferenceSize +
		maxEncryptedDataKeySize +
		NonceSize +
		MaxPlaintextSize +
		16
)

var ErrInvalidEnvelope = errors.New("secret envelope is invalid")

// Envelope is an immutable encrypted payload and wrapped data key.
type Envelope struct {
	keyReference     string
	encryptedDataKey []byte
	nonce            []byte
	ciphertext       []byte
}

// KeyReference identifies the wrapping key without exposing plaintext.
func (envelope Envelope) KeyReference() string {
	return envelope.keyReference
}

// Ciphertext returns a caller-owned copy of the authenticated ciphertext.
func (envelope Envelope) Ciphertext() []byte {
	return append([]byte(nil), envelope.ciphertext...)
}

// EncryptedDataKey returns a caller-owned copy of the wrapped data key.
func (envelope Envelope) EncryptedDataKey() []byte {
	return append([]byte(nil), envelope.encryptedDataKey...)
}

func (Envelope) String() string   { return redacted }
func (Envelope) GoString() string { return redacted }

// LogValue prevents envelope bytes and key references from entering slog.
func (Envelope) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON prevents accidental JSON disclosure of encrypted material.
func (Envelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(redacted)
}

// MarshalBinary returns the stable versioned persistence representation.
func (envelope Envelope) MarshalBinary() ([]byte, error) {
	if !envelope.valid() {
		return nil, ErrInvalidEnvelope
	}

	encoded := make([]byte, envelope.encodedSize())
	copy(encoded[:4], envelopeMagic)
	encoded[4] = envelopeVersion
	encoded[5] = algorithmAES256GCM
	binary.BigEndian.PutUint16(encoded[6:8], uint16(len(envelope.keyReference)))
	binary.BigEndian.PutUint32(
		encoded[8:12],
		uint32(len(envelope.encryptedDataKey)),
	)
	encoded[12] = byte(len(envelope.nonce))
	binary.BigEndian.PutUint32(encoded[13:17], uint32(len(envelope.ciphertext)))
	offset := envelopeHeaderSize
	offset += copy(encoded[offset:], envelope.keyReference)
	offset += copy(encoded[offset:], envelope.encryptedDataKey)
	offset += copy(encoded[offset:], envelope.nonce)
	copy(encoded[offset:], envelope.ciphertext)

	return encoded, nil
}

// ParseEnvelope validates and copies a versioned persistence representation.
func ParseEnvelope(encoded []byte) (Envelope, error) {
	if len(encoded) < minimumEnvelopeSize ||
		len(encoded) > MaxEnvelopeSize ||
		string(encoded[:4]) != envelopeMagic ||
		encoded[4] != envelopeVersion ||
		encoded[5] != algorithmAES256GCM {
		return Envelope{}, ErrInvalidEnvelope
	}

	keyReferenceSize := uint64(binary.BigEndian.Uint16(encoded[6:8]))
	encryptedDataKeySize := uint64(binary.BigEndian.Uint32(encoded[8:12]))
	nonceSize := uint64(encoded[12])
	ciphertextSize := uint64(binary.BigEndian.Uint32(encoded[13:17]))
	totalSize := uint64(envelopeHeaderSize) +
		keyReferenceSize +
		encryptedDataKeySize +
		nonceSize +
		ciphertextSize
	if totalSize != uint64(len(encoded)) ||
		keyReferenceSize > maxKeyReferenceSize ||
		encryptedDataKeySize > maxEncryptedDataKeySize ||
		nonceSize != NonceSize ||
		ciphertextSize > MaxPlaintextSize+16 {
		return Envelope{}, ErrInvalidEnvelope
	}

	offset := envelopeHeaderSize
	keyReferenceLength := int(keyReferenceSize)
	encryptedDataKeyLength := int(encryptedDataKeySize)
	nonceLength := int(nonceSize)
	envelope := Envelope{
		keyReference: string(encoded[offset : offset+keyReferenceLength]),
	}
	offset += keyReferenceLength
	envelope.encryptedDataKey = append(
		[]byte(nil),
		encoded[offset:offset+encryptedDataKeyLength]...,
	)
	offset += encryptedDataKeyLength
	envelope.nonce = append([]byte(nil), encoded[offset:offset+nonceLength]...)
	offset += nonceLength
	envelope.ciphertext = append([]byte(nil), encoded[offset:]...)
	if !envelope.valid() {
		return Envelope{}, ErrInvalidEnvelope
	}

	return envelope, nil
}

func (envelope Envelope) encodedSize() int {
	return envelopeHeaderSize +
		len(envelope.keyReference) +
		len(envelope.encryptedDataKey) +
		len(envelope.nonce) +
		len(envelope.ciphertext)
}

func (envelope Envelope) valid() bool {
	return validKeyReference(envelope.keyReference) &&
		len(envelope.encryptedDataKey) > 0 &&
		len(envelope.encryptedDataKey) <= maxEncryptedDataKeySize &&
		len(envelope.nonce) == NonceSize &&
		len(envelope.ciphertext) >= 16 &&
		len(envelope.ciphertext) <= MaxPlaintextSize+16 &&
		envelope.encodedSize() <= MaxEnvelopeSize
}

func validKeyReference(value string) bool {
	if value == "" ||
		len(value) > maxKeyReferenceSize ||
		!utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}
