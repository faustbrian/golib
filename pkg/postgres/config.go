package postgres

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// DefaultConnectTimeout bounds each new PostgreSQL connection attempt.
	DefaultConnectTimeout = 5 * time.Second
	// DefaultAcquireTimeout bounds waiting for a pooled connection.
	DefaultAcquireTimeout = 5 * time.Second
	// DefaultPingTimeout bounds startup and readiness probes.
	DefaultPingTimeout = 2 * time.Second
	// DefaultShutdownTimeout bounds how long Close waits for borrowed connections.
	DefaultShutdownTimeout = 10 * time.Second
)

// PoolConfig is the native pgxpool configuration type. The alias makes hooks
// explicit while preserving direct access to every pgxpool option.
type PoolConfig = pgxpool.Config

// TLSMode controls whether typed TLS settings override the DSN.
type TLSMode uint8

const (
	// TLSFromDSN preserves pgx parsing of sslmode and related DSN settings.
	TLSFromDSN TLSMode = iota
	// TLSDisable explicitly disables TLS for every configured host.
	TLSDisable
	// TLSRequire requires the supplied tls.Config for every configured host.
	TLSRequire
)

// TLSConfig is an explicit TLS override. TLSRequire copies Config and its
// certificate pools, protocol slices, and certificate bytes before use.
// Callback, private-key, cache, random-source, clock, and writer values remain
// caller-owned collaborators and must be safe for concurrent use.
type TLSConfig struct {
	Mode   TLSMode
	Config *tls.Config
}

// StartupPolicy controls whether New proves connectivity before returning.
type StartupPolicy uint8

const (
	// StartupPing is the fail-fast default and pings PostgreSQL during New.
	StartupPing StartupPolicy = iota
	// StartupLazy returns after pool construction without opening a connection.
	StartupLazy
)

// Config defines safe, finite defaults for constructing a PostgreSQL pool.
// Zero values select the documented defaults. Negative sizes or durations are
// rejected rather than passed through to pgxpool.
type Config struct {
	DSN string

	ConnectTimeout  time.Duration
	AcquireTimeout  time.Duration
	PingTimeout     time.Duration
	ShutdownTimeout time.Duration

	MaxConns     int32
	MinConns     int32
	MinIdleConns int32

	MaxConnLifetime       time.Duration
	MaxConnLifetimeJitter time.Duration
	MaxConnIdleTime       time.Duration
	HealthCheckPeriod     time.Duration
	StartupPolicy         StartupPolicy
	TLS                   TLSConfig
	Observer              Observer

	// SessionInit runs for every newly established connection after any native
	// AfterConnect hook. Returning an error rejects that connection.
	SessionInit func(context.Context, *pgx.Conn) error

	// Configure receives the parsed native configuration after typed options
	// are applied and before the pool is created.
	Configure func(*PoolConfig) error
}

// ConfigError reports a configuration field without echoing the DSN or its
// credentials. Cause is retained only for errors outside DSN parsing.
type ConfigError struct {
	Field   string
	Problem string
	Cause   error
}

// Error implements error.
func (e *ConfigError) Error() string {
	return fmt.Sprintf("postgres: invalid %s: %s", e.Field, e.Problem)
}

// Unwrap exposes a safe underlying cause when one is available.
func (e *ConfigError) Unwrap() error {
	return e.Cause
}

// ParseConfig parses the DSN, applies finite defaults and overrides, validates
// pool invariants, and finally invokes Config.Configure.
func ParseConfig(input Config) (*PoolConfig, error) {
	if input.DSN == "" {
		return nil, configError("dsn", "must not be empty")
	}

	if field, ok := invalidNegativeField(input); ok {
		return nil, configError(field, "must not be negative")
	}
	if input.StartupPolicy > StartupLazy {
		return nil, configError("startup_policy", "is not recognized")
	}
	if input.TLS.Mode > TLSRequire {
		return nil, configError("tls.mode", "is not recognized")
	}
	if input.TLS.Mode == TLSRequire && input.TLS.Config == nil {
		return nil, configError("tls.config", "is required when TLS is required")
	}

	config, err := parsePoolConfig(input.DSN)
	if err != nil {
		return nil, configError("dsn", "could not be parsed")
	}

	config.ConnConfig.ConnectTimeout = valueOrDefault(input.ConnectTimeout, DefaultConnectTimeout)
	config.MaxConns = int32OrDefault(input.MaxConns, 10)
	config.MinConns = input.MinConns
	config.MinIdleConns = input.MinIdleConns
	config.MaxConnLifetime = valueOrDefault(input.MaxConnLifetime, time.Hour)
	config.MaxConnLifetimeJitter = valueOrDefault(input.MaxConnLifetimeJitter, 5*time.Minute)
	config.MaxConnIdleTime = valueOrDefault(input.MaxConnIdleTime, 30*time.Minute)
	config.HealthCheckPeriod = valueOrDefault(input.HealthCheckPeriod, time.Minute)
	config.PingTimeout = valueOrDefault(input.PingTimeout, DefaultPingTimeout)
	applyTLSConfig(config, input.TLS)

	if config.MinConns > config.MaxConns {
		return nil, configError("min_conns", "must not exceed max_conns")
	}
	if config.MinIdleConns > config.MaxConns {
		return nil, configError("min_idle_conns", "must not exceed max_conns")
	}

	if input.Configure != nil {
		if err := input.Configure(config); err != nil {
			return nil, &ConfigError{
				Field:   "configure hook",
				Problem: "returned an error",
				Cause:   err,
			}
		}
	}

	composeSessionInit(config, input.SessionInit)

	return config, nil
}

