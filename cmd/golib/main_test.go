package main

import (
	"reflect"
	"testing"
)

func TestReferenceDurabilityDeclaresExternalServices(t *testing.T) {
	t.Parallel()

	got := requiredServices("pkg/service/integration/reference-durability")
	want := []string{"postgresql", "valkey"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requiredServices() = %v, want %v", got, want)
	}
}
