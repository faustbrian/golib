package kafka

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type usernamePasswordProviderStub struct {
	provider UsernamePasswordProviderFunc
}

func (stub usernamePasswordProviderStub) Credentials(
	ctx context.Context,
) (UsernamePassword, error) {
	return stub.provider(ctx)
}

type oauthBearerProviderStub struct {
	provider OAuthBearerProviderFunc
}

func (stub oauthBearerProviderStub) Token(
	ctx context.Context,
) (OAuthBearerToken, error) {
	return stub.provider(ctx)
}

type clientCertificateProviderStub struct {
	provider ClientCertificateProviderFunc
}

func (stub clientCertificateProviderStub) ClientCertificate(
	ctx context.Context,
	request ClientCertificateRequest,
) (tls.Certificate, error) {
	return stub.provider(ctx, request)
}

type trustAnchorProviderStub struct {
	provider TrustAnchorProviderFunc
}

func (stub trustAnchorProviderStub) TrustAnchors(
	ctx context.Context,
) (TrustAnchors, error) {
	return stub.provider(ctx)
}

func newTestTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	server := httptest.NewTLSServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusNoContent)
	}))
	certificate := server.TLS.Certificates[0]
	server.Close()

	return certificate
}

func newTestTrustAnchor(t *testing.T, serial int64) *x509.Certificate {
	return newTestTrustAnchorWithExtension(t, serial, 0)
}

func newTestTrustAnchorWithExtension(
	t *testing.T,
	serial int64,
	extensionBytes int,
) *x509.Certificate {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate trust-anchor key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if extensionBytes > 0 {
		template.ExtraExtensions = []pkix.Extension{{
			Id:    []int{1, 3, 6, 1, 4, 1, 55555, 1},
			Value: make([]byte, extensionBytes),
		}}
	}
	encoded, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		publicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create trust anchor: %v", err)
	}
	certificate, err := x509.ParseCertificate(encoded)
	if err != nil {
		t.Fatalf("parse trust anchor: %v", err)
	}

	return certificate
}

func TestClientSecurityDefaultsToVerifiedTLSAndRequiresExplicitPlaintext(t *testing.T) {

	if err := (ClientSecurity{}).Validate(); err != nil {
		t.Fatalf("validate zero security: %v", err)
	}
	secure, err := normalizeClientSecurity(ClientSecurity{})
	if err != nil {
		t.Fatalf("normalize zero security: %v", err)
	}
	if secure.Transport != TransportTLS || secure.TLS == nil ||
		secure.TLS.MinVersion != tls.VersionTLS12 || secure.TLS.InsecureSkipVerify {
		t.Fatalf("zero security = %#v", secure)
	}

	plaintext, err := normalizeClientSecurity(DevelopmentPlaintextSecurity())
	if err != nil {
		t.Fatalf("normalize development plaintext: %v", err)
	}
	if plaintext.Transport != TransportDevelopmentPlaintext || plaintext.TLS != nil {
		t.Fatalf("development plaintext = %#v", plaintext)
	}

	_, err = normalizeClientSecurity(ClientSecurity{
		Transport: TransportDevelopmentPlaintext,
		Authentication: NewPlainAuthentication(UsernamePasswordProviderFunc(
			func(context.Context) (UsernamePassword, error) {
				return UsernamePassword{}, nil
			},
		)),
	})
	if !errors.Is(err, ErrInvalidSecurityConfig) {
		t.Fatalf("plaintext authentication error = %v", err)
	}
	if err := (ClientSecurity{
		Transport: TransportDevelopmentPlaintext,
		TLS:       &tls.Config{},
	}).Validate(); !errors.Is(err, ErrInvalidSecurityConfig) {
		t.Fatalf("Validate() plaintext TLS error = %v", err)
	}
}

func TestClientSecurityOptionsApplyTLSAndSASL(t *testing.T) {

	security, err := normalizeClientSecurity(ClientSecurity{
		TLS: &tls.Config{MinVersion: tls.VersionTLS13},
		Authentication: NewPlainAuthentication(usernamePasswordProviderStub{
			provider: UsernamePasswordProviderFunc(
				func(context.Context) (UsernamePassword, error) {
					return UsernamePassword{
						Username: "test-user",
						Password: []byte("test-password"),
					}, nil
				},
			)}),
	})
	if err != nil {
		t.Fatalf("normalize security: %v", err)
	}
	options := clientSecurityOptions(security, time.Second)

	if len(options) != 2 {
		t.Fatalf("clientSecurityOptions() length = %d, want 2", len(options))
	}
}

func TestAuthenticationProvidersRotateWithinBoundedRedactedSessions(t *testing.T) {

	var calls atomic.Int32
	usernamePasswordProvider := UsernamePasswordProviderFunc(func(
		ctx context.Context,
	) (UsernamePassword, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatalf("credential provider context deadline = %v, %t", deadline, ok)
		}
		call := calls.Add(1)

		return UsernamePassword{
			Username: "service",
			Password: []byte{byte('0' + call)},
		}, nil
	})
	oauthProvider := OAuthBearerProviderFunc(func(ctx context.Context) (OAuthBearerToken, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatalf("token provider context deadline = %v, %t", deadline, ok)
		}

		return OAuthBearerToken{
			Token:     []byte("rotating-token"),
			ExpiresAt: time.Now().Add(time.Minute),
			Extensions: map[string]string{
				"tenant": "bounded",
			},
		}, nil
	})

	authentications := []Authentication{
		NewPlainAuthentication(usernamePasswordProvider),
		NewSCRAMSHA256Authentication(usernamePasswordProvider),
		NewSCRAMSHA512Authentication(usernamePasswordProvider),
		NewOAuthBearerAuthentication(oauthProvider),
		NewOAuthBearerAuthentication(oauthBearerProviderStub{provider: OAuthBearerProviderFunc(func(
			context.Context,
		) (OAuthBearerToken, error) {
			return OAuthBearerToken{
				Token:     []byte("token-without-extensions=="),
				ExpiresAt: time.Now().Add(time.Minute),
			}, nil
		})}),
	}
	for _, authentication := range authentications {
		if strings.Contains(authentication.String()+authentication.GoString(), "secret") {
			t.Fatalf("%s authentication formatting disclosed a secret", authentication.Method())
		}
		mechanism := authentication.saslMechanism(time.Second)
		if mechanism == nil {
			t.Fatalf("%s mechanism = nil", authentication.Method())
		}
		if _, _, err := mechanism.Authenticate(context.Background(), "broker.internal"); err != nil {
			t.Fatalf("%s authenticate: %v", authentication.Method(), err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("username/password provider calls = %d, want 3", calls.Load())
	}
	if _, _, err := authentications[0].saslMechanism(time.Second).Authenticate(
		context.Background(), "broker.internal",
	); err != nil {
		t.Fatalf("second rotating PLAIN authentication: %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("rotating provider calls = %d, want 4", calls.Load())
	}
	if err := (ClientSecurity{Authentication: authentications[4]}).Validate(); err != nil {
		t.Fatalf("validate custom OAuth provider: %v", err)
	}

	secretCause := errors.New("token secret-token failed")
	failing := NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
		context.Context,
	) (OAuthBearerToken, error) {
		return OAuthBearerToken{}, secretCause
	}))
	_, _, err := failing.saslMechanism(time.Second).Authenticate(
		context.Background(),
		"broker.internal",
	)
	if !errors.Is(err, ErrCredentialProviderFailed) || !errors.Is(err, secretCause) ||
		strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("provider error = %v", err)
	}

	security := ClientSecurity{
		Authentication: NewPlainAuthentication(usernamePasswordProvider),
	}
	formatted := security.String() + " " + security.GoString() + " " +
		usernamePasswordProviderString(UsernamePassword{
			Username: "service", Password: []byte("secret-password"),
		})
	if strings.Contains(formatted, "secret") || strings.Contains(formatted, "service") {
		t.Fatalf("security formatting disclosed credentials: %q", formatted)
	}
}

