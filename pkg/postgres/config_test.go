package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestParseConfigAppliesFiniteProductionDefaults(t *testing.T) {
	t.Parallel()

	config, err := ParseConfig(Config{
		DSN: "postgres://app:secret@localhost:5432/app?sslmode=disable",
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", config.MaxConns)
	}
	if config.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %s, want 1h", config.MaxConnLifetime)
	}
	if config.MaxConnLifetimeJitter != 5*time.Minute {
		t.Errorf("MaxConnLifetimeJitter = %s, want 5m", config.MaxConnLifetimeJitter)
	}
	if config.MaxConnIdleTime != 30*time.Minute {
		t.Errorf("MaxConnIdleTime = %s, want 30m", config.MaxConnIdleTime)
	}
	if config.HealthCheckPeriod != time.Minute {
		t.Errorf("HealthCheckPeriod = %s, want 1m", config.HealthCheckPeriod)
	}
	if config.PingTimeout != DefaultPingTimeout {
		t.Errorf("PingTimeout = %s, want %s", config.PingTimeout, DefaultPingTimeout)
	}
	if config.ConnConfig.ConnectTimeout != 5*time.Second {
		t.Errorf("ConnectTimeout = %s, want 5s", config.ConnConfig.ConnectTimeout)
	}
}

func TestParseConfigAcceptsRepresentativePostgreSQLDSNForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		dsn           string
		host          string
		password      string
		fallbackHosts []string
	}{
		{
			name:     "URL with percent-encoded credentials",
			dsn:      "postgres://app:p%40ss%3Aword@localhost:5432/app?sslmode=disable",
			host:     "localhost",
			password: "p@ss:word",
		},
		{
			name:     "keyword values with spaces",
			dsn:      "host=localhost user=app password='space secret' dbname=app sslmode=disable",
			host:     "localhost",
			password: "space secret",
		},
		{
			name:     "IPv6 URL",
			dsn:      "postgres://app:secret@[::1]:5432/app?sslmode=disable",
			host:     "::1",
			password: "secret",
		},
		{
			name: "Unix socket URL",
			dsn:  "postgres://app@/app?host=%2Fvar%2Frun%2Fpostgresql&sslmode=disable",
			host: "/var/run/postgresql",
		},
		{
			name:          "multi-host keyword values",
			dsn:           "host=primary,fallback user=app dbname=app sslmode=disable",
			host:          "primary",
			fallbackHosts: []string{"fallback"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config, err := ParseConfig(Config{DSN: tt.dsn})
			if err != nil {
				t.Fatalf("ParseConfig() error = %v", err)
			}
			if config.ConnConfig.Host != tt.host || config.ConnConfig.Password != tt.password {
				t.Fatalf(
					"connection fields = host %q password %q",
					config.ConnConfig.Host,
					config.ConnConfig.Password,
				)
			}
			if len(config.ConnConfig.Fallbacks) != len(tt.fallbackHosts) {
				t.Fatalf("fallback count = %d, want %d", len(config.ConnConfig.Fallbacks), len(tt.fallbackHosts))
			}
			for index, host := range tt.fallbackHosts {
				if config.ConnConfig.Fallbacks[index].Host != host {
					t.Fatalf("fallback %d host = %q, want %q", index, config.ConnConfig.Fallbacks[index].Host, host)
				}
			}
		})
	}
}

