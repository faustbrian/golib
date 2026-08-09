package kafka

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

var (
	ErrInvalidSecurityConfig = errors.New(
		"kafka: client security configuration is invalid",
	)
	ErrCredentialProviderFailed = errors.New(
		"kafka: credential provider failed",
	)
	ErrCredentialProviderPanic = errors.New(
		"kafka: credential provider panicked",
	)
	ErrInvalidCredentials = errors.New(
		"kafka: credential provider returned invalid credentials",
	)
	ErrExpiredOAuthBearerToken = errors.New(
		"kafka: credential provider returned an expired OAuth bearer token",
	)
	ErrInvalidClientCertificateRequest = errors.New(
		"kafka: TLS client certificate request exceeds policy limits",
	)
	ErrInvalidTrustAnchors = errors.New(
		"kafka: trust-anchor provider returned invalid certificates",
	)
)

const (
	maxTrustAnchorCertificates = 64
	maxTrustAnchorBytes        = 1 << 20
)

// TransportSecurity selects verified TLS or an explicitly development-only
// plaintext connection. The zero value is verified TLS.
type TransportSecurity uint8

const (
	// TransportTLS requires verified TLS with TLS 1.2 or newer.
	TransportTLS TransportSecurity = iota
	// TransportDevelopmentPlaintext permits an unencrypted connection for
	// isolated development fixtures. Authentication is forbidden in this mode.
	TransportDevelopmentPlaintext
)

// AuthenticationMethod identifies one supported Kafka SASL mechanism.
type AuthenticationMethod uint8

const (
	// AuthenticationNone disables SASL authentication.
	AuthenticationNone AuthenticationMethod = iota
	// AuthenticationPlain selects SASL/PLAIN over verified TLS.
	AuthenticationPlain
	// AuthenticationSCRAMSHA256 selects SCRAM-SHA-256 over verified TLS.
	AuthenticationSCRAMSHA256
	// AuthenticationSCRAMSHA512 selects SCRAM-SHA-512 over verified TLS.
	AuthenticationSCRAMSHA512
	// AuthenticationOAuthBearer selects OAUTHBEARER over verified TLS.
	AuthenticationOAuthBearer
)

// UsernamePassword contains one owned credential result. String formatting is
// always redacted; callers remain responsible for not logging the fields.
type UsernamePassword struct {
	Username        string
	Password        []byte
	AuthorizationID string
}

// String returns a stable redacted representation.
func (credentials UsernamePassword) String() string {
	return "kafka.UsernamePassword{redacted}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (credentials UsernamePassword) GoString() string {
	return credentials.String()
}

// UsernamePasswordProvider returns fresh credentials for one authentication
// session. Implementations must be concurrency-safe and honor ctx.
type UsernamePasswordProvider interface {
	Credentials(context.Context) (UsernamePassword, error)
}

// UsernamePasswordProviderFunc adapts a function to UsernamePasswordProvider.
type UsernamePasswordProviderFunc func(context.Context) (UsernamePassword, error)

// Credentials invokes provider.
func (provider UsernamePasswordProviderFunc) Credentials(
	ctx context.Context,
) (UsernamePassword, error) {
	return provider(ctx)
}

// OAuthBearerToken contains one owned token result. ExpiresAt is required so
// expired credentials fail before any authentication bytes are constructed.
// String formatting is always redacted.
type OAuthBearerToken struct {
	Token           []byte
	ExpiresAt       time.Time
	AuthorizationID string
	Extensions      map[string]string
}

// String returns a stable redacted representation.
func (token OAuthBearerToken) String() string {
	return "kafka.OAuthBearerToken{redacted}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (token OAuthBearerToken) GoString() string {
	return token.String()
}

// OAuthBearerProvider returns a fresh token for one authentication session.
// Implementations must be concurrency-safe and honor ctx.
type OAuthBearerProvider interface {
	Token(context.Context) (OAuthBearerToken, error)
}

// OAuthBearerProviderFunc adapts a function to OAuthBearerProvider.
type OAuthBearerProviderFunc func(context.Context) (OAuthBearerToken, error)

// Token invokes provider.
func (provider OAuthBearerProviderFunc) Token(
	ctx context.Context,
) (OAuthBearerToken, error) {
	return provider(ctx)
}

