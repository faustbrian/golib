// Package otlp constructs explicitly configured OTLP trace and metric
// exporters without reading vendor-specific settings.
package otlp

import (
	"errors"
	"fmt"
	"time"
)

// Protocol selects an OTLP transport.
type Protocol string

const (
	// ProtocolGRPC exports OTLP over gRPC.
	ProtocolGRPC Protocol = "grpc"
	// ProtocolHTTPProtobuf exports OTLP protobuf payloads over HTTP.
	ProtocolHTTPProtobuf Protocol = "http/protobuf"
)

// Compression selects payload compression.
type Compression string

const (
	// CompressionNone disables compression.
	CompressionNone Compression = "none"
	// CompressionGZIP enables gzip compression.
	CompressionGZIP Compression = "gzip"
)

// Config completely describes an OTLP exporter transport.
type Config struct {
	Protocol    Protocol
	Endpoint    string
	URLPath     string
	Headers     map[string]string
	Compression Compression
	TLS         TLSConfig
	Retry       RetryConfig
	Timeout     time.Duration
}

// TLSConfig controls server verification and optional client certificates.
type TLSConfig struct {
	Insecure           bool
	CAFile             string
	CertificateFile    string
	PrivateKeyFile     string
	ServerName         string
	InsecureSkipVerify bool
}

// RetryConfig bounds retry backoff and elapsed time.
type RetryConfig struct {
	Enabled         bool
	InitialInterval time.Duration
	MaxInterval     time.Duration
	MaxElapsedTime  time.Duration
}

// Validate rejects incomplete, unsupported, or unbounded transport settings.
func (c Config) Validate() error {
	var errs []error
	if c.Protocol != ProtocolGRPC && c.Protocol != ProtocolHTTPProtobuf {
		errs = append(errs, fmt.Errorf("OTLP protocol %q is unsupported", c.Protocol))
	}
	if c.Endpoint == "" {
		errs = append(errs, errors.New("OTLP endpoint is required"))
	}
	if c.Compression != CompressionNone && c.Compression != CompressionGZIP {
		errs = append(errs, fmt.Errorf("OTLP compression %q is unsupported", c.Compression))
	}
	if c.Timeout <= 0 {
		errs = append(errs, errors.New("OTLP timeout must be positive"))
	}
	if c.Retry.Enabled && (c.Retry.InitialInterval <= 0 || c.Retry.MaxInterval <= 0 || c.Retry.MaxElapsedTime <= 0) {
		errs = append(errs, errors.New("OTLP retry intervals must be positive"))
	}
	if (c.TLS.CertificateFile == "") != (c.TLS.PrivateKeyFile == "") {
		errs = append(errs, errors.New("OTLP client certificate and private key must be configured together"))
	}
	if c.TLS.Insecure && (c.TLS.CAFile != "" || c.TLS.CertificateFile != "" ||
		c.TLS.PrivateKeyFile != "" || c.TLS.ServerName != "" || c.TLS.InsecureSkipVerify) {
		errs = append(errs, errors.New("OTLP plaintext mode cannot include TLS settings"))
	}
	return errors.Join(errs...)
}
