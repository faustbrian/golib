package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

func TestBuildResourceOwnsServiceIdentity(t *testing.T) {
	t.Parallel()

	config := DefaultConfig("orders", "1.2.3")
	config.Service.Namespace = "commerce"
	config.Service.Instance = "orders-7d9f"
	config.Environment = "production"
	config.Resource["region"] = "eu-north-1"

	res, err := BuildResource(context.Background(), config)
	if err != nil {
		t.Fatalf("BuildResource() error = %v", err)
	}

	want := map[attribute.Key]string{
		semconv.ServiceNameKey:               "orders",
		semconv.ServiceVersionKey:            "1.2.3",
		semconv.ServiceNamespaceKey:          "commerce",
		semconv.ServiceInstanceIDKey:         "orders-7d9f",
		semconv.DeploymentEnvironmentNameKey: "production",
		attribute.Key("region"):              "eu-north-1",
		semconv.TelemetrySDKLanguageKey:      "go",
		semconv.TelemetrySDKNameKey:          "opentelemetry",
	}
	for key, value := range want {
		got, ok := res.Set().Value(key)
		if !ok {
			t.Errorf("resource attribute %q is missing", key)
			continue
		}
		if got.AsString() != value {
			t.Errorf("resource attribute %q = %q, want %q", key, got.AsString(), value)
		}
	}
}

func TestConfigValidationRejectsReservedResourceAttributes(t *testing.T) {
	t.Parallel()

	config := DefaultConfig("orders", "1.2.3")
	config.Resource[string(semconv.ServiceNameKey)] = "attacker-controlled"

	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want reserved attribute error")
	}
}

func TestBuildResourceIgnoresReservedCustomAttributes(t *testing.T) {
	t.Parallel()

	config := DefaultConfig("orders", "1.2.3")
	config.Resource[string(semconv.ServiceNameKey)] = "untrusted"
	res, err := BuildResource(context.Background(), config)
	if err != nil {
		t.Fatalf("BuildResource() error = %v", err)
	}
	value, _ := res.Set().Value(semconv.ServiceNameKey)
	if value.AsString() != "orders" {
		t.Fatalf("service.name = %q, want owned identity", value.AsString())
	}
}
