package environment

import (
	"reflect"
	"testing"
)

func TestCollectFieldsRejectsSchemaBeyondDepthLimitInternally(t *testing.T) {
	t.Parallel()

	type settings struct{ Value string }
	fields, err := collectFields(
		reflect.TypeFor[settings](),
		[]string{"nested"},
		nil,
		Options{},
		maxSchemaDepth+1,
		make(map[reflect.Type]bool),
	)
	if fields != nil || err == nil {
		t.Fatalf("collectFields() = %#v, %v", fields, err)
	}
}
