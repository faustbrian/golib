package httpclient

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTLSPolicyAppliesRootsServerNameAndPins(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "ok")
	}))
	t.Cleanup(server.Close)
	certificate := server.Certificate()
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	pin := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	policy, err := NewTLSPolicy(TLSOptions{
		RootCertificatesPEM: rootPEM,
		ServerName:          "example.com",
		SPKISHA256Pins:      [][sha256.Size]byte{pin},
	})
	if err != nil {
		t.Fatalf("construct TLS policy: %v", err)
	}
	rootPEM[0] = 'x'
	client, err := New(Config{TLS: policy})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	response, err := client.Do(mustTLSRequest(t, server.URL))
	if err != nil {
		t.Fatalf("TLS request: %v", err)
	}
	_ = response.Body.Close()
	transport := client.transport.(*http.Transport)
	if transport.TLSClientConfig.MinVersion != tlsMinimumVersion ||
		transport.TLSClientConfig.ServerName != "example.com" {
		t.Fatalf("TLS config = %#v", transport.TLSClientConfig)
	}
}

func TestTLSPolicyRejectsPinMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	certificate := server.Certificate()
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	policy, err := NewTLSPolicy(TLSOptions{
		RootCertificatesPEM: rootPEM,
		ServerName:          "example.com",
		SPKISHA256Pins:      [][sha256.Size]byte{{1}},
	})
	if err != nil {
		t.Fatalf("construct TLS policy: %v", err)
	}
	client, err := New(Config{TLS: policy})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.Do(mustTLSRequest(t, server.URL))
	if !errors.Is(err, ErrTLSPinMismatch) {
		t.Fatalf("pin mismatch error = %v", err)
	}
}

func mustTLSRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("construct TLS request: %v", err)
	}
	return request
}

func TestTLSPolicyValidationAndClientCertificate(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	certificate := server.TLS.Certificates[0]
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	privateDER, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	policy, err := NewTLSPolicy(TLSOptions{
		ClientCertificatePEM: leafPEM,
		ClientPrivateKeyPEM:  keyPEM,
	})
	if err != nil || len(policy.tlsConfig().Certificates) != 1 {
		t.Fatalf("client certificate policy = %#v, %v", policy, err)
	}

	for _, options := range []TLSOptions{
		{MinimumVersion: 1},
		{MinimumVersion: 0xffff},
		{ServerName: "bad name"},
		{RootCertificatesPEM: []byte("not pem")},
		{ClientCertificatePEM: leafPEM},
		{ClientPrivateKeyPEM: keyPEM},
		{ClientCertificatePEM: []byte("bad"), ClientPrivateKeyPEM: keyPEM},
	} {
		if _, err := NewTLSPolicy(options); !errors.Is(err, ErrInvalidTLSPolicy) {
			t.Fatalf("invalid TLS options %#v error = %v", options, err)
		}
	}
	if _, err := New(Config{TLS: policy, Transport: roundTripFunc(nil)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("custom transport TLS error = %v", err)
	}
}
