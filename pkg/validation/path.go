package validation

import (
	"strconv"
	"strings"
)

// SegmentKind distinguishes fields, collection indexes, keys, and items.
type SegmentKind uint8

const (
	// FieldSegment names an object field.
	FieldSegment SegmentKind = iota + 1
	// IndexSegment identifies a collection index.
	IndexSegment
	// KeySegment identifies a map key.
	KeySegment
	// ItemSegment identifies the current collection item in a plan.
	ItemSegment
)

// Segment is one typed path component.
type Segment struct {
	kind  SegmentKind
	value string
}

// Field creates a field segment.
func Field(name string) Segment { return Segment{kind: FieldSegment, value: name} }

// Index creates an index segment.
func Index(index int) Segment {
	return Segment{kind: IndexSegment, value: strconv.Itoa(index)}
}

// Key creates a map-key segment.
func Key(key string) Segment { return Segment{kind: KeySegment, value: key} }

// Item creates a generic item segment.
func Item() Segment { return Segment{kind: ItemSegment} }

// Kind returns the segment type.
func (s Segment) Kind() SegmentKind { return s.kind }

// Value returns the segment's safe location value.
func (s Segment) Value() string { return s.value }

// Path is an immutable ordered location.
type Path struct{ segments []Segment }

// RootPath returns an empty root path.
func RootPath() Path { return Path{} }

// Append returns a path copy with segment appended.
func (p Path) Append(segment Segment) Path {
	segments := make([]Segment, len(p.segments)+1)
	copy(segments, p.segments)
	segments[len(p.segments)] = segment
	return Path{segments: segments}
}

// Segments returns a defensive copy of the path segments.
func (p Path) Segments() []Segment {
	return append([]Segment(nil), p.segments...)
}

// String renders a stable human-readable location.
func (p Path) String() string {
	var result strings.Builder
	for index, segment := range p.segments {
		switch segment.kind {
		case FieldSegment:
			if index > 0 {
				result.WriteByte('.')
			}
			result.WriteString(segment.value)
		case IndexSegment, KeySegment:
			result.WriteByte('[')
			result.WriteString(segment.value)
			result.WriteByte(']')
		case ItemSegment:
			result.WriteString("[]")
		}
	}
	return result.String()
}

// JSONPointer renders the path using RFC 6901 escaping.
func (p Path) JSONPointer() string {
	var result strings.Builder
	for _, segment := range p.segments {
		result.WriteByte('/')
		if segment.kind == ItemSegment {
			result.WriteByte('-')
			continue
		}
		value := strings.ReplaceAll(segment.value, "~", "~0")
		result.WriteString(strings.ReplaceAll(value, "/", "~1"))
	}
	return result.String()
}

func (p Path) identity() string {
	var result strings.Builder
	for _, segment := range p.segments {
		result.WriteByte(byte(segment.kind))
		result.WriteString(strconv.Itoa(len(segment.value)))
		result.WriteByte(':')
		result.WriteString(segment.value)
	}
	return result.String()
}

func (p Path) exceedsRenderedLength(maximum int) bool {
	remaining := maximum
	for index, segment := range p.segments {
		length := 0
		switch segment.kind {
		case FieldSegment:
			length = len(segment.value)
			if index > 0 {
				length++
			}
		case IndexSegment, KeySegment:
			length = len(segment.value) + 2
		case ItemSegment:
			length = 2
		}
		if length > remaining {
			return true
		}
		remaining -= length
	}
	return false
}
