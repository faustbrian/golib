package apiguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageReferencesFindsOnlyExportedNativeTypes(t *testing.T) {
	directory := t.TempDir()
	source := `package sample
import native "example.com/native-client"
type Public struct {
	Visible native.Client
	hidden native.Client
}
type private struct { Visible native.Client }
func (private) ExportedButPrivateReceiver() native.Result { panic("unused") }
func Exposed() native.Result { panic("unused") }
func hidden() native.Result { panic("unused") }
`
	require.NoError(t, os.WriteFile(filepath.Join(directory, "sample.go"), []byte(source), 0o600))

	references, err := PackageReferences(directory, map[string]string{
		"example.com/native-client": "native",
	})
	require.NoError(t, err)
	assert.Len(t, references, 2)
}

func TestPackageReferencesRejectsMalformedSource(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(directory, "broken.go"), []byte("package"), 0o600,
	))
	_, err := PackageReferences(directory, nil)
	assert.Error(t, err)
}
