package main

import (
	"errors"
	"strconv"
	"strings"
)

const defaultAccessDocumentSize int64 = 1 << 20

// ErrInvalidRuntimeConfiguration is deliberately stable and secret-safe.
var ErrInvalidRuntimeConfiguration = errors.New("queue-control-plane: invalid runtime configuration")

// Config contains the bounded process configuration loaded at startup.
type Config struct {
	ListenAddress            string
	DatabaseURL              string
	AccessDocumentPath       string
	AccessDocumentSize       int64
	AllowedOrigins           []string
	RunMigrations            bool
	UIEnabled                bool
	MigrateOnly              bool
	RetentionOnly            bool
	RetentionDocumentPath    string
	RetentionDocumentSize    int64
	KubernetesTenantPath     string
	KubernetesTenantSize     int64
	ManagementTenantPath     string
	ManagementTenantSize     int64
	TelemetryEnabled         bool
	TelemetryEndpoint        string
	TelemetryProtocol        string
	TelemetryInsecure        bool
	TelemetryEnvironment     string
	TelemetryInstance        string
	TelemetryTrustedInbound  bool
	TelemetryCAFile          string
	TelemetryCertificateFile string
	TelemetryPrivateKeyFile  string
	TelemetryServerName      string
}

// LoadConfig reads and validates the process environment without formatting
// secret-bearing values into errors.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, ErrInvalidRuntimeConfiguration
	}

	config := Config{
		DatabaseURL:        strings.TrimSpace(getenv("DATABASE_URL")),
		AccessDocumentPath: strings.TrimSpace(getenv("QUEUE_CONTROL_ACCESS_FILE")),
		AccessDocumentSize: defaultAccessDocumentSize,
	}
	if config.DatabaseURL == "" {
		return Config{}, ErrInvalidRuntimeConfiguration
	}
	if encoded := getenv("QUEUE_CONTROL_MIGRATE_ONLY"); encoded != "" {
		value, err := strconv.ParseBool(encoded)
		if err != nil {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.MigrateOnly = value
	}
	if encoded := getenv("QUEUE_CONTROL_RETENTION_ONLY"); encoded != "" {
		value, err := strconv.ParseBool(encoded)
		if err != nil {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.RetentionOnly = value
	}
	if config.MigrateOnly && config.RetentionOnly {
		return Config{}, ErrInvalidRuntimeConfiguration
	}
	uiEnabled, ok := parseOptionalBool(getenv("QUEUE_CONTROL_UI_ENABLED"))
	if !ok || (uiEnabled && (config.MigrateOnly || config.RetentionOnly)) {
		return Config{}, ErrInvalidRuntimeConfiguration
	}
	config.UIEnabled = uiEnabled
	if !config.MigrateOnly && !config.RetentionOnly && config.AccessDocumentPath == "" {
		return Config{}, ErrInvalidRuntimeConfiguration
	}

	address := getenv("QUEUE_CONTROL_LISTEN_ADDRESS")
	if address == "" {
		config.ListenAddress = ":8080"
	} else {
		config.ListenAddress = strings.TrimSpace(address)
		if config.ListenAddress == "" {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
	}

	if encoded := getenv("QUEUE_CONTROL_ACCESS_MAX_BYTES"); encoded != "" {
		value, err := strconv.ParseInt(encoded, 10, 64)
		if err != nil || value < 1 || value > defaultAccessDocumentSize {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.AccessDocumentSize = value
	}

	if encoded := getenv("QUEUE_CONTROL_RUN_MIGRATIONS"); encoded != "" {
		value, err := strconv.ParseBool(encoded)
		if err != nil {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.RunMigrations = value
	}
	retentionPath := getenv("QUEUE_CONTROL_RETENTION_FILE")
	retentionSize := getenv("QUEUE_CONTROL_RETENTION_MAX_BYTES")
	if config.RetentionOnly {
		config.RetentionDocumentPath = strings.TrimSpace(retentionPath)
		if config.RetentionDocumentPath == "" || config.RunMigrations {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.RetentionDocumentSize = defaultAccessDocumentSize
		if retentionSize != "" {
			value, err := strconv.ParseInt(retentionSize, 10, 64)
			if err != nil || value < 1 || value > defaultAccessDocumentSize {
				return Config{}, ErrInvalidRuntimeConfiguration
			}
			config.RetentionDocumentSize = value
		}
	} else if retentionPath != "" || retentionSize != "" {
		return Config{}, ErrInvalidRuntimeConfiguration
	}

	tenantPath := getenv("QUEUE_CONTROL_KUBERNETES_TENANTS_FILE")
	tenantSize := getenv("QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES")
	if tenantPath == "" {
		if tenantSize != "" {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
	} else {
		config.KubernetesTenantPath = strings.TrimSpace(tenantPath)
		if config.KubernetesTenantPath == "" {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.KubernetesTenantSize = defaultAccessDocumentSize
		if tenantSize != "" {
			value, err := strconv.ParseInt(tenantSize, 10, 64)
			if err != nil || value < 1 || value > defaultAccessDocumentSize {
				return Config{}, ErrInvalidRuntimeConfiguration
			}
			config.KubernetesTenantSize = value
		}
	}
	managementPath := getenv("QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE")
	managementSize := getenv("QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES")
	if managementPath == "" {
		if managementSize != "" {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
	} else {
		config.ManagementTenantPath = strings.TrimSpace(managementPath)
		if config.ManagementTenantPath == "" {
			return Config{}, ErrInvalidRuntimeConfiguration
		}
		config.ManagementTenantSize = defaultAccessDocumentSize
		if managementSize != "" {
			value, err := strconv.ParseInt(managementSize, 10, 64)
			if err != nil || value < 1 || value > defaultAccessDocumentSize {
				return Config{}, ErrInvalidRuntimeConfiguration
			}
			config.ManagementTenantSize = value
		}
	}

	origins, ok := parseOrigins(getenv("QUEUE_CONTROL_ALLOWED_ORIGINS"))
	if !ok {
		return Config{}, ErrInvalidRuntimeConfiguration
	}
	config.AllowedOrigins = origins
	if !loadTelemetryConfig(&config, getenv) {
		return Config{}, ErrInvalidRuntimeConfiguration
	}
	if config.RetentionOnly && config.TelemetryEnabled {
		return Config{}, ErrInvalidRuntimeConfiguration
	}

	return config, nil
}

func loadTelemetryConfig(config *Config, getenv func(string) string) bool {
	enabled, ok := parseOptionalBool(getenv("QUEUE_CONTROL_TELEMETRY_ENABLED"))
	if !ok {
		return false
	}
	endpoint := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_ENDPOINT"))
	protocol := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_PROTOCOL"))
	insecureValue := getenv("QUEUE_CONTROL_OTLP_INSECURE")
	environment := strings.TrimSpace(getenv("QUEUE_CONTROL_TELEMETRY_ENVIRONMENT"))
	instance := strings.TrimSpace(getenv("QUEUE_CONTROL_TELEMETRY_INSTANCE"))
	trustedValue := getenv("QUEUE_CONTROL_TRUST_INBOUND_TRACE_CONTEXT")
	caFile := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_CA_FILE"))
	certificateFile := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_CERTIFICATE_FILE"))
	privateKeyFile := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_PRIVATE_KEY_FILE"))
	serverName := strings.TrimSpace(getenv("QUEUE_CONTROL_OTLP_SERVER_NAME"))
	if !enabled {
		return endpoint == "" && protocol == "" && insecureValue == "" &&
			environment == "" && instance == "" && trustedValue == "" &&
			caFile == "" && certificateFile == "" && privateKeyFile == "" &&
			serverName == ""
	}
	if endpoint == "" {
		return false
	}
	if protocol == "" {
		protocol = "grpc"
	}
	if protocol != "grpc" && protocol != "http/protobuf" {
		return false
	}
	insecure, ok := parseOptionalBool(insecureValue)
	if !ok {
		return false
	}
	trusted, ok := parseOptionalBool(trustedValue)
	if !ok {
		return false
	}
	if (certificateFile == "") != (privateKeyFile == "") {
		return false
	}
	if insecure && (caFile != "" || certificateFile != "" || serverName != "") {
		return false
	}

	config.TelemetryEnabled = true
	config.TelemetryEndpoint = endpoint
	config.TelemetryProtocol = protocol
	config.TelemetryInsecure = insecure
	config.TelemetryEnvironment = environment
	config.TelemetryInstance = instance
	config.TelemetryTrustedInbound = trusted
	config.TelemetryCAFile = caFile
	config.TelemetryCertificateFile = certificateFile
	config.TelemetryPrivateKeyFile = privateKeyFile
	config.TelemetryServerName = serverName

	return true
}

func parseOptionalBool(encoded string) (bool, bool) {
	if encoded == "" {
		return false, true
	}
	value, err := strconv.ParseBool(encoded)

	return value, err == nil
}

func parseOrigins(encoded string) ([]string, bool) {
	if encoded == "" {
		return nil, true
	}
	parts := strings.Split(encoded, ",")
	origins := make([]string, len(parts))
	for index, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" {
			return nil, false
		}
		origins[index] = origin
	}

	return origins, true
}
