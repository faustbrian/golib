package msgpackwire

import (
	"bytes"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestDecodeNumericMapRejectsTruncation(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string][]byte{
		"header":        {0xde},
		"missing key":   {0x81},
		"missing value": {0x81, 0xa1, 'a'},
	} {
		t.Run(name, func(t *testing.T) {
			decoder := msgpack.NewDecoder(bytes.NewReader(payload))
			if _, err := decodeNumericMap(decoder); err == nil {
				t.Fatal("decodeNumericMap() error = nil")
			}
		})
	}
}
