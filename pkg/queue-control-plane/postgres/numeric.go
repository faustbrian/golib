package postgres

import "math"

func int64FromUint64(value uint64) (int64, bool) {
	if value > math.MaxInt64 {
		return 0, false
	}

	return int64(value), true
}

func uint64FromInt64(value int64) (uint64, bool) {
	if value < 0 {
		return 0, false
	}

	return uint64(value), true
}

func uint32FromInt64(value int64) (uint32, bool) {
	if value < 0 || value > math.MaxUint32 {
		return 0, false
	}

	return uint32(value), true
}

func uint16FromInt64(value int64) (uint16, bool) {
	if value < 0 || value > math.MaxUint16 {
		return 0, false
	}

	return uint16(value), true
}

func uint16FromInt16(value int16) (uint16, bool) {
	if value < 0 {
		return 0, false
	}

	return uint16(value), true
}
