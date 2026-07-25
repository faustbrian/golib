package interfaceplacement

import "testing"

func TestIsValueInterfaceRejectsMissingTypeObject(t *testing.T) {
	t.Parallel()

	if isValueInterface(nil) {
		t.Fatal("isValueInterface(nil) = true")
	}
}