// ClientCertificateRequest is an owned, bounded view of the TLS server's
// client-certificate request.
type ClientCertificateRequest struct {
	AcceptableCAs    [][]byte
	SignatureSchemes []tls.SignatureScheme
	Version          uint16
}

// ClientCertificateProvider returns a fresh mTLS certificate for one
// handshake. Implementations must be concurrency-safe and honor ctx.
type ClientCertificateProvider interface {
	ClientCertificate(context.Context, ClientCertificateRequest) (tls.Certificate, error)
}

// ClientCertificateProviderFunc adapts a function to
// ClientCertificateProvider.
type ClientCertificateProviderFunc func(
	context.Context,
	ClientCertificateRequest,
) (tls.Certificate, error)

// ClientCertificate invokes provider.
func (provider ClientCertificateProviderFunc) ClientCertificate(
	ctx context.Context,
	request ClientCertificateRequest,
) (tls.Certificate, error) {
	return provider(ctx, request)
}

// TrustAnchors contains one complete result from a TrustAnchorProvider. Each
// certificate must be one DER-encoded X.509 trust anchor. Ownership of the
// returned slice structure and certificate bytes transfers to the package; a
// provider must not mutate or reuse them after returning. The package retains
// only owned copies. String formatting is always redacted.
type TrustAnchors struct {
	Certificates [][]byte
}

// String returns a stable redacted representation.
func (anchors TrustAnchors) String() string {
	return "kafka.TrustAnchors{redacted}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (anchors TrustAnchors) GoString() string {
	return anchors.String()
}

// TrustAnchorProvider returns the complete root set for one new TLS
// connection. Implementations must be concurrency-safe and honor ctx.
type TrustAnchorProvider interface {
	TrustAnchors(context.Context) (TrustAnchors, error)
}

// TrustAnchorProviderFunc adapts a function to TrustAnchorProvider.
type TrustAnchorProviderFunc func(context.Context) (TrustAnchors, error)

// TrustAnchors invokes provider.
func (provider TrustAnchorProviderFunc) TrustAnchors(
	ctx context.Context,
) (TrustAnchors, error) {
	return provider(ctx)
}

// Authentication is an immutable, redacted authentication policy constructed
// by one of the New*Authentication functions.
type Authentication struct {
	method                   AuthenticationMethod
	usernamePasswordProvider UsernamePasswordProvider
	oauthBearerProvider      OAuthBearerProvider
}

// NewSCRAMSHA256Authentication selects rotating SCRAM-SHA-256 credentials.
func NewSCRAMSHA256Authentication(
	provider UsernamePasswordProvider,
) Authentication {
	return Authentication{
		method:                   AuthenticationSCRAMSHA256,
		usernamePasswordProvider: provider,
	}
}

// NewSCRAMSHA512Authentication selects rotating SCRAM-SHA-512 credentials.
func NewSCRAMSHA512Authentication(
	provider UsernamePasswordProvider,
) Authentication {
	return Authentication{
		method:                   AuthenticationSCRAMSHA512,
		usernamePasswordProvider: provider,
	}
}

// NewOAuthBearerAuthentication selects a rotating bounded OAUTHBEARER token
// provider.
func NewOAuthBearerAuthentication(
	provider OAuthBearerProvider,
) Authentication {
	return Authentication{
		method:              AuthenticationOAuthBearer,
		oauthBearerProvider: provider,
	}
}

// NewPlainAuthentication selects rotating SASL/PLAIN credentials. Validation
// rejects a nil provider and any plaintext transport configuration.
func NewPlainAuthentication(
	provider UsernamePasswordProvider,
) Authentication {
	return Authentication{
		method:                   AuthenticationPlain,
		usernamePasswordProvider: provider,
	}
}

// Method returns the stable authentication method.
func (authentication Authentication) Method() AuthenticationMethod {
	return authentication.method
}

// String returns a stable redacted representation.
func (authentication Authentication) String() string {
	return "kafka.Authentication{method:" + authentication.method.String() + "}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (authentication Authentication) GoString() string {
	return authentication.String()
}