func usernamePasswordProviderString(credentials UsernamePassword) string {
	return credentials.String() + " " + credentials.GoString()
}

func TestClientSecurityDefensivelyCopiesTLSMaterial(t *testing.T) {

	roots := x509.NewCertPool()
	certificate := newTestTLSCertificate(t)
	certificate.SupportedSignatureAlgorithms = []tls.SignatureScheme{tls.PSSWithSHA256}
	firstCertificateByte := certificate.Certificate[0][0]
	source := &tls.Config{
		RootCAs:          roots,
		Certificates:     []tls.Certificate{certificate},
		NextProtos:       []string{"kafka"},
		CipherSuites:     []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		CurvePreferences: []tls.CurveID{tls.X25519},
	}

	security, err := normalizeClientSecurity(ClientSecurity{TLS: source})
	if err != nil {
		t.Fatalf("normalize TLS: %v", err)
	}
	if security.TLS == source || security.TLS.RootCAs == source.RootCAs {
		t.Fatal("normalized TLS configuration shares mutable roots")
	}
	source.Certificates[0].Certificate[0][0] ^= 0xff
	source.NextProtos[0] = "changed"
	source.CipherSuites[0] = tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
	source.CurvePreferences[0] = tls.CurveP256
	source.Certificates[0].SupportedSignatureAlgorithms[0] = tls.Ed25519
	if security.TLS.Certificates[0].Certificate[0][0] != firstCertificateByte ||
		security.TLS.NextProtos[0] != "kafka" ||
		security.TLS.CipherSuites[0] != tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 ||
		security.TLS.CurvePreferences[0] != tls.X25519 ||
		security.TLS.Certificates[0].SupportedSignatureAlgorithms[0] != tls.PSSWithSHA256 {
		t.Fatalf("normalized TLS configuration aliases caller slices: %#v", security)
	}
}

func TestAuthenticationRejectsInvalidExpiredAndPanickingProviderResults(t *testing.T) {

	usernameProviderFailure := errors.New("username provider secret failed")
	tests := []struct {
		name           string
		authentication Authentication
		want           error
	}{
		{
			name: "empty username",
			authentication: NewPlainAuthentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				return UsernamePassword{Password: []byte("password")}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "non-UTF-8 password",
			authentication: NewPlainAuthentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				return UsernamePassword{Username: "service", Password: []byte{0xff}}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "leading NUL password",
			authentication: NewSCRAMSHA512Authentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				return UsernamePassword{Username: "service", Password: []byte{0, 'a'}}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "username provider error",
			authentication: NewSCRAMSHA256Authentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				return UsernamePassword{}, usernameProviderFailure
			})),
			want: usernameProviderFailure,
		},
		{
			name: "expired token",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(-time.Second),
				}, nil
			})),
			want: ErrExpiredOAuthBearerToken,
		},
		{
			name: "invalid extension",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: map[string]string{"invalid key": "value"},
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "non-alpha extension key",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: map[string]string{"tenant1": "value"},
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "reserved auth extension",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: map[string]string{"auth": "value"},
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "extension separator injection",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: map[string]string{"tenant": "value\x01auth=Bearer injected"},
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "invalid bearer token bytes",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token\x01injected"), ExpiresAt: time.Now().Add(time.Minute),
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "empty bearer token",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{ExpiresAt: time.Now().Add(time.Minute)}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "bearer token data after padding",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token=a"), ExpiresAt: time.Now().Add(time.Minute),
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "invalid OAuth authorization ID",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					AuthorizationID: "service,admin",
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "whitespace OAuth authorization ID",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					AuthorizationID: " ",
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "empty extension key",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: map[string]string{"": "value"},
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "too many extensions",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				extensions := make(map[string]string, 33)
				for index := range 33 {
					extensions[string(rune('a'+index))] = "value"
				}

				return OAuthBearerToken{
					Token: []byte("token"), ExpiresAt: time.Now().Add(time.Minute),
					Extensions: extensions,
				}, nil
			})),
			want: ErrInvalidCredentials,
		},
		{
			name: "provider panic",
			authentication: NewPlainAuthentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				panic("secret panic payload")
			})),
			want: ErrCredentialProviderPanic,
		},
		{
			name: "OAuth provider panic",
			authentication: NewOAuthBearerAuthentication(OAuthBearerProviderFunc(func(
				context.Context,
			) (OAuthBearerToken, error) {
				panic("secret OAuth panic payload")
			})),
			want: ErrCredentialProviderPanic,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			_, _, err := test.authentication.saslMechanism(time.Second).Authenticate(
				context.Background(), "broker.internal",
			)
			if !errors.Is(err, ErrCredentialProviderFailed) ||
				!errors.Is(err, test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Authenticate() error = %v", err)
			}
		})
	}
}

