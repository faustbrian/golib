package kafkatest

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

// AuthenticationProviderHarness supplies isolated implementations of every
// provider seam owned by the root Kafka module.
type AuthenticationProviderHarness struct {
	UsernamePassword  func(*testing.T) kafka.UsernamePasswordProvider
	OAuthBearer       func(*testing.T) kafka.OAuthBearerProvider
	ClientCertificate func(*testing.T) kafka.ClientCertificateProvider
	TrustAnchors      func(*testing.T) kafka.TrustAnchorProvider
}

// Validate checks whether every authentication provider seam is present.
func (harness AuthenticationProviderHarness) Validate() error {
	if harness.UsernamePassword == nil || harness.OAuthBearer == nil ||
		harness.ClientCertificate == nil || harness.TrustAnchors == nil {
		return ErrInvalidHarness
	}

	return nil
}

// RunAuthenticationProviderConformance proves cancellation, refresh,
// independent result ownership, and redacted formatting for every root
// authentication-provider seam. Provider factories must return isolated state
// for each subtest.
func RunAuthenticationProviderConformance(
	t *testing.T,
	harness AuthenticationProviderHarness,
) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("username password", func(t *testing.T) {
		provider := harness.UsernamePassword(t)
		if provider == nil {
			t.Fatal("UsernamePassword provider is nil")
		}
		first, err := provider.Credentials(t.Context())
		if err != nil || first.Username == "" || len(first.Password) == 0 {
			t.Fatalf("Credentials() = %v, %v", first, err)
		}
		original := append([]byte(nil), first.Password...)
		first.Password[0] ^= 0xff
		second, err := provider.Credentials(t.Context())
		if err != nil || !bytes.Equal(second.Password, original) {
			t.Fatalf("refreshed Credentials() = %v, %v", second, err)
		}
		if fmt.Sprint(second) != "kafka.UsernamePassword{redacted}" ||
			fmt.Sprintf("%#v", second) != "kafka.UsernamePassword{redacted}" {
			t.Fatalf("credential formatting is not redacted: %v %#v", second, second)
		}
		assertCanceledUsernamePasswordProvider(t, provider)
	})

	t.Run("oauth bearer", func(t *testing.T) {
		provider := harness.OAuthBearer(t)
		if provider == nil {
			t.Fatal("OAuthBearer provider is nil")
		}
		first, err := provider.Token(t.Context())
		if err != nil || len(first.Token) == 0 || !first.ExpiresAt.After(time.Now()) {
			t.Fatalf("Token() = %v, %v", first, err)
		}
		originalToken := append([]byte(nil), first.Token...)
		originalExtensions := maps.Clone(first.Extensions)
		first.Token[0] ^= 0xff
		for key := range first.Extensions {
			first.Extensions[key] = "mutated"
		}
		second, err := provider.Token(t.Context())
		if err != nil || !bytes.Equal(second.Token, originalToken) ||
			!maps.Equal(second.Extensions, originalExtensions) {
			t.Fatalf("refreshed Token() = %v, %v", second, err)
		}
		if fmt.Sprint(second) != "kafka.OAuthBearerToken{redacted}" ||
			fmt.Sprintf("%#v", second) != "kafka.OAuthBearerToken{redacted}" {
			t.Fatalf("token formatting is not redacted: %v %#v", second, second)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := provider.Token(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Token(canceled) error = %v", err)
		}
	})

	t.Run("client certificate", func(t *testing.T) {
		provider := harness.ClientCertificate(t)
		if provider == nil {
			t.Fatal("ClientCertificate provider is nil")
		}
		request := kafka.ClientCertificateRequest{
			AcceptableCAs:    [][]byte{{1, 2, 3}},
			SignatureSchemes: []tls.SignatureScheme{tls.PSSWithSHA256},
			Version:          tls.VersionTLS13,
		}
		first, err := provider.ClientCertificate(t.Context(), request)
		if err != nil || len(first.Certificate) == 0 || len(first.Certificate[0]) == 0 {
			t.Fatalf("ClientCertificate() chain entries = %d, error = %v", len(first.Certificate), err)
		}
		original := append([]byte(nil), first.Certificate[0]...)
		first.Certificate[0][0] ^= 0xff
		second, err := provider.ClientCertificate(t.Context(), request)
		if err != nil || !bytes.Equal(second.Certificate[0], original) {
			t.Fatalf("refreshed ClientCertificate() chain entries = %d, error = %v", len(second.Certificate), err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := provider.ClientCertificate(ctx, request); !errors.Is(err, context.Canceled) {
			t.Fatalf("ClientCertificate(canceled) error = %v", err)
		}
	})

	t.Run("trust anchors", func(t *testing.T) {
		provider := harness.TrustAnchors(t)
		if provider == nil {
			t.Fatal("TrustAnchor provider is nil")
		}
		first, err := provider.TrustAnchors(t.Context())
		if err != nil || len(first.Certificates) == 0 || len(first.Certificates[0]) == 0 {
			t.Fatalf("TrustAnchors() = %v, %v", first, err)
		}
		original := append([]byte(nil), first.Certificates[0]...)
		first.Certificates[0][0] ^= 0xff
		second, err := provider.TrustAnchors(t.Context())
		if err != nil || !slices.Equal(second.Certificates[0], original) {
			t.Fatalf("refreshed TrustAnchors() = %v, %v", second, err)
		}
		if fmt.Sprint(second) != "kafka.TrustAnchors{redacted}" ||
			fmt.Sprintf("%#v", second) != "kafka.TrustAnchors{redacted}" {
			t.Fatalf("anchor formatting is not redacted: %v %#v", second, second)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := provider.TrustAnchors(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("TrustAnchors(canceled) error = %v", err)
		}
	})
}

func assertCanceledUsernamePasswordProvider(
	t *testing.T,
	provider kafka.UsernamePasswordProvider,
) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := provider.Credentials(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Credentials(canceled) error = %v", err)
	}
}