// String returns the stable authentication method name.
func (method AuthenticationMethod) String() string {
	switch method {
	case AuthenticationNone:
		return "none"
	case AuthenticationPlain:
		return "plain"
	case AuthenticationSCRAMSHA256:
		return "scram-sha-256"
	case AuthenticationSCRAMSHA512:
		return "scram-sha-512"
	case AuthenticationOAuthBearer:
		return "oauthbearer"
	default:
		return "unknown"
	}
}

// ClientSecurity configures transport and optional authentication. The zero
// value uses verified TLS with system roots and a TLS 1.2 minimum. Construction
// clones mutable TLS slices, certificate bytes, and root pools. Interface
// values, private keys, TLS callbacks, and session caches remain caller-owned
// and must be immutable or concurrency-safe for the client's lifetime.
type ClientSecurity struct {
	Transport                 TransportSecurity
	TLS                       *tls.Config
	Authentication            Authentication
	ClientCertificateProvider ClientCertificateProvider
	TrustAnchorProvider       TrustAnchorProvider
	CredentialTimeout         time.Duration
}

// Validate reports whether the transport and authentication policy is safe and
// internally consistent without allocating a Kafka client or invoking a
// credential provider.
func (security ClientSecurity) Validate() error {
	_, err := normalizeClientSecurity(security)

	return err
}

// DevelopmentPlaintextSecurity returns the explicit development-only
// unencrypted transport policy.
func DevelopmentPlaintextSecurity() ClientSecurity {
	return ClientSecurity{Transport: TransportDevelopmentPlaintext}
}

// String returns a stable representation that cannot include credentials,
// certificates, roots, or callback internals.
func (security ClientSecurity) String() string {
	return "kafka.ClientSecurity{transport:" + security.Transport.String() +
		",authentication:" + security.Authentication.Method().String() + "}"
}

// GoString returns a stable redacted representation for %#v formatting.
func (security ClientSecurity) GoString() string {
	return security.String()
}

// String returns the stable transport policy name.
func (transport TransportSecurity) String() string {
	switch transport {
	case TransportTLS:
		return "tls"
	case TransportDevelopmentPlaintext:
		return "development-plaintext"
	default:
		return "unknown"
	}
}

