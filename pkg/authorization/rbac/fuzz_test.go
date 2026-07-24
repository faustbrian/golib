package rbac

import "testing"

func FuzzDecodeDocument(f *testing.F) {
	f.Add([]byte(`{"version":1,"roles":[],"permissions":[],"assignments":[]}`))
	f.Add([]byte(`{"version":1,"roles":null,"permissions":null,"assignments":null} trailing`))
	f.Add([]byte{0xff, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = DecodeDocument(encoded)
	})
}
