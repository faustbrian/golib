package analysis

import "testing"

func FuzzDecodeConfig(f *testing.F) {
	f.Add("version: 1\n")
	f.Add("version: 1\nunknown: true\n")
	f.Add("version: 1\n---\nversion: 1\n")
	f.Add("version: [\n")

	f.Fuzz(func(t *testing.T, contents string) {
		if len(contents) > maxConfigurationBytes {
			t.Skip()
		}
		_, _ = decodeConfig(
			[]byte(contents),
			"/repository",
			[]string{"security/no-unsafe"},
		)
	})
}
