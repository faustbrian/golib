package safeuse

import "encoding/binary"

func Decode(value []byte) uint32 {
	return binary.BigEndian.Uint32(value)
}