func normalizeClientSecurity(security ClientSecurity) (ClientSecurity, error) {
	if security.Transport > TransportDevelopmentPlaintext {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if security.Transport == TransportDevelopmentPlaintext {
		if security.TLS != nil ||
			security.Authentication.Method() != AuthenticationNone ||
			security.ClientCertificateProvider != nil ||
			security.TrustAnchorProvider != nil ||
			security.CredentialTimeout != 0 {
			return ClientSecurity{}, ErrInvalidSecurityConfig
		}

		return security, nil
	}

	tlsConfig := security.TLS
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		if !validTLSConfigMaterial(tlsConfig) {
			return ClientSecurity{}, ErrInvalidSecurityConfig
		}
		tlsConfig = cloneTLSConfig(tlsConfig)
	}
	if tlsConfig.InsecureSkipVerify ||
		(tlsConfig.MinVersion != 0 && tlsConfig.MinVersion != tls.VersionTLS12 &&
			tlsConfig.MinVersion != tls.VersionTLS13) ||
		(tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion != tls.VersionTLS12 &&
			tlsConfig.MaxVersion != tls.VersionTLS13) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	if tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tlsConfig.MinVersion {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	security.TLS = tlsConfig

	switch security.Authentication.Method() {
	case AuthenticationNone:
	case AuthenticationPlain, AuthenticationSCRAMSHA256, AuthenticationSCRAMSHA512:
		if !validUsernamePasswordProvider(
			security.Authentication.usernamePasswordProvider,
		) {
			return ClientSecurity{}, ErrInvalidSecurityConfig
		}
	case AuthenticationOAuthBearer:
		if !validOAuthBearerProvider(security.Authentication.oauthBearerProvider) {
			return ClientSecurity{}, ErrInvalidSecurityConfig
		}
	default:
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if !validClientCertificateProvider(security.ClientCertificateProvider) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if !validTrustAnchorProvider(security.TrustAnchorProvider) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if security.ClientCertificateProvider != nil &&
		(len(security.TLS.Certificates) != 0 ||
			security.TLS.GetClientCertificate != nil) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if security.TrustAnchorProvider != nil &&
		(security.TLS.RootCAs != nil || security.TLS.ClientSessionCache != nil) {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	usesCredentialProvider := security.Authentication.Method() != AuthenticationNone ||
		security.ClientCertificateProvider != nil || security.TrustAnchorProvider != nil
	if usesCredentialProvider {
		if security.CredentialTimeout == 0 {
			security.CredentialTimeout = 5 * time.Second
		}
		if security.CredentialTimeout < 100*time.Millisecond ||
			security.CredentialTimeout > time.Minute {
			return ClientSecurity{}, ErrInvalidSecurityConfig
		}
	} else if security.CredentialTimeout != 0 {
		return ClientSecurity{}, ErrInvalidSecurityConfig
	}
	if security.ClientCertificateProvider != nil {
		provider := security.ClientCertificateProvider
		timeout := security.CredentialTimeout
		security.TLS.GetClientCertificate = func(
			request *tls.CertificateRequestInfo,
		) (*tls.Certificate, error) {
			return callClientCertificateProvider(request, timeout, provider)
		}
	}

	return security, nil
}

func validUsernamePasswordProvider(provider UsernamePasswordProvider) bool {
	return provider != nil && !typedNilProvider(provider)
}

func validOAuthBearerProvider(provider OAuthBearerProvider) bool {
	return provider != nil && !typedNilProvider(provider)
}

func validClientCertificateProvider(provider ClientCertificateProvider) bool {
	if provider == nil {
		return true
	}

	return !typedNilProvider(provider)
}

func validTrustAnchorProvider(provider TrustAnchorProvider) bool {
	if provider == nil {
		return true
	}

	return !typedNilProvider(provider)
}

func typedNilProvider(provider any) bool {
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func cloneTLSConfig(source *tls.Config) *tls.Config {
	cloned := source.Clone()
	if source.RootCAs != nil {
		cloned.RootCAs = source.RootCAs.Clone()
	}
	cloned.NextProtos = append([]string(nil), source.NextProtos...)
	cloned.CipherSuites = append([]uint16(nil), source.CipherSuites...)
	cloned.CurvePreferences = append([]tls.CurveID(nil), source.CurvePreferences...)
	cloned.EncryptedClientHelloConfigList = append(
		[]byte(nil), source.EncryptedClientHelloConfigList...,
	)
	cloned.Certificates = make([]tls.Certificate, len(source.Certificates))
	for index, certificate := range source.Certificates {
		cloned.Certificates[index] = cloneTLSCertificate(certificate)
	}

	return cloned
}

func validTLSConfigMaterial(config *tls.Config) bool {
	if (config.ServerName != "" && !validTLSServerName(config.ServerName)) ||
		config.GetClientCertificate != nil || len(config.Certificates) > 16 ||
		len(config.NextProtos) > 16 ||
		len(config.CipherSuites) > tls12CipherSuiteCount() ||
		len(config.CurvePreferences) > 7 ||
		len(config.EncryptedClientHelloConfigList) != 0 ||
		config.KeyLogWriter != nil || config.Renegotiation != tls.RenegotiateNever ||
		hasDuplicates(config.NextProtos) || hasDuplicates(config.CipherSuites) ||
		hasDuplicates(config.CurvePreferences) ||
		!validCipherSuites(config.CipherSuites) ||
		!validCurvePreferences(config.CurvePreferences) ||
		hasServerTLSFields(config) {
		return false
	}
	for _, certificate := range config.Certificates {
		if !validClientCertificate(certificate) {
			return false
		}
	}
	for _, protocol := range config.NextProtos {
		if len(protocol) == 0 || len(protocol) > 255 {
			return false
		}
	}

	return true
}

func tls12CipherSuiteCount() int {
	count := 0
	for _, suite := range tls.CipherSuites() {
		if slices.Contains(suite.SupportedVersions, tls.VersionTLS12) {
			count++
		}
	}

	return count
}

func hasServerTLSFields(config *tls.Config) bool {
	//lint:ignore SA1019 The policy must reject this deprecated server-only field.
	hasLegacyNameMap := config.NameToCertificate != nil //nolint:staticcheck // Required rejection of a deprecated server field.
	//lint:ignore SA1019 The policy must reject this deprecated server-only field.
	hasLegacyCipherPreference := config.PreferServerCipherSuites //nolint:staticcheck // Required rejection of a deprecated server field.
	//lint:ignore SA1019 The policy must reject this deprecated server-only field.
	hasLegacyTicketKey := config.SessionTicketKey != [32]byte{} //nolint:staticcheck // Required rejection of a deprecated server field.

	return hasLegacyNameMap || config.GetCertificate != nil ||
		config.GetConfigForClient != nil || config.ClientAuth != tls.NoClientCert ||
		config.ClientCAs != nil || hasLegacyCipherPreference ||
		hasLegacyTicketKey || config.UnwrapSession != nil ||
		config.WrapSession != nil ||
		config.EncryptedClientHelloRejectionVerify != nil ||
		config.GetEncryptedClientHelloKeys != nil ||
		len(config.EncryptedClientHelloKeys) != 0
}

func validCipherSuites(configured []uint16) bool {
	for _, configuredID := range configured {
		found := false
		for _, safeSuite := range tls.CipherSuites() {
			if configuredID != safeSuite.ID {
				continue
			}
			found = slices.Contains(
				safeSuite.SupportedVersions,
				tls.VersionTLS12,
			)
		}
		if !found {
			return false
		}
	}

	return true
}

func validCurvePreferences(configured []tls.CurveID) bool {
	for _, curve := range configured {
		switch curve {
		case tls.CurveP256, tls.CurveP384, tls.CurveP521, tls.X25519,
			tls.X25519MLKEM768, tls.SecP256r1MLKEM768, tls.SecP384r1MLKEM1024:
		default:
			return false
		}
	}

	return true
}

func hasDuplicates[T comparable](values []T) bool {
	seen := make(map[T]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}

	return false
}

func cloneTLSCertificate(certificate tls.Certificate) tls.Certificate {
	cloned := certificate
	cloned.Certificate = cloneByteSlices(certificate.Certificate)
	cloned.SupportedSignatureAlgorithms = append(
		[]tls.SignatureScheme(nil),
		certificate.SupportedSignatureAlgorithms...,
	)
	cloned.OCSPStaple = append([]byte(nil), certificate.OCSPStaple...)
	cloned.SignedCertificateTimestamps = cloneByteSlices(
		certificate.SignedCertificateTimestamps,
	)
	// Leaf is a parsed cache of Certificate[0]. Dropping it prevents sharing a
	// caller-mutable parsed certificate while retaining the wire chain.
	cloned.Leaf = nil

	return cloned
}

func cloneByteSlices(source [][]byte) [][]byte {
	if source == nil {
		return nil
	}
	cloned := make([][]byte, len(source))
	for index, value := range source {
		cloned[index] = append([]byte(nil), value...)
	}

	return cloned
}

func clientSecurityOptions(
	security ClientSecurity,
	dialTimeout time.Duration,
) []kgo.Opt {
	options := make([]kgo.Opt, 0, 2)
	if security.Transport == TransportTLS {
		if security.TrustAnchorProvider == nil {
			options = append(options, kgo.DialTLSConfig(security.TLS))
		} else {
			options = append(options, kgo.Dialer(trustAnchorDialer(
				security,
				dialTimeout,
			)))
		}
	}
	if mechanism := security.Authentication.saslMechanism(
		security.CredentialTimeout,
	); mechanism != nil {
		options = append(options, kgo.SASL(mechanism))
	}

	return options
}

func trustAnchorDialer(
	security ClientSecurity,
	dialTimeout time.Duration,
) func(context.Context, string, string) (net.Conn, error) {
	baseTLSConfig := security.TLS
	provider := security.TrustAnchorProvider
	providerTimeout := security.CredentialTimeout
	netDialer := &net.Dialer{Timeout: dialTimeout}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		serverName := baseTLSConfig.ServerName
		if serverName == "" {
			var splitErr error
			serverName, _, splitErr = net.SplitHostPort(address)
			if splitErr != nil || !validTLSServerName(serverName) {
				return nil, ErrInvalidBroker
			}
		}
		roots, err := callTrustAnchorProvider(dialCtx, providerTimeout, provider)
		if err != nil {
			return nil, err
		}
		tlsConfig := cloneTLSConfig(baseTLSConfig)
		tlsConfig.RootCAs = roots
		tlsConfig.ServerName = serverName

		return (&tls.Dialer{
			NetDialer: netDialer,
			Config:    tlsConfig,
		}).DialContext(dialCtx, network, address)
	}
}

func validTLSServerName(serverName string) bool {
	return serverName != "" && serverName == strings.TrimSpace(serverName) &&
		validKafkaText(serverName, 255)
}

func (authentication Authentication) saslMechanism(
	timeout time.Duration,
) sasl.Mechanism {
	switch authentication.Method() {
	case AuthenticationPlain:
		return plain.Plain(func(ctx context.Context) (plain.Auth, error) {
			credentials, err := callUsernamePasswordProvider(
				ctx, timeout, authentication.usernamePasswordProvider,
			)
			if err != nil {
				return plain.Auth{}, err
			}

			return plain.Auth{
				Zid:  credentials.AuthorizationID,
				User: credentials.Username,
				Pass: string(append([]byte(nil), credentials.Password...)),
			}, nil
		})
	case AuthenticationSCRAMSHA256:
		return scram.Sha256(authentication.scramProvider(timeout))
	case AuthenticationSCRAMSHA512:
		return scram.Sha512(authentication.scramProvider(timeout))
	case AuthenticationOAuthBearer:
		mechanism := oauth.Oauth(func(ctx context.Context) (oauth.Auth, error) {
			token, err := callOAuthBearerProvider(
				ctx, timeout, authentication.oauthBearerProvider,
			)
			if err != nil {
				return oauth.Auth{}, err
			}

			return oauth.Auth{
				Zid:        token.AuthorizationID,
				Token:      string(append([]byte(nil), token.Token...)),
				Extensions: cloneStringMap(token.Extensions),
			}, nil
		})

		return oauthAuthenticationMechanism{delegate: mechanism}
	default:
		return nil
	}
}

type oauthAuthenticationMechanism struct {
	delegate sasl.Mechanism
}

func (mechanism oauthAuthenticationMechanism) Name() string {
	return mechanism.delegate.Name()
}

func (mechanism oauthAuthenticationMechanism) Authenticate(
	ctx context.Context,
	host string,
) (sasl.Session, []byte, error) {
	session, initial, err := mechanism.delegate.Authenticate(ctx, host)
	if err != nil {
		return nil, nil, err
	}

	return oauthAuthenticationSession{delegate: session}, initial, nil
}

type oauthAuthenticationSession struct {
	delegate sasl.Session
}

func (session oauthAuthenticationSession) Challenge(
	challenge []byte,
) (bool, []byte, error) {
	done, next, err := session.delegate.Challenge(challenge)
	if err != nil {
		// Kafka returns an RFC 7628 error challenge with a successful SASL
		// response code. franz-go deliberately rejects the non-empty challenge
		// but exposes only an unclassifiable generic error. Preserve no broker
		// payload while restoring Kafka's stable authentication-failure identity.
		return false, nil, kerr.SaslAuthenticationFailed
	}

	return done, next, nil
}

func (authentication Authentication) scramProvider(
	timeout time.Duration,
) func(context.Context) (scram.Auth, error) {
	return func(ctx context.Context) (scram.Auth, error) {
		credentials, err := callUsernamePasswordProvider(
			ctx, timeout, authentication.usernamePasswordProvider,
		)
		if err != nil {
			return scram.Auth{}, err
		}

		return scram.Auth{
			Zid:  credentials.AuthorizationID,
			User: credentials.Username,
			Pass: string(append([]byte(nil), credentials.Password...)),
		}, nil
	}
}

type credentialProviderError struct {
	cause error
}

func (err *credentialProviderError) Error() string {
	return ErrCredentialProviderFailed.Error()
}

func (err *credentialProviderError) Unwrap() []error {
	return []error{ErrCredentialProviderFailed, err.cause}
}

func newCredentialProviderError(cause error) error {
	return &credentialProviderError{cause: cause}
}

func callUsernamePasswordProvider(
	ctx context.Context,
	timeout time.Duration,
	provider UsernamePasswordProvider,
) (credentials UsernamePassword, err error) {
	providerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			credentials = UsernamePassword{}
			err = newCredentialProviderError(ErrCredentialProviderPanic)
		}
	}()

	credentials, err = provider.Credentials(providerCtx)
	if err != nil {
		return UsernamePassword{}, newCredentialProviderError(err)
	}
	if !validCredentialText(credentials.Username, true) ||
		!validCredentialText(credentials.AuthorizationID, false) ||
		len(credentials.Password) == 0 || len(credentials.Password) > 8<<10 ||
		!utf8.Valid(credentials.Password) || bytes.IndexByte(credentials.Password, 0) >= 0 {
		return UsernamePassword{}, newCredentialProviderError(ErrInvalidCredentials)
	}
	credentials.Password = append([]byte(nil), credentials.Password...)

	return credentials, nil
}

func callOAuthBearerProvider(
	ctx context.Context,
	timeout time.Duration,
	provider OAuthBearerProvider,
) (token OAuthBearerToken, err error) {
	providerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			token = OAuthBearerToken{}
			err = newCredentialProviderError(ErrCredentialProviderPanic)
		}
	}()

	token, err = provider.Token(providerCtx)
	if err != nil {
		return OAuthBearerToken{}, newCredentialProviderError(err)
	}
	if !validOAuthBearerToken(token.Token) ||
		!validOAuthAuthorizationID(token.AuthorizationID) ||
		!validOAuthExtensions(token.Extensions) {
		return OAuthBearerToken{}, newCredentialProviderError(ErrInvalidCredentials)
	}
	if token.ExpiresAt.IsZero() || !token.ExpiresAt.After(time.Now()) {
		return OAuthBearerToken{}, newCredentialProviderError(
			ErrExpiredOAuthBearerToken,
		)
	}
	token.Token = append([]byte(nil), token.Token...)
	token.Extensions = cloneStringMap(token.Extensions)

	return token, nil
}

