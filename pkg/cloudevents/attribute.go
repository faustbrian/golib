package cloudevents

import (
	"encoding/base64"
	"strconv"
	"time"
)

// AttributeKind is a CloudEvents abstract context-attribute type.
type AttributeKind uint8

const (
	_ AttributeKind = iota
	AttributeString
	AttributeBoolean
	AttributeInteger
	AttributeBinary
	AttributeURI
	AttributeURIReference
	AttributeTimestamp
)

// Attribute is an immutable CloudEvents context attribute value.
type Attribute struct {
	kind  AttributeKind
	text  string
	bytes []byte
}

// NewStringAttribute constructs a string-valued context attribute.
func NewStringAttribute(value string) (Attribute, error) {
	if !validCloudString(value) {
		return Attribute{}, ErrInvalidAttribute
	}
	return Attribute{kind: AttributeString, text: value}, nil
}

// NewBooleanAttribute constructs a Boolean context attribute.
func NewBooleanAttribute(value bool) Attribute {
	return Attribute{kind: AttributeBoolean, text: strconv.FormatBool(value)}
}

// NewIntegerAttribute constructs a 32-bit Integer context attribute.
func NewIntegerAttribute(value int64) (Attribute, error) {
	if value < -2147483648 || value > 2147483647 {
		return Attribute{}, ErrInvalidAttribute
	}
	return Attribute{kind: AttributeInteger, text: strconv.FormatInt(value, 10)}, nil
}

// NewBinaryAttribute constructs a Binary context attribute and takes a copy of
// value.
func NewBinaryAttribute(value []byte) Attribute {
	owned := append([]byte(nil), value...)
	return Attribute{
		kind:  AttributeBinary,
		text:  base64.StdEncoding.EncodeToString(owned),
		bytes: owned,
	}
}

// NewURIAttribute constructs an absolute URI context attribute.
func NewURIAttribute(value string) (Attribute, error) {
	if !validCloudString(value) || !validAbsoluteURI(value) {
		return Attribute{}, ErrInvalidAttribute
	}
	return Attribute{kind: AttributeURI, text: value}, nil
}

// NewURIReferenceAttribute constructs a URI-reference context attribute.
func NewURIReferenceAttribute(value string) (Attribute, error) {
	if !validCloudString(value) || !validURIReference(value) {
		return Attribute{}, ErrInvalidAttribute
	}
	return Attribute{kind: AttributeURIReference, text: value}, nil
}

// NewTimestampAttribute constructs an RFC 3339 Timestamp context attribute.
func NewTimestampAttribute(value time.Time) Attribute {
	return Attribute{
		kind: AttributeTimestamp,
		text: value.UTC().Format(time.RFC3339Nano),
	}
}

// Kind returns the CloudEvents abstract attribute type.
func (a Attribute) Kind() AttributeKind { return a.kind }

// String returns the canonical string encoding of the attribute.
func (a Attribute) String() string { return a.text }

// Bytes returns a copy of a Binary attribute. It returns nil for every other
// attribute type.
func (a Attribute) Bytes() []byte {
	if a.kind != AttributeBinary {
		return nil
	}
	return append([]byte(nil), a.bytes...)
}