func parsePoolConfig(dsn string) (config *PoolConfig, err error) {
	defer func() {
		if recover() != nil {
			config = nil
			err = errors.New("pgx rejected the connection string")
		}
	}()

	return pgxpool.ParseConfig(dsn)
}

func applyTLSConfig(config *PoolConfig, input TLSConfig) {
	switch input.Mode {
	case TLSFromDSN:
		return
	case TLSDisable:
		config.ConnConfig.TLSConfig = nil
		for _, fallback := range config.ConnConfig.Fallbacks {
			fallback.TLSConfig = nil
		}
	case TLSRequire:
		config.ConnConfig.TLSConfig = tlsConfigForHost(input.Config, config.ConnConfig.Host)
		for _, fallback := range config.ConnConfig.Fallbacks {
			fallback.TLSConfig = tlsConfigForHost(input.Config, fallback.Host)
		}
	}
}

func tlsConfigForHost(input *tls.Config, host string) *tls.Config {
	config := cloneTLSConfig(input)
	if config.ServerName == "" {
		config.ServerName = host
	}

	return config
}

func cloneTLSConfig(input *tls.Config) *tls.Config {
	config := input.Clone()
	if config.RootCAs != nil {
		config.RootCAs = config.RootCAs.Clone()
	}
	if config.ClientCAs != nil {
		config.ClientCAs = config.ClientCAs.Clone()
	}
	config.NextProtos = append([]string(nil), config.NextProtos...)
	config.CipherSuites = append([]uint16(nil), config.CipherSuites...)
	config.CurvePreferences = append([]tls.CurveID(nil), config.CurvePreferences...)
	config.EncryptedClientHelloConfigList = append(
		[]byte(nil),
		config.EncryptedClientHelloConfigList...,
	)
	config.Certificates = append([]tls.Certificate(nil), config.Certificates...)
	for index := range config.Certificates {
		certificate := &config.Certificates[index]
		certificate.Certificate = cloneByteSlices(certificate.Certificate)
		certificate.SupportedSignatureAlgorithms = append(
			[]tls.SignatureScheme(nil),
			certificate.SupportedSignatureAlgorithms...,
		)
		certificate.OCSPStaple = append([]byte(nil), certificate.OCSPStaple...)
		certificate.SignedCertificateTimestamps = cloneByteSlices(
			certificate.SignedCertificateTimestamps,
		)
		certificate.Leaf = nil
	}

	return config
}

func cloneByteSlices(input [][]byte) [][]byte {
	result := make([][]byte, len(input))
	for index := range input {
		result[index] = append([]byte(nil), input[index]...)
	}

	return result
}

func composeSessionInit(config *PoolConfig, sessionInit func(context.Context, *pgx.Conn) error) {
	if sessionInit == nil {
		return
	}

	nativeAfterConnect := config.AfterConnect
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if nativeAfterConnect != nil {
			if err := nativeAfterConnect(ctx, conn); err != nil {
				return err
			}
		}
		if err := sessionInit(ctx, conn); err != nil {
			return fmt.Errorf("postgres: initialize session: %w", err)
		}

		return nil
	}
}

func configError(field, problem string) error {
	return &ConfigError{Field: field, Problem: problem}
}

func valueOrDefault(value, defaultValue time.Duration) time.Duration {
	if value == 0 {
		return defaultValue
	}

	return value
}

func int32OrDefault(value, defaultValue int32) int32 {
	if value == 0 {
		return defaultValue
	}

	return value
}

func invalidNegativeField(config Config) (string, bool) {
	fields := []struct {
		name  string
		value int64
	}{
		{name: "connect_timeout", value: int64(config.ConnectTimeout)},
		{name: "acquire_timeout", value: int64(config.AcquireTimeout)},
		{name: "ping_timeout", value: int64(config.PingTimeout)},
		{name: "shutdown_timeout", value: int64(config.ShutdownTimeout)},
		{name: "max_conns", value: int64(config.MaxConns)},
		{name: "min_conns", value: int64(config.MinConns)},
		{name: "min_idle_conns", value: int64(config.MinIdleConns)},
		{name: "max_conn_lifetime", value: int64(config.MaxConnLifetime)},
		{name: "max_conn_lifetime_jitter", value: int64(config.MaxConnLifetimeJitter)},
		{name: "max_conn_idle_time", value: int64(config.MaxConnIdleTime)},
		{name: "health_check_period", value: int64(config.HealthCheckPeriod)},
	}

	for _, field := range fields {
		if field.value < 0 {
			return field.name, true
		}
	}

	return "", false
}