func validOAuthBearerToken(token []byte) bool {
	if len(token) == 0 || len(token) > 1<<20 {
		return false
	}
	padding := false
	for _, character := range token {
		if character == '=' {
			padding = true
			continue
		}
		if padding || !validOAuthBearerTokenCharacter(character) {
			return false
		}
	}

	return true
}

func validOAuthBearerTokenCharacter(character byte) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') ||
		character == '-' || character == '.' || character == '_' ||
		character == '~' || character == '+' || character == '/'
}

func validOAuthAuthorizationID(value string) bool {
	if !validCredentialText(value, false) {
		return false
	}
	for _, character := range value {
		if character == ',' || character == '=' || unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func callTrustAnchorProvider(
	ctx context.Context,
	timeout time.Duration,
	provider TrustAnchorProvider,
) (roots *x509.CertPool, err error) {
	providerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			roots = nil
			err = newCredentialProviderError(ErrCredentialProviderPanic)
		}
	}()

	anchors, providerErr := provider.TrustAnchors(providerCtx)
	if providerErr != nil {
		return nil, newCredentialProviderError(providerErr)
	}
	roots, valid := trustAnchorPool(anchors)
	if !valid {
		return nil, newCredentialProviderError(ErrInvalidTrustAnchors)
	}

	return roots, nil
}

