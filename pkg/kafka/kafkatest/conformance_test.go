package kafkatest_test

import (
	"context"
	"crypto/tls"
	"maps"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkatest"
)

func TestAuthenticationProviderFuncConformance(t *testing.T) {
	t.Parallel()

	kafkatest.RunAuthenticationProviderConformance(t, kafkatest.AuthenticationProviderHarness{
		UsernamePassword: func(*testing.T) kafka.UsernamePasswordProvider {
			return kafka.UsernamePasswordProviderFunc(func(
				ctx context.Context,
			) (kafka.UsernamePassword, error) {
				if err := ctx.Err(); err != nil {
					return kafka.UsernamePassword{}, err
				}
				return kafka.UsernamePassword{
					Username: "conformance-user",
					Password: []byte("conformance-password"),
				}, nil
			})
		},
		OAuthBearer: func(*testing.T) kafka.OAuthBearerProvider {
			extensions := map[string]string{"tenant": "conformance"}
			return kafka.OAuthBearerProviderFunc(func(
				ctx context.Context,
			) (kafka.OAuthBearerToken, error) {
				if err := ctx.Err(); err != nil {
					return kafka.OAuthBearerToken{}, err
				}
				return kafka.OAuthBearerToken{
					Token:      []byte("conformance-token"),
					ExpiresAt:  time.Now().Add(time.Hour),
					Extensions: maps.Clone(extensions),
				}, nil
			})
		},
		ClientCertificate: func(*testing.T) kafka.ClientCertificateProvider {
			return kafka.ClientCertificateProviderFunc(func(
				ctx context.Context,
				_ kafka.ClientCertificateRequest,
			) (tls.Certificate, error) {
				if err := ctx.Err(); err != nil {
					return tls.Certificate{}, err
				}
				return tls.Certificate{Certificate: [][]byte{{1, 2, 3, 4}}}, nil
			})
		},
		TrustAnchors: func(*testing.T) kafka.TrustAnchorProvider {
			return kafka.TrustAnchorProviderFunc(func(
				ctx context.Context,
			) (kafka.TrustAnchors, error) {
				if err := ctx.Err(); err != nil {
					return kafka.TrustAnchors{}, err
				}
				return kafka.TrustAnchors{Certificates: [][]byte{{4, 3, 2, 1}}}, nil
			})
		},
	})
}

func TestObserverPolicyConformance(t *testing.T) {
	t.Parallel()

	kafkatest.RunObserverConformance(t)
}
