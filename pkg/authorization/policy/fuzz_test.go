package policy

import "testing"

func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"format":"authorization.policy/v1","revision":1,"algorithm":"deny-overrides","policies":[]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte{0xff, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = Decode(encoded)
	})
}
