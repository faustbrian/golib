package kafka

import (
	"crypto/tls"
	"testing"
)

func TestClientCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("safe TLS 1.2 cipher suite is found after mismatches", func(t *testing.T) {
		var configuredID uint16
		for index := len(tls.CipherSuites()) - 1; index >= 0; index-- {
			suite := tls.CipherSuites()[index]
			for _, version := range suite.SupportedVersions {
				if version == tls.VersionTLS12 {
					configuredID = suite.ID

					break
				}
			}
			if configuredID != 0 {
				break
			}
		}
		if configuredID == 0 {
			t.Fatal("crypto/tls exposes no safe TLS 1.2 cipher suite")
		}
		if !validCipherSuites([]uint16{configuredID}) {
			t.Fatalf("validCipherSuites(%#x) = false", configuredID)
		}
		if validCipherSuites([]uint16{0}) {
			t.Fatal("validCipherSuites(unknown) = true")
		}
	})

	t.Run("OAuth extension value accepts only protocol characters", func(t *testing.T) {
		for _, character := range []byte{0x21, 0x7e, ' ', '\t', '\r', '\n'} {
			if !validOAuthExtensionValueCharacter(character) {
				t.Fatalf("validOAuthExtensionValueCharacter(%#x) = false", character)
			}
		}
		for _, character := range []byte{0x00, 0x7f} {
			if validOAuthExtensionValueCharacter(character) {
				t.Fatalf("validOAuthExtensionValueCharacter(%#x) = true", character)
			}
		}
	})
}
