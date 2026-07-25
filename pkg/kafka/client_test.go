package kafka

import (
	"crypto/tls"
	"testing"

	"github.com/twmb/franz-go/pkg/sasl/plain"
)

func TestClientSecurityOptionsApplyTLSAndSASL(t *testing.T) {
	t.Parallel()

	options := clientSecurityOptions(ClientSecurity{
		TLS: &tls.Config{MinVersion: tls.VersionTLS13},
		SASL: plain.Auth{
			User: "test-user",
			Pass: "test-password",
		}.AsMechanism(),
	})

	if len(options) != 2 {
		t.Fatalf("clientSecurityOptions() length = %d, want 2", len(options))
	}
}