func trustAnchorPool(anchors TrustAnchors) (*x509.CertPool, bool) {
	if !validTrustAnchorCount(len(anchors.Certificates)) {
		return nil, false
	}

	roots := x509.NewCertPool()
	seen := make(map[string]struct{}, len(anchors.Certificates))
	totalBytes := 0
	for _, encoded := range anchors.Certificates {
		if !trustAnchorBytesFit(totalBytes, len(encoded)) {
			return nil, false
		}
		owned := append([]byte(nil), encoded...)
		key := string(owned)
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		certificate, parseErr := x509.ParseCertificate(owned)
		if parseErr != nil {
			return nil, false
		}
		roots.AddCert(certificate)
		totalBytes += len(owned)
	}

	return roots, true
}

func validTrustAnchorCount(count int) bool {
	return count > 0 && count <= maxTrustAnchorCertificates
}

func trustAnchorBytesFit(totalBytes, certificateBytes int) bool {
	return certificateBytes > 0 && totalBytes >= 0 &&
		certificateBytes <= maxTrustAnchorBytes-totalBytes
}

func callClientCertificateProvider(
	request *tls.CertificateRequestInfo,
	timeout time.Duration,
	provider ClientCertificateProvider,
) (certificate *tls.Certificate, err error) {
	ctx := context.Background()
	if request != nil {
		if requestCtx := request.Context(); requestCtx != nil {
			ctx = requestCtx
		}
	}
	providerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			certificate = nil
			err = newCredentialProviderError(ErrCredentialProviderPanic)
		}
	}()

	ownedRequest := ClientCertificateRequest{}
	if request != nil {
		if !validClientCertificateRequest(request) {
			return nil, ErrInvalidClientCertificateRequest
		}
		ownedRequest = ClientCertificateRequest{
			AcceptableCAs: cloneByteSlices(request.AcceptableCAs),
			SignatureSchemes: append(
				[]tls.SignatureScheme(nil), request.SignatureSchemes...,
			),
			Version: request.Version,
		}
	}
	provided, providerErr := provider.ClientCertificate(providerCtx, ownedRequest)
	if providerErr != nil {
		return nil, newCredentialProviderError(providerErr)
	}
	if !validClientCertificate(provided) {
		return nil, newCredentialProviderError(ErrInvalidCredentials)
	}
	cloned := cloneTLSCertificate(provided)

	return &cloned, nil
}

