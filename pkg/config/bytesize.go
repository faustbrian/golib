package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ByteSize is a count of bytes decoded from values such as 10MiB or 2GB.
type ByteSize int64

const (
	KiB ByteSize = 1 << 10
	MiB ByteSize = 1 << 20
	GiB ByteSize = 1 << 30
	KB  ByteSize = 1_000
	MB  ByteSize = 1_000_000
	GB  ByteSize = 1_000_000_000
)

// UnmarshalText parses an integer followed by an optional supported unit.
func (b *ByteSize) UnmarshalText(text []byte) error {
	input := strings.TrimSpace(string(text))
	units := []struct {
		suffix string
		scale  ByteSize
	}{
		{"KiB", KiB}, {"MiB", MiB}, {"GiB", GiB},
		{"KB", KB}, {"MB", MB}, {"GB", GB}, {"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(input, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(input, unit.suffix))
			value, err := strconv.ParseInt(number, 10, 64)
			if err != nil || value < 0 || value > int64(^uint64(0)>>1)/int64(unit.scale) {
				return fmt.Errorf("invalid byte size")
			}
			*b = ByteSize(value) * unit.scale
			return nil
		}
	}
	value, err := strconv.ParseInt(input, 10, 64)
	if err != nil || value < 0 {
		return fmt.Errorf("invalid byte size")
	}
	*b = ByteSize(value)
	return nil
}
