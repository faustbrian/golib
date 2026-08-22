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

func TestRabbitStreamModulesDeclareOwnedBrokerFixtures(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"pkg/rabbitstream/rabbitmq":          {"rabbitstream"},
		"pkg/outbox/adapters/gorabbitstream": {"rabbitstream-standalone"},
	}
	for directory, want := range tests {
		if got := requiredServices(directory); !reflect.DeepEqual(got, want) {
			t.Errorf("requiredServices(%q) = %v, want %v", directory, got, want)
		}
	}
}