func TestClientSecurityRejectsInvalidPolicyCombinations(t *testing.T) {
	var nilUsernameProvider UsernamePasswordProviderFunc
	var nilOAuthProvider OAuthBearerProviderFunc
	var nilCertificateProvider ClientCertificateProviderFunc
	var nilTrustAnchorProvider TrustAnchorProviderFunc
	var nilTrustAnchorProviderStub *trustAnchorProviderStub
	validCertificate := newTestTLSCertificate(t)
	staticRoots := x509.NewCertPool()

	tests := []ClientSecurity{
		{Transport: TransportSecurity(255)},
		{CredentialTimeout: time.Second},
		{TLS: &tls.Config{ServerName: " "}},
		{TLS: &tls.Config{ServerName: string([]byte{0xff})}},
		{TLS: &tls.Config{ServerName: strings.Repeat("a", 256)}},
		{Authentication: NewPlainAuthentication(nil)},
		{Authentication: NewSCRAMSHA256Authentication(nil)},
		{Authentication: NewSCRAMSHA512Authentication(nil)},
		{Authentication: NewOAuthBearerAuthentication(nil)},
		{Authentication: NewPlainAuthentication(nilUsernameProvider)},
		{Authentication: NewOAuthBearerAuthentication(nilOAuthProvider)},
		{ClientCertificateProvider: nilCertificateProvider},
		{TrustAnchorProvider: nilTrustAnchorProvider},
		{TrustAnchorProvider: nilTrustAnchorProviderStub},
		{
			Transport: TransportDevelopmentPlaintext,
			TrustAnchorProvider: TrustAnchorProviderFunc(func(
				context.Context,
			) (TrustAnchors, error) {
				return TrustAnchors{}, nil
			}),
		},
		{
			TLS:                 &tls.Config{RootCAs: staticRoots},
			TrustAnchorProvider: trustAnchorProviderStub{},
		},
		{
			TLS: &tls.Config{
				ClientSessionCache: tls.NewLRUClientSessionCache(1),
			},
			TrustAnchorProvider: trustAnchorProviderStub{},
		},
		{
			Authentication: NewPlainAuthentication(UsernamePasswordProviderFunc(func(
				context.Context,
			) (UsernamePassword, error) {
				return UsernamePassword{}, nil
			})),
			CredentialTimeout: time.Millisecond,
		},
		{Authentication: Authentication{method: AuthenticationMethod(255)}},
		{
			TLS: &tls.Config{Certificates: []tls.Certificate{validCertificate}},
			ClientCertificateProvider: ClientCertificateProviderFunc(func(
				context.Context,
				ClientCertificateRequest,
			) (tls.Certificate, error) {
				return tls.Certificate{}, nil
			}),
		},
	}
	for index, security := range tests {
		if _, err := normalizeClientSecurity(security); !errors.Is(err, ErrInvalidSecurityConfig) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}

	methods := []AuthenticationMethod{
		AuthenticationNone,
		AuthenticationPlain,
		AuthenticationSCRAMSHA256,
		AuthenticationSCRAMSHA512,
		AuthenticationOAuthBearer,
	}
	for _, method := range methods {
		if method.String() == "unknown" {
			t.Fatalf("known method %d rendered unknown", method)
		}
	}
	if AuthenticationMethod(255).String() != "unknown" ||
		TransportDevelopmentPlaintext.String() != "development-plaintext" ||
		TransportSecurity(255).String() != "unknown" {
		t.Fatal("unknown security policy did not render safely")
	}

	token := OAuthBearerToken{Token: []byte("secret-token")}
	if strings.Contains(token.String()+token.GoString(), "secret-token") {
		t.Fatal("OAuthBearerToken formatting disclosed token")
	}
}

func TestTrustAnchorProviderIsBoundedOwnedRotatingAndPanicSafe(t *testing.T) {
	firstCertificate := newTestTrustAnchor(t, 1)
	secondCertificate := newTestTrustAnchor(t, 2)

	current := atomic.Value{}
	current.Store([][]byte{append([]byte(nil), firstCertificate.Raw...)})
	var calls atomic.Int64
	provider := trustAnchorProviderStub{provider: TrustAnchorProviderFunc(func(
		ctx context.Context,
	) (TrustAnchors, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatalf("trust-anchor provider deadline = %v, %t", deadline, ok)
		}
		calls.Add(1)

		return TrustAnchors{Certificates: current.Load().([][]byte)}, nil
	})}
	security, err := normalizeClientSecurity(ClientSecurity{
		TrustAnchorProvider: provider,
		CredentialTimeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("normalize trust-anchor provider: %v", err)
	}
	firstRoots, err := callTrustAnchorProvider(
		context.Background(),
		security.CredentialTimeout,
		security.TrustAnchorProvider,
	)
	if err != nil {
		t.Fatalf("load first trust anchors: %v", err)
	}
	current.Load().([][]byte)[0][0] ^= 0xff
	if _, err := firstCertificate.Verify(x509.VerifyOptions{Roots: firstRoots}); err != nil {
		t.Fatalf("verify first certificate after provider result mutation: %v", err)
	}

	current.Store([][]byte{append([]byte(nil), secondCertificate.Raw...)})
	secondRoots, err := callTrustAnchorProvider(
		context.Background(),
		security.CredentialTimeout,
		security.TrustAnchorProvider,
	)
	if err != nil {
		t.Fatalf("load rotated trust anchors: %v", err)
	}
	if _, err := secondCertificate.Verify(x509.VerifyOptions{Roots: secondRoots}); err != nil {
		t.Fatalf("verify rotated certificate: %v", err)
	}
	if _, err := firstCertificate.Verify(x509.VerifyOptions{Roots: secondRoots}); err == nil {
		t.Fatal("rotated trust anchors still accepted the retired certificate")
	}
	if calls.Load() != 2 {
		t.Fatalf("trust-anchor provider calls = %d, want 2", calls.Load())
	}

	formatted := TrustAnchors{Certificates: [][]byte{[]byte("secret-root")}}
	if strings.Contains(formatted.String()+formatted.GoString(), "secret-root") {
		t.Fatal("trust-anchor formatting disclosed certificate material")
	}

	secretCause := errors.New("secret trust provider failure")
	_, err = callTrustAnchorProvider(
		context.Background(),
		time.Second,
		TrustAnchorProviderFunc(func(context.Context) (TrustAnchors, error) {
			return TrustAnchors{}, secretCause
		}),
	)
	if !errors.Is(err, ErrCredentialProviderFailed) ||
		!errors.Is(err, secretCause) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("trust-anchor provider error = %v", err)
	}

	_, err = callTrustAnchorProvider(
		context.Background(),
		time.Second,
		TrustAnchorProviderFunc(func(context.Context) (TrustAnchors, error) {
			panic("secret trust provider panic")
		}),
	)
	if !errors.Is(err, ErrCredentialProviderPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("panicking trust-anchor provider error = %v", err)
	}
}