func validClientCertificateRequest(request *tls.CertificateRequestInfo) bool {
	if len(request.AcceptableCAs) > 64 || len(request.SignatureSchemes) > 64 {
		return false
	}
	totalBytes := 0
	for _, acceptableCA := range request.AcceptableCAs {
		if len(acceptableCA) > (64<<10)-totalBytes {
			return false
		}
		totalBytes += len(acceptableCA)
	}

	return true
}

func validClientCertificate(certificate tls.Certificate) bool {
	if len(certificate.Certificate) == 0 ||
		len(certificate.Certificate) > 16 || certificate.PrivateKey == nil ||
		len(certificate.SupportedSignatureAlgorithms) > 32 ||
		hasDuplicates(certificate.SupportedSignatureAlgorithms) {
		return false
	}
	totalBytes := len(certificate.OCSPStaple)
	var leaf *x509.Certificate
	for index, encoded := range certificate.Certificate {
		if len(encoded) == 0 || len(encoded) > 1<<20-totalBytes {
			return false
		}
		parsed, err := x509.ParseCertificate(encoded)
		if err != nil {
			return false
		}
		if index == 0 {
			leaf = parsed
		}
		totalBytes += len(encoded)
	}
	for _, timestamp := range certificate.SignedCertificateTimestamps {
		if len(timestamp) > 1<<20-totalBytes {
			return false
		}
		totalBytes += len(timestamp)
	}
	signer, ok := certificate.PrivateKey.(crypto.Signer)
	if !ok || !matchingPublicKeys(leaf.PublicKey, signer.Public()) {
		return false
	}

	return true
}

