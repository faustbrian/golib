package otlp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigValidationRejectsInvalidTransportSettings(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*Config){
		"protocol": func(config *Config) { config.Protocol = Protocol("udp") },
		"endpoint": func(config *Config) { config.Endpoint = "" },
		"compression": func(config *Config) {
			config.Compression = Compression("brotli")
		},
		"timeout": func(config *Config) { config.Timeout = 0 },
		"retry":   func(config *Config) { config.Retry.MaxElapsedTime = -time.Second },
		"mTLS pair": func(config *Config) {
			config.TLS.CertificateFile = "client.pem"
		},
		"plaintext with CA": func(config *Config) {
			config.TLS.CAFile = "collector-ca.pem"
		},
		"plaintext with mTLS": func(config *Config) {
			config.TLS.CertificateFile = "client.pem"
			config.TLS.PrivateKeyFile = "client-key.pem"
		},
		"plaintext with server name": func(config *Config) {
			config.TLS.ServerName = "collector.internal"
		},
		"plaintext with skip verify": func(config *Config) {
			config.TLS.InsecureSkipVerify = true
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := validConfig(ProtocolGRPC)
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestExporterConstructionSupportsEverySignalAndProtocol(t *testing.T) {
	t.Parallel()

	for _, protocol := range []Protocol{ProtocolGRPC, ProtocolHTTPProtobuf} {
		protocol := protocol
		t.Run(string(protocol), func(t *testing.T) {
			t.Parallel()
			config := validConfig(protocol)
			if protocol == ProtocolHTTPProtobuf {
				config.URLPath = "/custom/v1/telemetry"
			}

			traceExporter, err := NewTraceExporter(context.Background(), config)
			if err != nil {
				t.Fatalf("NewTraceExporter() error = %v", err)
			}
			if err := traceExporter.Shutdown(context.Background()); err != nil {
				t.Fatalf("trace exporter Shutdown() error = %v", err)
			}

			metricExporter, err := NewMetricExporter(context.Background(), config)
			if err != nil {
				t.Fatalf("NewMetricExporter() error = %v", err)
			}
			if err := metricExporter.Shutdown(context.Background()); err != nil {
				t.Fatalf("metric exporter Shutdown() error = %v", err)
			}
		})
	}
}

func TestExporterConstructionRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	config := validConfig(ProtocolGRPC)
	config.Endpoint = ""
	if _, err := NewTraceExporter(context.Background(), config); err == nil {
		t.Fatal("NewTraceExporter() error = nil, want validation error")
	}
	if _, err := NewMetricExporter(context.Background(), config); err == nil {
		t.Fatal("NewMetricExporter() error = nil, want validation error")
	}
}

func TestSecureExporterConstruction(t *testing.T) {
	t.Parallel()

	for _, protocol := range []Protocol{ProtocolGRPC, ProtocolHTTPProtobuf} {
		config := validConfig(protocol)
		config.TLS.Insecure = false
		traceExporter, err := NewTraceExporter(context.Background(), config)
		if err != nil {
			t.Fatalf("NewTraceExporter(%q) error = %v", protocol, err)
		}
		_ = traceExporter.Shutdown(context.Background())
		metricExporter, err := NewMetricExporter(context.Background(), config)
		if err != nil {
			t.Fatalf("NewMetricExporter(%q) error = %v", protocol, err)
		}
		_ = metricExporter.Shutdown(context.Background())
	}
}

func TestTLSConfigLoadsCAAndClientCertificate(t *testing.T) {
	t.Parallel()

	certificateFile, keyFile := writeCertificatePair(t)
	config, err := buildTLSConfig(TLSConfig{
		CAFile:             certificateFile,
		CertificateFile:    certificateFile,
		PrivateKeyFile:     keyFile,
		ServerName:         "collector.internal",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if config.RootCAs == nil || len(config.Certificates) != 1 || config.ServerName != "collector.internal" {
		t.Fatalf("TLS config = %+v, want CA, client certificate, and server name", config)
	}
}

func TestTLSConfigReportsMalformedMaterial(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	invalidCA := filepath.Join(directory, "invalid-ca.pem")
	if err := os.WriteFile(invalidCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid CA: %v", err)
	}
	if _, err := buildTLSConfig(TLSConfig{CAFile: invalidCA}); err == nil {
		t.Fatal("buildTLSConfig() error = nil, want malformed CA error")
	}
	if _, err := buildTLSConfig(TLSConfig{
		CertificateFile: invalidCA,
		PrivateKeyFile:  invalidCA,
	}); err == nil {
		t.Fatal("buildTLSConfig() error = nil, want malformed client certificate error")
	}
}

func TestTLSConfigReportsSystemPoolFailure(t *testing.T) {
	t.Parallel()

	certificateFile, _ := writeCertificatePair(t)
	want := errors.New("system pool failed")
	if _, err := buildTLSConfigWithSystemPool(TLSConfig{CAFile: certificateFile}, func() (*x509.CertPool, error) {
		return nil, want
	}); !errors.Is(err, want) {
		t.Fatalf("buildTLSConfigWithSystemPool() error = %v, want %v", err, want)
	}
}

func TestExporterConstructionReportsTLSFiles(t *testing.T) {
	t.Parallel()

	config := validConfig(ProtocolGRPC)
	config.TLS.Insecure = false
	config.TLS.CAFile = "missing-ca.pem"

	if _, err := NewTraceExporter(context.Background(), config); err == nil {
		t.Fatal("NewTraceExporter() error = nil, want TLS file error")
	}
	if _, err := NewMetricExporter(context.Background(), config); err == nil {
		t.Fatal("NewMetricExporter() error = nil, want TLS file error")
	}
}

func validConfig(protocol Protocol) Config {
	return Config{
		Protocol:    protocol,
		Endpoint:    "localhost:4317",
		Headers:     map[string]string{"authorization": "test-token"},
		Compression: CompressionGZIP,
		TLS:         TLSConfig{Insecure: true},
		Retry: RetryConfig{
			Enabled:         true,
			InitialInterval: time.Millisecond,
			MaxInterval:     2 * time.Millisecond,
			MaxElapsedTime:  5 * time.Millisecond,
		},
		Timeout: 10 * time.Millisecond,
	}
}

func writeCertificatePair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2_048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "collector.internal"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	directory := t.TempDir()
	certificateFile := filepath.Join(directory, "certificate.pem")
	keyFile := filepath.Join(directory, "key.pem")
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certificateFile, certificatePEM, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certificateFile, keyFile
}
