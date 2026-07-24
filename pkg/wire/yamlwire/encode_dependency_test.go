package yamlwire

import (
	"errors"
	"io"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"go.yaml.in/yaml/v4"
)

func TestEncodeClassifiesDumperConstructionFailure(t *testing.T) {
	t.Parallel()

	_, err := encode(
		map[string]string{"value": "bounded"},
		EncodeOptions{},
		func(io.Writer, ...yaml.Option) (*yaml.Dumper, error) {
			return nil, errors.New("dumper construction failed")
		},
	)
	if !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("error = %v, want wire.ErrValidation", err)
	}
}
