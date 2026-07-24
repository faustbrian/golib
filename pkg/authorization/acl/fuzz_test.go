package acl

import "testing"

func FuzzDecodeDocument(f *testing.F) {
	f.Add([]byte(`{"version":1,"entries":[]}`))
	f.Add([]byte(`{"version":1,"unknown":true,"entries":[]}`))
	f.Add([]byte{0xff, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = DecodeDocument(encoded)
	})
}
