package wire

import (
	"encoding/binary"
	"fmt"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

const headerSize = 21

var signature = [4]byte{'G', 'C', 'H', 1}

// Encode validates and serializes record within maxSize.
func Encode(record cache.Record, maxSize int) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	size := headerSize + len(record.Payload)
	if maxSize <= 0 || size > maxSize {
		return nil, fmt.Errorf("%w: encoded record length %d exceeds %d", cache.ErrValueTooLarge, size, maxSize)
	}
	encoded := make([]byte, size)
	copy(encoded[:4], signature[:])
	if record.Negative {
		encoded[4] = 1
	}
	binary.BigEndian.PutUint64(encoded[5:13], uint64(record.ExpiresAt.UnixNano()))
	binary.BigEndian.PutUint64(encoded[13:21], uint64(record.StaleAt.UnixNano()))
	copy(encoded[headerSize:], record.Payload)
	return encoded, nil
}

// Decode validates and deserializes one bounded record envelope.
func Decode(encoded []byte, maxSize int) (cache.Record, error) {
	if maxSize <= 0 || len(encoded) > maxSize {
		return cache.Record{}, fmt.Errorf("%w: encoded record length %d exceeds %d", cache.ErrValueTooLarge, len(encoded), maxSize)
	}
	if len(encoded) < headerSize {
		return cache.Record{}, fmt.Errorf("%w: record length %d is below header size", cache.ErrDecode, len(encoded))
	}
	if encoded[0] != signature[0] || encoded[1] != signature[1] || encoded[2] != signature[2] || encoded[3] != signature[3] {
		return cache.Record{}, cache.ErrSchemaMismatch
	}
	if encoded[4]&^byte(1) != 0 {
		return cache.Record{}, fmt.Errorf("%w: unknown record flags", cache.ErrDecode)
	}
	record := cache.Record{
		Payload: append([]byte(nil), encoded[headerSize:]...),
		// The uint64 conversion preserves the signed UnixNano bit pattern.
		ExpiresAt: time.Unix(0, int64(binary.BigEndian.Uint64(encoded[5:13]))),  // #nosec G115
		StaleAt:   time.Unix(0, int64(binary.BigEndian.Uint64(encoded[13:21]))), // #nosec G115
		Negative:  encoded[4]&1 != 0,
	}
	if err := record.Validate(); err != nil {
		return cache.Record{}, err
	}
	return record, nil
}
