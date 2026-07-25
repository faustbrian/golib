package specification

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)

func TestVerifyPinnedInputsChecksEveryManifestDigest(t *testing.T) {
	t.Parallel()

	if err := VerifyPinnedInputs(os.DirFS("../..")); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPinnedInputsRejectsMissingMalformedAndChangedInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files fs.FS
		want  error
	}{
		{name: "missing manifest", files: fstest.MapFS{}, want: ErrManifestRead},
		{
			name:  "malformed manifest",
			files: fstest.MapFS{"specification/manifest.json": {Data: []byte(`{`)}},
			want:  ErrManifestInvalid,
		},
		{
			name: "changed input",
			files: fstest.MapFS{
				"specification/manifest.json": {Data: []byte(`{
					"openrpc":{"files":[{"path":"schema.json","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}
				}`)},
				"specification/openrpc-1.4.1/schema.json": {Data: []byte(`{}`)},
			},
			want: ErrManifestMismatch,
		},
	}
	for _, test := range tests {
		if err := VerifyPinnedInputs(test.files); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}
}
