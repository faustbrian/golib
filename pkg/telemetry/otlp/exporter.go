package otlp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	metricexport "go.opentelemetry.io/otel/sdk/metric"
	traceexport "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

// NewTraceExporter constructs a standard OpenTelemetry span exporter.
func NewTraceExporter(ctx context.Context, config Config) (traceexport.SpanExporter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(config.TLS)
	if err != nil {
		return nil, err
	}
	if config.Protocol == ProtocolGRPC {
		options := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(config.Endpoint),
			otlptracegrpc.WithHeaders(cloneHeaders(config.Headers)),
			otlptracegrpc.WithTimeout(config.Timeout),
			otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig(config.Retry)),
		}
		if config.Compression == CompressionGZIP {
			options = append(options, otlptracegrpc.WithCompressor("gzip"))
		}
		if config.TLS.Insecure {
			options = append(options, otlptracegrpc.WithInsecure())
		} else {
			options = append(options, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
		}
		return otlptracegrpc.New(ctx, options...)
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(config.Endpoint),
		otlptracehttp.WithHeaders(cloneHeaders(config.Headers)),
		otlptracehttp.WithTimeout(config.Timeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig(config.Retry)),
	}
	if config.URLPath != "" {
		options = append(options, otlptracehttp.WithURLPath(config.URLPath))
	}
	if config.Compression == CompressionGZIP {
		options = append(options, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	if config.TLS.Insecure {
		options = append(options, otlptracehttp.WithInsecure())
	} else {
		options = append(options, otlptracehttp.WithTLSClientConfig(tlsConfig))
	}
	return otlptracehttp.New(ctx, options...)
}

// NewMetricExporter constructs a standard OpenTelemetry metric exporter.
func NewMetricExporter(ctx context.Context, config Config) (metricexport.Exporter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(config.TLS)
	if err != nil {
		return nil, err
	}
	if config.Protocol == ProtocolGRPC {
		options := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(config.Endpoint),
			otlpmetricgrpc.WithHeaders(cloneHeaders(config.Headers)),
			otlpmetricgrpc.WithTimeout(config.Timeout),
			otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig(config.Retry)),
		}
		if config.Compression == CompressionGZIP {
			options = append(options, otlpmetricgrpc.WithCompressor("gzip"))
		}
		if config.TLS.Insecure {
			options = append(options, otlpmetricgrpc.WithInsecure())
		} else {
			options = append(options, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
		}
		return otlpmetricgrpc.New(ctx, options...)
	}

	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(config.Endpoint),
		otlpmetrichttp.WithHeaders(cloneHeaders(config.Headers)),
		otlpmetrichttp.WithTimeout(config.Timeout),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig(config.Retry)),
	}
	if config.URLPath != "" {
		options = append(options, otlpmetrichttp.WithURLPath(config.URLPath))
	}
	if config.Compression == CompressionGZIP {
		options = append(options, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
	}
	if config.TLS.Insecure {
		options = append(options, otlpmetrichttp.WithInsecure())
	} else {
		options = append(options, otlpmetrichttp.WithTLSClientConfig(tlsConfig))
	}
	return otlpmetrichttp.New(ctx, options...)
}

func buildTLSConfig(config TLSConfig) (*tls.Config, error) {
	return buildTLSConfigWithSystemPool(config, x509.SystemCertPool)
}

func buildTLSConfigWithSystemPool(
	config TLSConfig,
	systemCertPool func() (*x509.CertPool, error),
) (*tls.Config, error) {
	if config.Insecure {
		return nil, nil
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Explicit compatibility setting.
	}
	if config.CAFile != "" {
		contents, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read OTLP CA file: %w", err)
		}
		pool, err := systemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool: %w", err)
		}
		if !pool.AppendCertsFromPEM(contents) {
			return nil, fmt.Errorf("parse OTLP CA file %q", config.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	if config.CertificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load OTLP client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}