func matchingPublicKeys(first, second any) bool {
	firstEncoded, firstErr := x509.MarshalPKIXPublicKey(first)
	secondEncoded, secondErr := x509.MarshalPKIXPublicKey(second)
	if firstErr != nil {
		return false
	}
	if secondErr != nil {
		return false
	}

	return bytes.Equal(firstEncoded, secondEncoded)
}

func validCredentialText(value string, required bool) bool {
	if value == "" {
		return !required
	}

	return len(value) <= 8<<10 && utf8.ValidString(value) &&
		!strings.ContainsRune(value, 0) && strings.TrimSpace(value) != ""
}

func validOAuthExtensions(extensions map[string]string) bool {
	if len(extensions) > 32 {
		return false
	}
	for key, value := range extensions {
		if key == "" || len(key) > 128 || len(value) > 8<<10 ||
			key == "auth" {
			return false
		}
		for _, character := range key {
			if !validOAuthExtensionKeyCharacter(character) {
				return false
			}
		}
		for _, character := range []byte(value) {
			if !validOAuthExtensionValueCharacter(character) {
				return false
			}
		}
	}

	return true
}

func validOAuthExtensionKeyCharacter(character rune) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z')
}

func validOAuthExtensionValueCharacter(character byte) bool {
	return (character >= 0x21 && character <= 0x7e) ||
		character == ' ' || character == '\t' || character == '\r' ||
		character == '\n'
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}

	return cloned
}
