package defaults

import (
	"reflect"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestCollectRejectsSchemaBeyondDepthLimitInternally(t *testing.T) {
	t.Parallel()

	type settings struct{ Value string }
	err := collect(
		reflect.TypeFor[settings](),
		make(map[string]any),
		make(map[string]config.Origin),
		"nested",
		maxSchemaDepth+1,
		make(map[reflect.Type]bool),
	)
	if err == nil {
		t.Fatal("collect() error = nil")
	}
}
