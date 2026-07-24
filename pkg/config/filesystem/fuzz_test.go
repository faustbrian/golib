package filesystem

import (
	"context"
	"testing"
	"testing/fstest"
)

func FuzzFilesystemBoundary(f *testing.F) {
	f.Add("config.json", uint8(FormatAuto), []byte(`{"value":"safe"}`))
	f.Add("../config.yaml", uint8(FormatYAML), []byte("value: safe\n"))
	f.Add("/config.toml", uint8(FormatTOML), []byte(`value = "safe"`))
	f.Add("config\x00.json", uint8(255), []byte{0, 255})

	f.Fuzz(func(t *testing.T, path string, formatSelector uint8, data []byte) {
		if len(path) > 512 || len(data) > 8192 {
			t.Skip()
		}
		format := Format(formatSelector % uint8(FormatTOML+2))
		filesystem := fstest.MapFS{
			"config.json": &fstest.MapFile{Data: append([]byte(nil), data...), Mode: 0o600},
			"config.yaml": &fstest.MapFile{Data: append([]byte(nil), data...), Mode: 0o600},
			"config.toml": &fstest.MapFile{Data: append([]byte(nil), data...), Mode: 0o600},
		}
		source, err := FromFS(filesystem, path, Options{
			Name: "fuzz-filesystem", Format: format,
			Limits: Limits{MaxBytes: 8192, MaxDepth: 32, MaxKeys: 128},
		})
		if err != nil {
			return
		}
		_, _ = source.Load(context.Background())
	})
}