func TestTrustAnchorProviderRejectsInvalidMaterial(t *testing.T) {
	certificate := newTestTLSCertificate(t).Certificate[0]
	firstLarge := newTestTrustAnchorWithExtension(t, 10, maxTrustAnchorBytes/2).Raw
	secondLarge := newTestTrustAnchorWithExtension(t, 11, maxTrustAnchorBytes/2).Raw
	if len(firstLarge) > maxTrustAnchorBytes || len(secondLarge) > maxTrustAnchorBytes ||
		len(firstLarge)+len(secondLarge) <= maxTrustAnchorBytes {
		t.Fatalf("large trust-anchor sizes = %d + %d", len(firstLarge), len(secondLarge))
	}
	cumulative := [][]byte{
		newTestTrustAnchorWithExtension(t, 12, maxTrustAnchorBytes/3).Raw,
		newTestTrustAnchorWithExtension(t, 13, maxTrustAnchorBytes/3).Raw,
		newTestTrustAnchorWithExtension(t, 14, maxTrustAnchorBytes/3).Raw,
	}
	if len(cumulative[0])+len(cumulative[1]) > maxTrustAnchorBytes ||
		len(cumulative[0])+len(cumulative[1])+len(cumulative[2]) <= maxTrustAnchorBytes {
		t.Fatalf(
			"cumulative trust-anchor sizes = %d + %d + %d",
			len(cumulative[0]), len(cumulative[1]), len(cumulative[2]),
		)
	}
	tooMany := make([][]byte, maxTrustAnchorCertificates+1)
	for index := range tooMany {
		tooMany[index] = certificate
	}
	tests := []TrustAnchors{
		{},
		{Certificates: [][]byte{nil}},
		{Certificates: [][]byte{[]byte("not a certificate")}},
		{Certificates: [][]byte{certificate, certificate}},
		{Certificates: tooMany},
		{Certificates: [][]byte{make([]byte, maxTrustAnchorBytes+1)}},
		{Certificates: [][]byte{firstLarge, secondLarge}},
		{Certificates: cumulative},
	}
	for index, anchors := range tests {
		_, err := callTrustAnchorProvider(
			context.Background(),
			time.Second,
			TrustAnchorProviderFunc(func(context.Context) (TrustAnchors, error) {
				return anchors, nil
			}),
		)
		if !errors.Is(err, ErrInvalidTrustAnchors) {
			t.Fatalf("case %d trust-anchor error = %v", index, err)
		}
	}
	if !validTrustAnchorCount(maxTrustAnchorCertificates) ||
		validTrustAnchorCount(0) ||
		validTrustAnchorCount(maxTrustAnchorCertificates+1) {
		t.Fatal("trust-anchor count boundaries are invalid")
	}
	if !trustAnchorBytesFit(maxTrustAnchorBytes-1, 1) ||
		trustAnchorBytesFit(maxTrustAnchorBytes, 1) ||
		trustAnchorBytesFit(-1, 1) ||
		trustAnchorBytesFit(0, 0) {
		t.Fatal("trust-anchor byte boundaries are invalid")
	}
}