func TestParseConfigHonorsOverridesAndHook(t *testing.T) {
	t.Parallel()

	hookCalled := false
	config, err := ParseConfig(Config{
		DSN:                   "postgres://localhost/app?sslmode=disable",
		ConnectTimeout:        7 * time.Second,
		PingTimeout:           9 * time.Second,
		MaxConns:              24,
		MinConns:              3,
		MinIdleConns:          2,
		MaxConnLifetime:       2 * time.Hour,
		MaxConnLifetimeJitter: 10 * time.Minute,
		MaxConnIdleTime:       20 * time.Minute,
		HealthCheckPeriod:     30 * time.Second,
		Configure: func(config *PoolConfig) error {
			hookCalled = true
			config.ConnConfig.RuntimeParams["application_name"] = "worker"

			return nil
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if !hookCalled {
		t.Fatal("Configure hook was not called")
	}
	if config.MaxConns != 24 || config.MinConns != 3 || config.MinIdleConns != 2 {
		t.Fatalf("pool sizes = (%d, %d, %d), want (24, 3, 2)", config.MaxConns, config.MinConns, config.MinIdleConns)
	}
	if config.PingTimeout != 9*time.Second {
		t.Errorf("PingTimeout = %s, want 9s", config.PingTimeout)
	}
	if got := config.ConnConfig.RuntimeParams["application_name"]; got != "worker" {
		t.Errorf("application_name = %q, want worker", got)
	}
}

func TestParseConfigRejectsInvalidValuesWithoutLeakingDSN(t *testing.T) {
	t.Parallel()

	const password = "do-not-print-this-password"
	tests := []struct {
		name   string
		config Config
	}{
		{name: "empty DSN", config: Config{}},
		{
			name:   "invalid DSN",
			config: Config{DSN: "postgres://app:" + password + "@%zz/app"},
		},
		{
			name: "maximum connections",
			config: Config{
				DSN:      "postgres://app:" + password + "@localhost/app",
				MaxConns: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseConfig(tt.config)
			if err == nil {
				t.Fatal("ParseConfig() error = nil")
			}
			if strings.Contains(err.Error(), password) {
				t.Fatalf("error leaked password: %v", err)
			}
		})
	}
}

func TestParseConfigRejectsInconsistentPoolSizes(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(Config{
		DSN:      "postgres://localhost/app?sslmode=disable",
		MaxConns: 2,
		MinConns: 3,
	})
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
}

func TestParseConfigComposesNativeAndSessionInitializationHooks(t *testing.T) {
	t.Parallel()

	var calls []string
	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		Configure: func(config *PoolConfig) error {
			config.AfterConnect = func(context.Context, *pgx.Conn) error {
				calls = append(calls, "native")

				return nil
			}

			return nil
		},
		SessionInit: func(context.Context, *pgx.Conn) error {
			calls = append(calls, "session")

			return nil
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if err := config.AfterConnect(context.Background(), nil); err != nil {
		t.Fatalf("AfterConnect() error = %v", err)
	}
	if got := strings.Join(calls, ","); got != "native,session" {
		t.Fatalf("hook order = %q, want native,session", got)
	}
}

func TestParseConfigPreservesSessionInitializationFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("session setup failed")
	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		SessionInit: func(context.Context, *pgx.Conn) error {
			return sentinel
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	err = config.AfterConnect(context.Background(), nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("AfterConnect() error = %v, want wrapped sentinel", err)
	}
}

func TestParseConfigRejectsUnknownStartupPolicy(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(Config{
		DSN:           "postgres://localhost/app?sslmode=disable",
		StartupPolicy: StartupPolicy(99),
	})
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
}

func TestParseConfigAppliesTypedTLSOverride(t *testing.T) {
	t.Parallel()

	tlsConfig := &tls.Config{ServerName: "db.internal", MinVersion: tls.VersionTLS13}
	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		TLS: TLSConfig{
			Mode:   TLSRequire,
			Config: tlsConfig,
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.ConnConfig.TLSConfig == nil {
		t.Fatal("ConnConfig.TLSConfig = nil")
	}
	if config.ConnConfig.TLSConfig == tlsConfig {
		t.Fatal("ConnConfig.TLSConfig aliases caller configuration")
	}
	if config.ConnConfig.TLSConfig.ServerName != "db.internal" {
		t.Errorf("TLS server name = %q, want db.internal", config.ConnConfig.TLSConfig.ServerName)
	}
}

func TestParseConfigCopiesMutableTLSInputs(t *testing.T) {
	t.Parallel()

	roots := x509.NewCertPool()
	clients := x509.NewCertPool()
	tlsConfig := &tls.Config{
		RootCAs:      roots,
		ClientCAs:    clients,
		NextProtos:   []string{"postgres"},
		CipherSuites: []uint16{tls.TLS_AES_128_GCM_SHA256},
		Certificates: []tls.Certificate{{Certificate: [][]byte{{1, 2, 3}}}},
	}
	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		TLS: TLSConfig{Mode: TLSRequire, Config: tlsConfig},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	got := config.ConnConfig.TLSConfig
	if got.RootCAs == roots {
		t.Fatal("TLS RootCAs aliases caller pool")
	}
	if got.ClientCAs == clients {
		t.Fatal("TLS ClientCAs aliases caller pool")
	}
	tlsConfig.NextProtos[0] = "mutated"
	tlsConfig.CipherSuites[0] = tls.TLS_AES_256_GCM_SHA384
	tlsConfig.Certificates[0].Certificate[0][0] = 9
	if got.NextProtos[0] != "postgres" || got.CipherSuites[0] != tls.TLS_AES_128_GCM_SHA256 ||
		got.Certificates[0].Certificate[0][0] != 1 {
		t.Fatalf("parsed TLS configuration changed after caller mutation: %#v", got)
	}
}

func TestParseConfigCanExplicitlyDisableTLS(t *testing.T) {
	t.Parallel()

	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=require",
		TLS: TLSConfig{Mode: TLSDisable},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.ConnConfig.TLSConfig != nil {
		t.Fatal("ConnConfig.TLSConfig is enabled")
	}
	for index, fallback := range config.ConnConfig.Fallbacks {
		if fallback.TLSConfig != nil {
			t.Fatalf("fallback %d TLSConfig is enabled", index)
		}
	}
}

func TestParseConfigRejectsRequiredTLSWithoutConfiguration(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		TLS: TLSConfig{Mode: TLSRequire},
	})
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
}

func TestParseConfigCoversValidationAndSafeCauses(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("configuration rejected")
	_, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		Configure: func(*PoolConfig) error {
			return sentinel
		},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ParseConfig() error = %v, want sentinel", err)
	}
	var configErr *ConfigError
	if !errors.As(err, &configErr) || !errors.Is(configErr.Unwrap(), sentinel) {
		t.Fatalf("ParseConfig() error = %#v, want ConfigError cause", err)
	}

	for _, config := range []Config{
		{
			DSN:          "postgres://localhost/app?sslmode=disable",
			MaxConns:     1,
			MinIdleConns: 2,
		},
		{
			DSN: "postgres://localhost/app?sslmode=disable",
			TLS: TLSConfig{Mode: TLSMode(99)},
		},
	} {
		if _, err := ParseConfig(config); err == nil {
			t.Fatalf("ParseConfig(%+v) error = nil", config)
		}
	}
}

func TestParseConfigPreservesDSNTLSAndConfiguresFallbackHosts(t *testing.T) {
	t.Parallel()

	fromDSN, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=require",
		TLS: TLSConfig{Mode: TLSFromDSN},
	})
	if err != nil {
		t.Fatalf("ParseConfig(from DSN) error = %v", err)
	}
	if fromDSN.ConnConfig.TLSConfig == nil {
		t.Fatal("DSN TLS configuration was removed")
	}

	configured, err := ParseConfig(Config{
		DSN: "host=primary,fallback user=app dbname=app sslmode=disable",
		TLS: TLSConfig{Mode: TLSRequire, Config: &tls.Config{MinVersion: tls.VersionTLS13}},
	})
	if err != nil {
		t.Fatalf("ParseConfig(fallbacks) error = %v", err)
	}
	if configured.ConnConfig.TLSConfig.ServerName != "primary" {
		t.Fatalf("primary ServerName = %q", configured.ConnConfig.TLSConfig.ServerName)
	}
	if len(configured.ConnConfig.Fallbacks) == 0 ||
		configured.ConnConfig.Fallbacks[0].TLSConfig.ServerName != "fallback" {
		t.Fatalf("fallbacks = %#v", configured.ConnConfig.Fallbacks)
	}
}

