package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestLoadConfigRequiresSecretBearingLocations(t *testing.T) {
	t.Parallel()

	for name, environment := range map[string]map[string]string{
		"database":        {"QUEUE_CONTROL_ACCESS_FILE": "/run/secrets/access.json"},
		"access document": {"DATABASE_URL": "postgres://secret@database/control"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config, err := LoadConfig(mapEnvironment(environment))
			if !errors.Is(err, ErrInvalidRuntimeConfiguration) || !reflect.DeepEqual(config, Config{}) {
				t.Fatalf("LoadConfig() = (%+v, %v), want zero config and stable error", config, err)
			}
			if err != nil && err.Error() != ErrInvalidRuntimeConfiguration.Error() {
				t.Fatalf("error = %q, want secret-safe stable error", err)
			}
		})
	}
}

func TestLoadConfigAppliesProductionDefaults(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapEnvironment(map[string]string{
		"DATABASE_URL":              "postgres://database/control",
		"QUEUE_CONTROL_ACCESS_FILE": "/run/secrets/access.json",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := Config{
		ListenAddress:      ":8080",
		DatabaseURL:        "postgres://database/control",
		AccessDocumentPath: "/run/secrets/access.json",
		AccessDocumentSize: 1 << 20,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadConfig() = %+v, want %+v", config, want)
	}
}

func TestLoadConfigAllowsMigrationOnlyWithoutAccessDocument(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapEnvironment(map[string]string{
		"DATABASE_URL":                   "postgres://database/control",
		"QUEUE_CONTROL_MIGRATE_ONLY":     "true",
		"QUEUE_CONTROL_LISTEN_ADDRESS":   "127.0.0.1:9090",
		"QUEUE_CONTROL_ALLOWED_ORIGINS":  "https://control.example",
		"QUEUE_CONTROL_RUN_MIGRATIONS":   "false",
		"QUEUE_CONTROL_ACCESS_MAX_BYTES": "2048",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := Config{
		ListenAddress:      "127.0.0.1:9090",
		DatabaseURL:        "postgres://database/control",
		AccessDocumentSize: 2048,
		AllowedOrigins:     []string{"https://control.example"},
		MigrateOnly:        true,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadConfig() = %+v, want %+v", config, want)
	}
}

func TestLoadConfigAllowsRetentionOnlyWithoutServingConfiguration(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapEnvironment(map[string]string{
		"DATABASE_URL":                      "postgres://database/control",
		"QUEUE_CONTROL_RETENTION_ONLY":      "true",
		"QUEUE_CONTROL_RETENTION_FILE":      "/etc/control/retention.json",
		"QUEUE_CONTROL_RETENTION_MAX_BYTES": "2048",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := Config{
		ListenAddress:         ":8080",
		DatabaseURL:           "postgres://database/control",
		AccessDocumentSize:    1 << 20,
		RetentionOnly:         true,
		RetentionDocumentPath: "/etc/control/retention.json",
		RetentionDocumentSize: 2048,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadConfig() = %+v, want %+v", config, want)
	}
}

func TestLoadConfigParsesExplicitOptions(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(mapEnvironment(map[string]string{
		"DATABASE_URL":                               "postgres://database/control",
		"QUEUE_CONTROL_ACCESS_FILE":                  "/run/secrets/access.json",
		"QUEUE_CONTROL_LISTEN_ADDRESS":               "127.0.0.1:9090",
		"QUEUE_CONTROL_ACCESS_MAX_BYTES":             "2048",
		"QUEUE_CONTROL_ALLOWED_ORIGINS":              "https://one.example, https://two.example",
		"QUEUE_CONTROL_RUN_MIGRATIONS":               "true",
		"QUEUE_CONTROL_UI_ENABLED":                   "true",
		"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "/etc/control/tenants.json",
		"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "4096",
		"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "/etc/control/management.json",
		"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "8192",
		"QUEUE_CONTROL_TELEMETRY_ENABLED":            "true",
		"QUEUE_CONTROL_OTLP_ENDPOINT":                "collector.telemetry.svc:4317",
		"QUEUE_CONTROL_OTLP_PROTOCOL":                "grpc",
		"QUEUE_CONTROL_OTLP_INSECURE":                "true",
		"QUEUE_CONTROL_TELEMETRY_ENVIRONMENT":        "production",
		"QUEUE_CONTROL_TELEMETRY_INSTANCE":           "control-plane-1",
		"QUEUE_CONTROL_TRUST_INBOUND_TRACE_CONTEXT":  "true",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := Config{
		ListenAddress:           "127.0.0.1:9090",
		DatabaseURL:             "postgres://database/control",
		AccessDocumentPath:      "/run/secrets/access.json",
		AccessDocumentSize:      2048,
		AllowedOrigins:          []string{"https://one.example", "https://two.example"},
		RunMigrations:           true,
		UIEnabled:               true,
		KubernetesTenantPath:    "/etc/control/tenants.json",
		KubernetesTenantSize:    4096,
		ManagementTenantPath:    "/etc/control/management.json",
		ManagementTenantSize:    8192,
		TelemetryEnabled:        true,
		TelemetryEndpoint:       "collector.telemetry.svc:4317",
		TelemetryProtocol:       "grpc",
		TelemetryInsecure:       true,
		TelemetryEnvironment:    "production",
		TelemetryInstance:       "control-plane-1",
		TelemetryTrustedInbound: true,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadConfig() = %+v, want %+v", config, want)
	}
}

func TestLoadConfigRejectsMalformedBoundsAndFlags(t *testing.T) {
	t.Parallel()

	for name, values := range map[string]map[string]string{
		"nil environment":           nil,
		"blank address":             {"QUEUE_CONTROL_LISTEN_ADDRESS": " "},
		"zero access bound":         {"QUEUE_CONTROL_ACCESS_MAX_BYTES": "0"},
		"oversized access bound":    {"QUEUE_CONTROL_ACCESS_MAX_BYTES": "1048577"},
		"invalid access bound":      {"QUEUE_CONTROL_ACCESS_MAX_BYTES": "many"},
		"invalid migration flag":    {"QUEUE_CONTROL_RUN_MIGRATIONS": "sometimes"},
		"invalid UI flag":           {"QUEUE_CONTROL_UI_ENABLED": "sometimes"},
		"invalid migrate-only flag": {"QUEUE_CONTROL_MIGRATE_ONLY": "sometimes"},
		"invalid retention flag":    {"QUEUE_CONTROL_RETENTION_ONLY": "sometimes"},
		"conflicting one-shot modes": {
			"QUEUE_CONTROL_MIGRATE_ONLY":   "true",
			"QUEUE_CONTROL_RETENTION_ONLY": "true",
		},
		"retention without file": {"QUEUE_CONTROL_RETENTION_ONLY": "true"},
		"retention file without mode": {
			"QUEUE_CONTROL_RETENTION_FILE": "/etc/control/retention.json",
		},
		"retention bound without mode": {"QUEUE_CONTROL_RETENTION_MAX_BYTES": "2048"},
		"invalid retention bound": {
			"QUEUE_CONTROL_RETENTION_ONLY":      "true",
			"QUEUE_CONTROL_RETENTION_FILE":      "/etc/control/retention.json",
			"QUEUE_CONTROL_RETENTION_MAX_BYTES": "many",
		},
		"retention with migrations": {
			"QUEUE_CONTROL_RETENTION_ONLY": "true",
			"QUEUE_CONTROL_RETENTION_FILE": "/etc/control/retention.json",
			"QUEUE_CONTROL_RUN_MIGRATIONS": "true",
		},
		"retention with telemetry": {
			"QUEUE_CONTROL_RETENTION_ONLY":    "true",
			"QUEUE_CONTROL_RETENTION_FILE":    "/etc/control/retention.json",
			"QUEUE_CONTROL_TELEMETRY_ENABLED": "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":     "collector:4317",
		},
		"invalid telemetry flag":     {"QUEUE_CONTROL_TELEMETRY_ENABLED": "sometimes"},
		"telemetry without endpoint": {"QUEUE_CONTROL_TELEMETRY_ENABLED": "true"},
		"endpoint without telemetry": {"QUEUE_CONTROL_OTLP_ENDPOINT": "collector:4317"},
		"invalid telemetry protocol": {
			"QUEUE_CONTROL_TELEMETRY_ENABLED": "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":     "collector:4317",
			"QUEUE_CONTROL_OTLP_PROTOCOL":     "udp",
		},
		"invalid telemetry insecure flag": {
			"QUEUE_CONTROL_TELEMETRY_ENABLED": "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":     "collector:4317",
			"QUEUE_CONTROL_OTLP_INSECURE":     "sometimes",
		},
		"invalid inbound trace flag": {
			"QUEUE_CONTROL_TELEMETRY_ENABLED":           "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":               "collector:4317",
			"QUEUE_CONTROL_TRUST_INBOUND_TRACE_CONTEXT": "sometimes",
		},
		"telemetry certificate without key": {
			"QUEUE_CONTROL_TELEMETRY_ENABLED":     "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":         "collector:4317",
			"QUEUE_CONTROL_OTLP_CERTIFICATE_FILE": "/run/certs/client.pem",
		},
		"plaintext telemetry with TLS": {
			"QUEUE_CONTROL_TELEMETRY_ENABLED": "true",
			"QUEUE_CONTROL_OTLP_ENDPOINT":     "collector:4317",
			"QUEUE_CONTROL_OTLP_INSECURE":     "true",
			"QUEUE_CONTROL_OTLP_CA_FILE":      "/run/certs/ca.pem",
		},
		"empty origin":              {"QUEUE_CONTROL_ALLOWED_ORIGINS": "https://one.example,,https://two.example"},
		"tenant bound without file": {"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "4096"},
		"blank tenant file":         {"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE": "   "},
		"zero tenant bound": {
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "/etc/control/tenants.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "0",
		},
		"oversized tenant bound": {
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "/etc/control/tenants.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "1048577",
		},
		"invalid tenant bound": {
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "/etc/control/tenants.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "many",
		},
		"management bound without file": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "4096",
		},
		"blank management file": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE": "   ",
		},
		"zero management bound": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "/etc/control/management.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "0",
		},
		"oversized management bound": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "/etc/control/management.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "1048577",
		},
		"invalid management bound": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "/etc/control/management.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "many",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var getenv func(string) string
			if values != nil {
				values["DATABASE_URL"] = "postgres://database/control"
				values["QUEUE_CONTROL_ACCESS_FILE"] = "/run/secrets/access.json"
				getenv = mapEnvironment(values)
			}
			config, err := LoadConfig(getenv)
			if !errors.Is(err, ErrInvalidRuntimeConfiguration) || !reflect.DeepEqual(config, Config{}) {
				t.Fatalf("LoadConfig() = (%+v, %v), want zero config and stable error", config, err)
			}
		})
	}
}

func mapEnvironment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}
