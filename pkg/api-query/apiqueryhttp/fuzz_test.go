package apiqueryhttp

import "testing"

func FuzzParse(f *testing.F) {
	f.Add("fields=id,status&sort=-created_at,id&page%5Bsize%5D=20")
	f.Add("fields=%FF&fields=id")
	f.Add("filter=%7B%22predicate%22%3A%7B%22name%22%3A%22id%22%7D%7D")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}
		_, _ = Parse(raw, 4096)
	})
}