func TestParseConfigDisablesTLSForFallbackHosts(t *testing.T) {
	t.Parallel()

	config, err := ParseConfig(Config{
		DSN: "host=primary,fallback user=app dbname=app sslmode=require",
		TLS: TLSConfig{Mode: TLSDisable},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if config.ConnConfig.TLSConfig != nil {
		t.Fatal("primary TLS remains enabled")
	}
	for index, fallback := range config.ConnConfig.Fallbacks {
		if fallback.TLSConfig != nil {
			t.Fatalf("fallback %d TLS remains enabled", index)
		}
	}
}

func TestNativeSessionHookFailureSkipsSessionInitialization(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("native hook failed")
	sessionCalled := false
	config, err := ParseConfig(Config{
		DSN: "postgres://localhost/app?sslmode=disable",
		Configure: func(config *PoolConfig) error {
			config.AfterConnect = func(context.Context, *pgx.Conn) error { return sentinel }

			return nil
		},
		SessionInit: func(context.Context, *pgx.Conn) error {
			sessionCalled = true

			return nil
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if err := config.AfterConnect(context.Background(), nil); !errors.Is(err, sentinel) {
		t.Fatalf("AfterConnect() error = %v, want sentinel", err)
	}
	if sessionCalled {
		t.Fatal("session initialization ran after native hook failure")
	}
}

func TestTrustedConfigurationHookPanicsPropagate(t *testing.T) {
	t.Parallel()

	t.Run("configure", func(t *testing.T) {
		const panicValue = "configure panic"
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Fatalf("recovered panic = %v", recovered)
			}
		}()

		_, _ = ParseConfig(Config{
			DSN: "postgres://localhost/app?sslmode=disable",
			Configure: func(*PoolConfig) error {
				panic(panicValue)
			},
		})
	})

	t.Run("native after connect", func(t *testing.T) {
		const panicValue = "native hook panic"
		sessionCalled := false
		config, err := ParseConfig(Config{
			DSN: "postgres://localhost/app?sslmode=disable",
			Configure: func(config *PoolConfig) error {
				config.AfterConnect = func(context.Context, *pgx.Conn) error {
					panic(panicValue)
				}

				return nil
			},
			SessionInit: func(context.Context, *pgx.Conn) error {
				sessionCalled = true

				return nil
			},
		})
		if err != nil {
			t.Fatalf("ParseConfig() error = %v", err)
		}
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Fatalf("recovered panic = %v", recovered)
			}
			if sessionCalled {
				t.Fatal("session initialization ran after native hook panic")
			}
		}()

		_ = config.AfterConnect(context.Background(), nil)
	})
}
