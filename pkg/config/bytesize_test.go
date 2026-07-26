package config_test

import (
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestByteSizeParsesSupportedUnitsAndPlainBytes(t *testing.T) {
	t.Parallel()

	tests := map[string]config.ByteSize{
		"42": 42, "42B": 42,
		"2KiB": 2 * config.KiB, "2MiB": 2 * config.MiB, "2GiB": 2 * config.GiB,
		"2KB": 2 * config.KB, "2MB": 2 * config.MB, "2GB": 2 * config.GB,
		" 2 MiB ": 2 * config.MiB,
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var got config.ByteSize
			if err := got.UnmarshalText([]byte(input)); err != nil {
				t.Fatalf("UnmarshalText() error = %v", err)
			}
			if got != want {
				t.Fatalf("ByteSize = %d, want %d", got, want)
			}
		})
	}
}

func TestByteSizeRejectsNegativeMalformedAndOverflowValues(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "-1", "-1MiB", "value", "1TB", "9223372036854775807GiB"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			value := config.ByteSize(7)
			if err := value.UnmarshalText([]byte(input)); err == nil {
				t.Fatal("UnmarshalText() error = nil")
			}
			if value != 7 {
				t.Fatalf("ByteSize changed to %d after failure", value)
			}
		})
	}
}
