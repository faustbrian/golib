package apiqueryrpc

import "testing"

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"fields":["id"],"page":{"mode":"cursor","size":10}}`))
	f.Add([]byte(`{"fields":[],"fields":["id"]}`))
	f.Add([]byte("\xff\x00{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		params, err := Parse(data, 4096)
		if err == nil {
			_ = params.Request()
		}
	})
}
