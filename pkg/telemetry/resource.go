package telemetry

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

var reservedResourceKeys = map[string]struct{}{
	string(semconv.ServiceNameKey):               {},
	string(semconv.ServiceVersionKey):            {},
	string(semconv.ServiceNamespaceKey):          {},
	string(semconv.ServiceInstanceIDKey):         {},
	string(semconv.DeploymentEnvironmentNameKey): {},
	string(semconv.TelemetrySDKLanguageKey):      {},
	string(semconv.TelemetrySDKNameKey):          {},
	string(semconv.TelemetrySDKVersionKey):       {},
}

// BuildResource constructs the resource used by every enabled signal. Service
// identity always wins over custom attributes.
func BuildResource(ctx context.Context, config Config) (*resource.Resource, error) {
	attributes := make([]attribute.KeyValue, 0, len(config.Resource)+5)
	attributes = append(
		attributes,
		semconv.ServiceName(config.Service.Name),
		semconv.TelemetrySDKLanguageGo,
		semconv.TelemetrySDKName("opentelemetry"),
		semconv.TelemetrySDKVersion(otel.Version()),
	)
	if config.Service.Version != "" {
		attributes = append(attributes, semconv.ServiceVersion(config.Service.Version))
	}
	if config.Service.Namespace != "" {
		attributes = append(attributes, semconv.ServiceNamespace(config.Service.Namespace))
	}
	if config.Service.Instance != "" {
		attributes = append(attributes, semconv.ServiceInstanceID(config.Service.Instance))
	}
	if config.Environment != "" {
		attributes = append(attributes, semconv.DeploymentEnvironmentNameKey.String(config.Environment))
	}

	keys := make([]string, 0, len(config.Resource))
	for key := range config.Resource {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, reserved := reservedResourceKeys[key]; reserved {
			continue
		}
		attributes = append(attributes, attribute.String(key, config.Resource[key]))
	}

	return resource.New(
		ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attributes...),
	)
}
