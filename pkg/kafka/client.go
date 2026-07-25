package kafka

import (
	"crypto/tls"
	"errors"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
)

var ErrInvalidSecurityConfig = errors.New(
	"kafka: client security configuration is invalid",
)

// ClientSecurity configures encrypted transport and optional SASL
// authentication. TLS verification cannot be disabled.
type ClientSecurity struct {
	TLS  *tls.Config
	SASL sasl.Mechanism
}

func normalizeClientSecurity(security ClientSecurity) (ClientSecurity, error) {
	if security.TLS == nil {
		return security, nil
	}

	tlsConfig := security.TLS.Clone()
	if tlsConfig.InsecureSkipVerify ||
		(tlsConfig.MinVersion != 0 && tlsConfig.MinVersion < tls.VersionTLS12) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	if tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tlsConfig.MinVersion {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	security.TLS = tlsConfig

	return security, nil
}

func clientSecurityOptions(security ClientSecurity) []kgo.Opt {
	options := make([]kgo.Opt, 0, 2)
	if security.TLS != nil {
		options = append(options, kgo.DialTLSConfig(security.TLS))
	}
	if security.SASL != nil {
		options = append(options, kgo.SASL(security.SASL))
	}

	return options
}