func TestTrustAnchorDialerLoadsRootsForEveryConnection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "https://")
	var current atomic.Value
	current.Store(TrustAnchors{Certificates: [][]byte{
		append([]byte(nil), server.Certificate().Raw...),
	}})
	var calls atomic.Int64
	security, err := normalizeClientSecurity(ClientSecurity{
		TrustAnchorProvider: TrustAnchorProviderFunc(func(
			ctx context.Context,
		) (TrustAnchors, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 100*time.Millisecond {
				t.Fatalf("trust-anchor dial deadline = %v, %t", deadline, ok)
			}
			calls.Add(1)

			return current.Load().(TrustAnchors), nil
		}),
		CredentialTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("normalize trust-anchor dialer: %v", err)
	}
	dial := trustAnchorDialer(security, 100*time.Millisecond)
	connection, err := dial(context.Background(), "tcp", address)
	if err != nil {
		t.Fatalf("dial trusted TLS server: %v", err)
	}
	if closeErr := connection.Close(); closeErr != nil {
		t.Fatalf("close trusted TLS connection: %v", closeErr)
	}

	current.Store(TrustAnchors{})
	_, err = dial(context.Background(), "tcp", address)
	if !errors.Is(err, ErrInvalidTrustAnchors) {
		t.Fatalf("dial after invalid trust rotation error = %v", err)
	}
	current.Store(TrustAnchors{Certificates: [][]byte{
		append([]byte(nil), server.Certificate().Raw...),
	}})
	invalidAddresses := []string{
		"invalid-address",
		" padded.example:9092",
		":9092",
		strings.Repeat("a", 256) + ":9092",
		string([]byte{0xff}) + ":9092",
	}
	for _, address := range invalidAddresses {
		_, err = dial(context.Background(), "tcp", address)
		if !errors.Is(err, ErrInvalidBroker) {
			t.Fatalf("dial invalid broker %q error = %v", address, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("trust-anchor dial calls = %d, want 2", calls.Load())
	}
}

func TestClientSecurityAcceptsInclusivePolicyBoundaries(t *testing.T) {

	provider := UsernamePasswordProviderFunc(func(
		context.Context,
	) (UsernamePassword, error) {
		return UsernamePassword{
			Username: "service",
			Password: []byte("password"),
		}, nil
	})
	for _, timeout := range []time.Duration{100 * time.Millisecond, time.Minute} {
		timeout := timeout
		t.Run(timeout.String(), func(t *testing.T) {

			security, err := normalizeClientSecurity(ClientSecurity{
				TLS: &tls.Config{
					MinVersion: tls.VersionTLS12,
					MaxVersion: tls.VersionTLS12,
				},
				Authentication:    NewPlainAuthentication(provider),
				CredentialTimeout: timeout,
			})
			if err != nil {
				t.Fatalf("normalize inclusive boundary: %v", err)
			}
			if security.CredentialTimeout != timeout ||
				security.TLS.MinVersion != tls.VersionTLS12 ||
				security.TLS.MaxVersion != tls.VersionTLS12 {
				t.Fatalf("normalized security = %#v", security)
			}
		})
	}
}

func TestClientSecurityAcceptsTLSMaterialLimits(t *testing.T) {

	certificate := newTestTLSCertificate(t)
	certificates := make([]tls.Certificate, 16)
	for index := range certificates {
		certificates[index] = certificate
	}
	nextProtocols := make([]string, 16)
	nextProtocols[0] = strings.Repeat("a", 255)
	for index := 1; index < len(nextProtocols); index++ {
		nextProtocols[index] = string(rune('a' + index))
	}
	cipherSuites := make([]uint16, 0, len(tls.CipherSuites()))
	for _, suite := range tls.CipherSuites() {
		for _, version := range suite.SupportedVersions {
			if version == tls.VersionTLS12 {
				cipherSuites = append(cipherSuites, suite.ID)
				break
			}
		}
	}
	curves := []tls.CurveID{
		tls.CurveP256,
		tls.CurveP384,
		tls.CurveP521,
		tls.X25519,
		tls.X25519MLKEM768,
		tls.SecP256r1MLKEM768,
		tls.SecP384r1MLKEM1024,
	}
	security, err := normalizeClientSecurity(ClientSecurity{TLS: &tls.Config{
		MinVersion:       tls.VersionTLS12,
		MaxVersion:       tls.VersionTLS12,
		ServerName:       strings.Repeat("s", 255),
		Certificates:     certificates,
		NextProtos:       nextProtocols,
		CipherSuites:     cipherSuites,
		CurvePreferences: curves,
	}})
	if err != nil {
		t.Fatalf("normalize inclusive TLS material limits: %v", err)
	}
	if len(security.TLS.Certificates) != len(certificates) ||
		len(security.TLS.ServerName) != 255 ||
		len(security.TLS.NextProtos) != len(nextProtocols) ||
		len(security.TLS.CipherSuites) != len(cipherSuites) ||
		len(security.TLS.CurvePreferences) != len(curves) {
		t.Fatalf("normalized TLS material = %#v", security.TLS)
	}
}

func TestCredentialProvidersAcceptInclusiveMaterialLimits(t *testing.T) {

	credentials, err := callUsernamePasswordProvider(
		context.Background(),
		time.Second,
		UsernamePasswordProviderFunc(func(context.Context) (UsernamePassword, error) {
			return UsernamePassword{
				Username:        strings.Repeat("u", 8<<10),
				Password:        []byte(strings.Repeat("p", 8<<10)),
				AuthorizationID: strings.Repeat("a", 8<<10),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("call username-password provider at limits: %v", err)
	}
	if len(credentials.Username) != 8<<10 ||
		len(credentials.Password) != 8<<10 ||
		len(credentials.AuthorizationID) != 8<<10 {
		t.Fatalf("credentials lengths = %d, %d, %d",
			len(credentials.Username),
			len(credentials.Password),
			len(credentials.AuthorizationID),
		)
	}

	extensions := make(map[string]string, 32)
	extensions[strings.Repeat("k", 128)] = strings.Repeat("v", 8<<10)
	for length := 1; len(extensions) < 32; length++ {
		extensions[strings.Repeat("e", length)] = "value"
	}
	token, err := callOAuthBearerProvider(
		context.Background(),
		time.Second,
		OAuthBearerProviderFunc(func(context.Context) (OAuthBearerToken, error) {
			return OAuthBearerToken{
				Token:           []byte(strings.Repeat("t", 1<<20)),
				AuthorizationID: strings.Repeat("a", 8<<10),
				Extensions:      extensions,
				ExpiresAt:       time.Now().Add(time.Minute),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("call OAuth bearer provider at limits: %v", err)
	}
	if len(token.Token) != 1<<20 ||
		len(token.AuthorizationID) != 8<<10 ||
		len(token.Extensions) != 32 ||
		len(token.Extensions[strings.Repeat("k", 128)]) != 8<<10 {
		t.Fatalf("OAuth token limits = %d, %d, %d, %d",
			len(token.Token),
			len(token.AuthorizationID),
			len(token.Extensions),
			len(token.Extensions[strings.Repeat("k", 128)]),
		)
	}
}

func TestClientCertificateProviderAcceptsInclusiveRequestLimits(t *testing.T) {

	acceptableCAs := make([][]byte, 64)
	signatureSchemes := make([]tls.SignatureScheme, 64)
	for index := range acceptableCAs {
		acceptableCAs[index] = make([]byte, 1<<10)
		signatureSchemes[index] = tls.SignatureScheme(index + 1)
	}
	certificate := newTestTLSCertificate(t)
	security, err := normalizeClientSecurity(ClientSecurity{
		ClientCertificateProvider: ClientCertificateProviderFunc(func(
			_ context.Context,
			request ClientCertificateRequest,
		) (tls.Certificate, error) {
			totalBytes := 0
			for _, acceptableCA := range request.AcceptableCAs {
				totalBytes += len(acceptableCA)
			}
			if len(request.AcceptableCAs) != 64 ||
				totalBytes != 64<<10 ||
				len(request.SignatureSchemes) != 64 {
				t.Fatalf("owned certificate request limits = %d, %d, %d",
					len(request.AcceptableCAs),
					totalBytes,
					len(request.SignatureSchemes),
				)
			}

			return certificate, nil
		}),
	})
	if err != nil {
		t.Fatalf("normalize mTLS request limits: %v", err)
	}
	if _, err = security.TLS.GetClientCertificate(&tls.CertificateRequestInfo{
		AcceptableCAs:    acceptableCAs,
		SignatureSchemes: signatureSchemes,
		Version:          tls.VersionTLS13,
	}); err != nil {
		t.Fatalf("GetClientCertificate() at request limits: %v", err)
	}
}

func TestClientCertificateProviderAcceptsInclusiveMaterialLimits(t *testing.T) {

	base := newTestTLSCertificate(t)
	signatureAlgorithms := make([]tls.SignatureScheme, 32)
	for index := range signatureAlgorithms {
		signatureAlgorithms[index] = tls.SignatureScheme(index + 1)
	}
	tests := []struct {
		name        string
		certificate func() tls.Certificate
	}{
		{
			name: "chain count and aggregate bytes",
			certificate: func() tls.Certificate {
				certificate := base
				certificate.Certificate = make([][]byte, 16)
				totalCertificateBytes := 0
				for index := range certificate.Certificate {
					certificate.Certificate[index] = append(
						[]byte(nil),
						base.Certificate[0]...,
					)
					totalCertificateBytes += len(certificate.Certificate[index])
				}
				certificate.OCSPStaple = make(
					[]byte,
					(1<<20)-totalCertificateBytes,
				)
				certificate.SupportedSignatureAlgorithms = signatureAlgorithms

				return certificate
			},
		},
		{
			name: "signed timestamp aggregate bytes",
			certificate: func() tls.Certificate {
				certificate := base
				certificate.SignedCertificateTimestamps = [][]byte{
					make([]byte, (1<<20)-len(certificate.Certificate[0])),
				}

				return certificate
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			provided := test.certificate()
			security, err := normalizeClientSecurity(ClientSecurity{
				ClientCertificateProvider: ClientCertificateProviderFunc(func(
					context.Context,
					ClientCertificateRequest,
				) (tls.Certificate, error) {
					return provided, nil
				}),
			})
			if err != nil {
				t.Fatalf("normalize mTLS material limits: %v", err)
			}
			if _, err = security.TLS.GetClientCertificate(
				&tls.CertificateRequestInfo{},
			); err != nil {
				t.Fatalf("GetClientCertificate() at material limits: %v", err)
			}
		})
	}
}

func TestClientCertificateValidationIsolatesEveryMaterialInvariant(t *testing.T) {

	base := newTestTLSCertificate(t)
	if !validClientCertificate(base) {
		t.Fatal("validClientCertificate(base) = false")
	}

	chain := make([][]byte, 17)
	for index := range chain {
		chain[index] = append([]byte(nil), base.Certificate[0]...)
	}
	signatureAlgorithms := make([]tls.SignatureScheme, 33)
	for index := range signatureAlgorithms {
		signatureAlgorithms[index] = tls.SignatureScheme(index + 1)
	}
	remaining := (1 << 20) - len(base.Certificate[0])
	tests := map[string]func() tls.Certificate{
		"empty chain": func() tls.Certificate {
			certificate := base
			certificate.Certificate = nil

			return certificate
		},
		"too many certificates": func() tls.Certificate {
			certificate := base
			certificate.Certificate = chain

			return certificate
		},
		"missing private key": func() tls.Certificate {
			certificate := base
			certificate.PrivateKey = nil

			return certificate
		},
		"too many signature algorithms": func() tls.Certificate {
			certificate := base
			certificate.SupportedSignatureAlgorithms = signatureAlgorithms

			return certificate
		},
		"duplicate signature algorithms": func() tls.Certificate {
			certificate := base
			certificate.SupportedSignatureAlgorithms = []tls.SignatureScheme{
				tls.Ed25519,
				tls.Ed25519,
			}

			return certificate
		},
		"empty encoded certificate": func() tls.Certificate {
			certificate := base
			certificate.Certificate = [][]byte{{}}

			return certificate
		},
		"OCSP plus certificate exceeds aggregate": func() tls.Certificate {
			certificate := base
			certificate.OCSPStaple = make([]byte, remaining+1)

			return certificate
		},
		"certificate chain cumulatively exceeds aggregate": func() tls.Certificate {
			certificate := base
			certificate.OCSPStaple = make([]byte, remaining)
			certificate.Certificate = [][]byte{
				append([]byte(nil), base.Certificate[0]...),
				append([]byte(nil), base.Certificate[0]...),
			}

			return certificate
		},
		"certificate plus timestamp exceeds aggregate": func() tls.Certificate {
			certificate := base
			certificate.SignedCertificateTimestamps = [][]byte{
				make([]byte, remaining+1),
			}

			return certificate
		},
		"timestamps cumulatively exceed aggregate": func() tls.Certificate {
			certificate := base
			certificate.SignedCertificateTimestamps = [][]byte{
				{1},
				make([]byte, remaining),
			}

			return certificate
		},
	}
	for name, build := range tests {
		if validClientCertificate(build()) {
			t.Fatalf("%s validClientCertificate() = true", name)
		}
	}
}

func TestClientCertificateRequestAggregatesEveryCA(t *testing.T) {

	if !validClientCertificateRequest(&tls.CertificateRequestInfo{
		AcceptableCAs: [][]byte{{1}, make([]byte, (64<<10)-1)},
	}) {
		t.Fatal("exact aggregate certificate request rejected")
	}
	if validClientCertificateRequest(&tls.CertificateRequestInfo{
		AcceptableCAs: [][]byte{{1}, make([]byte, 64<<10)},
	}) {
		t.Fatal("oversized cumulative certificate request accepted")
	}
	if validClientCertificateRequest(&tls.CertificateRequestInfo{
		AcceptableCAs: [][]byte{
			make([]byte, 32<<10),
			make([]byte, 32<<10),
			{1},
		},
	}) {
		t.Fatal("three-part oversized cumulative certificate request accepted")
	}
}

func TestMatchingPublicKeysRequiresBothEncodingsAndEquality(t *testing.T) {

	first := newTestTLSCertificate(t)
	signer := first.PrivateKey.(crypto.Signer)
	if !matchingPublicKeys(signer.Public(), signer.Public()) {
		t.Fatal("matchingPublicKeys(equal) = false")
	}
	_, secondPrivateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate second private key: %v", err)
	}
	if matchingPublicKeys(signer.Public(), secondPrivateKey.Public()) {
		t.Fatal("matchingPublicKeys(different) = true")
	}
	if matchingPublicKeys(struct{}{}, signer.Public()) {
		t.Fatal("matchingPublicKeys(invalid first) = true")
	}
	if matchingPublicKeys(signer.Public(), struct{}{}) {
		t.Fatal("matchingPublicKeys(invalid second) = true")
	}
}

func TestClientSecurityRejectsUnboundedOrBypassingTLSMaterial(t *testing.T) {
	firstCertificate := newTestTLSCertificate(t)
	_, secondPrivateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate mismatched private key: %v", err)
	}
	mismatchedCertificate := firstCertificate
	mismatchedCertificate.PrivateKey = secondPrivateKey

	tests := []ClientSecurity{
		{TLS: &tls.Config{Certificates: make([]tls.Certificate, 17)}},
		{TLS: &tls.Config{Certificates: []tls.Certificate{{}}}},
		{TLS: &tls.Config{Certificates: []tls.Certificate{{
			Certificate: [][]byte{{1}}, PrivateKey: struct{}{},
		}}}},
		{TLS: &tls.Config{Certificates: []tls.Certificate{mismatchedCertificate}}},
		{TLS: &tls.Config{NextProtos: make([]string, 17)}},
		{TLS: &tls.Config{NextProtos: []string{""}}},
		{TLS: &tls.Config{NextProtos: []string{strings.Repeat("a", 256)}}},
		{TLS: &tls.Config{NextProtos: []string{"kafka", "kafka"}}},
		{TLS: &tls.Config{CipherSuites: make([]uint16, 65)}},
		{TLS: &tls.Config{CipherSuites: []uint16{1, 1}}},
		{TLS: &tls.Config{CurvePreferences: make([]tls.CurveID, 33)}},
		{TLS: &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519, tls.X25519}}},
		{TLS: &tls.Config{EncryptedClientHelloConfigList: []byte{1}}},
		{TLS: &tls.Config{EncryptedClientHelloConfigList: make([]byte, (1<<20)+1)}},
		{TLS: &tls.Config{KeyLogWriter: io.Discard}},
		{TLS: &tls.Config{Renegotiation: tls.RenegotiateOnceAsClient}},
		{TLS: &tls.Config{CipherSuites: []uint16{tls.TLS_AES_128_GCM_SHA256}}},
		{TLS: &tls.Config{CipherSuites: []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA}}},
		{TLS: &tls.Config{CurvePreferences: []tls.CurveID{tls.CurveID(1)}}},
		{TLS: &tls.Config{MinVersion: 0xffff}},
		{TLS: &tls.Config{MaxVersion: 0xffff}},
		{TLS: &tls.Config{GetClientCertificate: func(
			*tls.CertificateRequestInfo,
		) (*tls.Certificate, error) {
			return &tls.Certificate{}, nil
		}}},
		{TLS: &tls.Config{NameToCertificate: map[string]*tls.Certificate{"broker": {}}}},
		{TLS: &tls.Config{GetCertificate: func(
			*tls.ClientHelloInfo,
		) (*tls.Certificate, error) {
			return &tls.Certificate{}, nil
		}}},
		{TLS: &tls.Config{GetConfigForClient: func(
			*tls.ClientHelloInfo,
		) (*tls.Config, error) {
			return &tls.Config{}, nil
		}}},
		{TLS: &tls.Config{ClientAuth: tls.RequestClientCert}},
		{TLS: &tls.Config{ClientCAs: x509.NewCertPool()}},
		{TLS: &tls.Config{PreferServerCipherSuites: true}},
		{TLS: &tls.Config{SessionTicketKey: [32]byte{1}}},
		{TLS: &tls.Config{UnwrapSession: func(
			[]byte,
			tls.ConnectionState,
		) (*tls.SessionState, error) {
			return nil, nil
		}}},
		{TLS: &tls.Config{WrapSession: func(
			tls.ConnectionState,
			*tls.SessionState,
		) ([]byte, error) {
			return nil, nil
		}}},
		{TLS: &tls.Config{EncryptedClientHelloRejectionVerify: func(
			tls.ConnectionState,
		) error {
			return nil
		}}},
		{TLS: &tls.Config{GetEncryptedClientHelloKeys: func(
			*tls.ClientHelloInfo,
		) ([]tls.EncryptedClientHelloKey, error) {
			return nil, nil
		}}},
		{TLS: &tls.Config{EncryptedClientHelloKeys: []tls.EncryptedClientHelloKey{{}}}},
	}
	for index, security := range tests {
		if err := security.Validate(); !errors.Is(err, ErrInvalidSecurityConfig) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
}

func TestClientCertificateProviderIsBoundedOwnedAndPanicSafe(t *testing.T) {

	providedCertificate := newTestTLSCertificate(t)
	providedCertificate.OCSPStaple = []byte{7}
	providedCertificate.SignedCertificateTimestamps = [][]byte{{8}}
	firstCertificateByte := providedCertificate.Certificate[0][0]
	provider := clientCertificateProviderStub{provider: ClientCertificateProviderFunc(func(
		ctx context.Context,
		request ClientCertificateRequest,
	) (tls.Certificate, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatalf("certificate provider deadline = %v, %t", deadline, ok)
		}
		if len(request.AcceptableCAs) != 1 || request.AcceptableCAs[0][0] != 4 ||
			len(request.SignatureSchemes) != 1 || request.Version != tls.VersionTLS13 {
			t.Fatalf("certificate request = %#v", request)
		}
		request.AcceptableCAs[0][0] = 9

		return providedCertificate, nil
	})}
	security, err := normalizeClientSecurity(ClientSecurity{
		ClientCertificateProvider: provider,
		CredentialTimeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("normalize mTLS: %v", err)
	}
	acceptableCA := []byte{4, 5, 6}
	certificate, err := security.TLS.GetClientCertificate(&tls.CertificateRequestInfo{
		AcceptableCAs:    [][]byte{acceptableCA},
		SignatureSchemes: []tls.SignatureScheme{tls.Ed25519},
		Version:          tls.VersionTLS13,
	})
	if err != nil {
		t.Fatalf("GetClientCertificate(): %v", err)
	}
	providedCertificate.Certificate[0][0] ^= 0xff
	if acceptableCA[0] != 4 ||
		certificate.Certificate[0][0] != firstCertificateByte ||
		certificate.OCSPStaple[0] != 7 ||
		certificate.SignedCertificateTimestamps[0][0] != 8 {
		t.Fatalf("certificate provider aliased request or result: %#v", certificate)
	}

	panicking, err := normalizeClientSecurity(ClientSecurity{
		ClientCertificateProvider: ClientCertificateProviderFunc(func(
			context.Context,
			ClientCertificateRequest,
		) (tls.Certificate, error) {
			panic("secret certificate panic")
		}),
	})
	if err != nil {
		t.Fatalf("normalize panicking mTLS: %v", err)
	}
	_, err = panicking.TLS.GetClientCertificate(&tls.CertificateRequestInfo{})
	if !errors.Is(err, ErrCredentialProviderPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("panicking certificate provider error = %v", err)
	}
}

func TestClientCertificateProviderReceivesTLSHandshakeContext(t *testing.T) {

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	var providerCalled atomic.Bool
	security, err := normalizeClientSecurity(ClientSecurity{
		TLS: &tls.Config{RootCAs: roots},
		ClientCertificateProvider: ClientCertificateProviderFunc(func(
			ctx context.Context,
			_ ClientCertificateRequest,
		) (tls.Certificate, error) {
			if ctx == nil {
				t.Fatal("client certificate provider context is nil")
			}
			providerCalled.Store(true)

			return server.TLS.Certificates[0], nil
		}),
	})
	if err != nil {
		t.Fatalf("normalize mTLS: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: security.TLS,
	}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET test TLS server: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close test response: %v", closeErr)
	}
	if !providerCalled.Load() {
		t.Fatal("client certificate provider was not called")
	}
}

func TestClientCertificateProviderAcceptsNilTLSRequest(t *testing.T) {

	provided := newTestTLSCertificate(t)
	certificate, err := callClientCertificateProvider(
		nil,
		time.Second,
		ClientCertificateProviderFunc(func(
			ctx context.Context,
			request ClientCertificateRequest,
		) (tls.Certificate, error) {
			if ctx == nil {
				t.Fatal("client certificate provider context is nil")
			}
			if _, exists := ctx.Deadline(); !exists {
				t.Fatal("client certificate provider context is unbounded")
			}
			if len(request.AcceptableCAs) != 0 ||
				len(request.SignatureSchemes) != 0 ||
				request.Version != 0 {
				t.Fatalf("nil TLS request metadata = %#v", request)
			}

			return provided, nil
		}),
	)
	if err != nil {
		t.Fatalf("callClientCertificateProvider(nil) error = %v", err)
	}
	if certificate == nil || len(certificate.Certificate) == 0 {
		t.Fatalf("callClientCertificateProvider(nil) certificate = %#v", certificate)
	}
}

func TestAuthenticationCharacterPoliciesCoverExactBoundaries(t *testing.T) {

	tokenCharacters := map[byte]bool{
		'a': true, 'z': true,
		'A': true, 'Z': true,
		'0': true, '9': true,
		'-': true, '.': true, '_': true, '~': true,
		'+': true, '/': true,
		'@': false, '[': false, '`': false, '{': false,
		':': false, 0: false,
	}
	for character, want := range tokenCharacters {
		if got := validOAuthBearerTokenCharacter(character); got != want {
			t.Fatalf(
				"validOAuthBearerTokenCharacter(%#x) = %t, want %t",
				character,
				got,
				want,
			)
		}
	}

	extensionKeyCharacters := map[rune]bool{
		'a': true, 'z': true,
		'A': true, 'Z': true,
		'@': false, '[': false, '`': false, '{': false,
		'0': false,
	}
	for character, want := range extensionKeyCharacters {
		if got := validOAuthExtensionKeyCharacter(character); got != want {
			t.Fatalf(
				"validOAuthExtensionKeyCharacter(%#x) = %t, want %t",
				character,
				got,
				want,
			)
		}
	}

	extensionValueCharacters := map[byte]bool{
		0x21: true,
		0x7e: true,
		' ':  true,
		'\t': true,
		'\r': true,
		'\n': true,
		0x00: false,
		0x1f: false,
		0x7f: false,
	}
	for character, want := range extensionValueCharacters {
		if got := validOAuthExtensionValueCharacter(character); got != want {
			t.Fatalf(
				"validOAuthExtensionValueCharacter(%#x) = %t, want %t",
				character,
				got,
				want,
			)
		}
	}
}

func TestCredentialTextPolicyIsolatedRules(t *testing.T) {

	tests := []struct {
		name     string
		value    string
		required bool
		want     bool
	}{
		{name: "required empty", required: true},
		{name: "optional empty", required: false, want: true},
		{name: "maximum", value: strings.Repeat("a", 8<<10), required: true, want: true},
		{name: "oversized", value: strings.Repeat("a", (8<<10)+1), required: true},
		{name: "invalid UTF-8", value: string([]byte{0xff}), required: true},
		{name: "NUL", value: "a\x00b", required: true},
		{name: "whitespace", value: " \t", required: true},
		{name: "valid", value: " service ", required: true, want: true},
	}
	for _, test := range tests {
		if got := validCredentialText(test.value, test.required); got != test.want {
			t.Fatalf("%s validCredentialText() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestClientCertificateProviderRejectsErrorsAndInvalidMaterial(t *testing.T) {

	providerFailure := errors.New("certificate secret-key failed")
	validCertificate := newTestTLSCertificate(t)
	oversizedTimestampCertificate := validCertificate
	oversizedTimestampCertificate.SignedCertificateTimestamps = [][]byte{
		make([]byte, 1<<20),
	}
	nonSignerCertificate := validCertificate
	nonSignerCertificate.PrivateKey = struct{}{}
	tests := []struct {
		name        string
		certificate tls.Certificate
		providerErr error
	}{
		{name: "provider error", providerErr: providerFailure},
		{name: "empty chain", certificate: tls.Certificate{PrivateKey: struct{}{}}},
		{
			name: "too many certificates",
			certificate: tls.Certificate{
				Certificate: make([][]byte, 17), PrivateKey: struct{}{},
			},
		},
		{name: "missing private key", certificate: tls.Certificate{Certificate: [][]byte{{1}}}},
		{
			name: "empty certificate",
			certificate: tls.Certificate{
				Certificate: [][]byte{{}}, PrivateKey: struct{}{},
			},
		},
		{
			name: "oversized OCSP staple",
			certificate: tls.Certificate{
				Certificate: [][]byte{{1}}, PrivateKey: struct{}{},
				OCSPStaple: make([]byte, (1<<20)+1),
			},
		},
		{
			name: "oversized certificate",
			certificate: tls.Certificate{
				Certificate: [][]byte{make([]byte, (1<<20)+1)}, PrivateKey: struct{}{},
			},
		},
		{
			name:        "oversized signed timestamp",
			certificate: oversizedTimestampCertificate,
		},
		{name: "private key is not a signer", certificate: nonSignerCertificate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			security, err := normalizeClientSecurity(ClientSecurity{
				ClientCertificateProvider: ClientCertificateProviderFunc(func(
					context.Context,
					ClientCertificateRequest,
				) (tls.Certificate, error) {
					return test.certificate, test.providerErr
				}),
			})
			if err != nil {
				t.Fatalf("normalize mTLS: %v", err)
			}
			_, err = security.TLS.GetClientCertificate(&tls.CertificateRequestInfo{})
			if !errors.Is(err, ErrCredentialProviderFailed) ||
				strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("GetClientCertificate() error = %v", err)
			}
			if test.providerErr == nil && !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("invalid certificate error = %v", err)
			}
			if test.providerErr != nil && !errors.Is(err, test.providerErr) {
				t.Fatalf("provider identity error = %v", err)
			}
		})
	}
}

func TestClientCertificateProviderRejectsUnboundedBrokerRequestBeforeCallback(t *testing.T) {

	var providerCalled atomic.Bool
	security, err := normalizeClientSecurity(ClientSecurity{
		ClientCertificateProvider: ClientCertificateProviderFunc(func(
			context.Context,
			ClientCertificateRequest,
		) (tls.Certificate, error) {
			providerCalled.Store(true)

			return tls.Certificate{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("normalize mTLS: %v", err)
	}
	requests := []*tls.CertificateRequestInfo{
		{AcceptableCAs: make([][]byte, 65)},
		{SignatureSchemes: make([]tls.SignatureScheme, 65)},
		{AcceptableCAs: [][]byte{make([]byte, (64<<10)+1)}},
	}
	for index, request := range requests {
		_, err = security.TLS.GetClientCertificate(request)
		if !errors.Is(err, ErrInvalidClientCertificateRequest) || providerCalled.Load() {
			t.Fatalf(
				"request %d error = %v, provider called = %t",
				index,
				err,
				providerCalled.Load(),
			)
		}
	}
}
