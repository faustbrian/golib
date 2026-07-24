package httpclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

const tlsMinimumVersion = tls.VersionTLS12

var (
	// ErrInvalidTLSPolicy indicates malformed TLS roots, identity, or pins.
	ErrInvalidTLSPolicy = errors.New("invalid HTTP TLS policy")
	// ErrTLSPinMismatch indicates that no configured SPKI pin matched the peer.
	ErrTLSPinMismatch = errors.New("HTTP TLS public key pin mismatch")
)

// TLSOptions configures immutable TLS trust and peer identity policy. Empty
// roots use the platform trust store. Zero minimum version selects TLS 1.2.
type TLSOptions struct {
	MinimumVersion       uint16
	RootCertificatesPEM  []byte
	ServerName           string
	ClientCertificatePEM []byte
	ClientPrivateKeyPEM  []byte
	SPKISHA256Pins       [][sha256.Size]byte
}

// TLSPolicy is an immutable standard-transport TLS configuration.
type TLSPolicy struct {
	config *tls.Config
	pins   [][sha256.Size]byte
}

// NewTLSPolicy validates and snapshots TLS trust, identity, and pin policy.
func NewTLSPolicy(options TLSOptions) (*TLSPolicy, error) {
	minimum := options.MinimumVersion
	if minimum == 0 {
		minimum = tlsMinimumVersion
	}
	if minimum != tls.VersionTLS12 && minimum != tls.VersionTLS13 {
		return nil, fmt.Errorf("%w: minimum version is unsupported", ErrInvalidTLSPolicy)
	}
	serverName := ""
	if options.ServerName != "" {
		var err error
		serverName, err = normalizeEgressHost(options.ServerName)
		if err != nil {
			return nil, fmt.Errorf("%w: server name is malformed", ErrInvalidTLSPolicy)
		}
	}
	config := &tls.Config{MinVersion: minimum, ServerName: serverName}
	if len(options.RootCertificatesPEM) > 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(append([]byte(nil), options.RootCertificatesPEM...)) {
			return nil, fmt.Errorf("%w: root certificates are malformed", ErrInvalidTLSPolicy)
		}
		config.RootCAs = roots
	}
	hasCertificate := len(options.ClientCertificatePEM) > 0
	hasKey := len(options.ClientPrivateKeyPEM) > 0
	if hasCertificate != hasKey {
		return nil, fmt.Errorf("%w: client certificate and key must be paired", ErrInvalidTLSPolicy)
	}
	if hasCertificate {
		certificate, err := tls.X509KeyPair(
			append([]byte(nil), options.ClientCertificatePEM...),
			append([]byte(nil), options.ClientPrivateKeyPEM...),
		)
		if err != nil {
			return nil, fmt.Errorf("%w: client certificate is malformed", ErrInvalidTLSPolicy)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	pins := append([][sha256.Size]byte(nil), options.SPKISHA256Pins...)
	return &TLSPolicy{config: config, pins: pins}, nil
}

func (policy *TLSPolicy) tlsConfig() *tls.Config {
	config := policy.config.Clone()
	if len(policy.pins) == 0 {
		return config
	}
	pins := append([][sha256.Size]byte(nil), policy.pins...)
	config.VerifyConnection = func(state tls.ConnectionState) error {
		for _, certificate := range state.PeerCertificates {
			digest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
			for _, pin := range pins {
				if hmac.Equal(digest[:], pin[:]) {
					return nil
				}
			}
		}
		return ErrTLSPinMismatch
	}
	return config
}
