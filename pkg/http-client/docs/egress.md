# Egress Security

Destination policy is explicit. Construct an immutable `EgressPolicy` and
attach it to a client that uses the package-owned standard transport:

```go
egress, err := httpclient.NewEgressPolicy(httpclient.EgressOptions{
	AllowedSchemes: []string{"https"},
	AllowedHosts:   []string{"api.example.com"},
	AllowedPorts:   []uint16{443},
	AllowedOrigins: []string{"https://api.example.com"},
	AllowedCIDRs:   []string{"203.0.113.0/24"},
})
if err != nil {
	return err
}

client, err := httpclient.New(httpclient.Config{Egress: egress})
```

An empty `EgressOptions` permits HTTPS on port 443 to public addresses. Host,
origin, and CIDR allowlists are optional intersections; denied CIDRs always
win. DNS names must be ASCII or already punycoded and are matched exactly.
Wildcards, user information, malformed authorities, dynamic scheme changes,
and unsupported schemes are rejected.

Private, loopback, link-local, multicast, and known metadata-service addresses
are denied by default. Each class has a separate explicit opt-in. Metadata
service access requires `AllowMetadataService` even when link-local access is
enabled. Unix and other non-TCP dial networks are rejected.

## Connection-time enforcement

Every physical attempt validates its final URL after attempt middleware. The
same policy runs for redirects. The owned transport resolves a DNS name before
connecting, validates every returned address, and performs no dial when any
answer is denied. It then dials only the validated numeric addresses, avoiding
a second resolver lookup between validation and connection.

Proxy connections pass through the same dialer, so the proxy host, port, and
resolved addresses must also satisfy the policy. A host allowlist therefore
needs to include an explicitly trusted proxy when environment proxy settings
are in use.

`Config.Egress` cannot be combined with a caller-supplied `RoundTripper`: the
package cannot prove that a custom transport validates DNS, proxy, Unix-socket,
or alternate-protocol destinations at connection time. The standard transport
retains its finite DNS/connect/TLS/header/total timeouts, TLS 1.2 minimum,
system trust roots, and HTTP/2 support.

`EgressError` exposes only a stable low-cardinality `Reason` and matches
`ErrEgressDenied`. Its rendered text contains no host, IP address, path, query,
userinfo, or resolver cause.

## TLS trust and identity

The package-owned transport defaults to TLS 1.2 or newer and platform roots.
`TLSPolicy` can replace the roots, fix the expected server name, configure a
client certificate, and require one of several SHA-256 Subject Public Key Info
pins:

```go
tlsPolicy, err := httpclient.NewTLSPolicy(httpclient.TLSOptions{
	MinimumVersion:      tls.VersionTLS13,
	RootCertificatesPEM: privateRoots,
	ServerName:          "api.example.com",
	SPKISHA256Pins:      [][sha256.Size]byte{currentPin, nextPin},
})
if err != nil {
	return err
}

client, err := httpclient.New(httpclient.Config{
	Egress: egress,
	TLS:    tlsPolicy,
})
```

Root and client-certificate PEM input and pin slices are snapshotted. Custom
roots replace rather than extend platform roots. Normal certificate-chain and
hostname verification always runs before optional pin matching; there is no
insecure-skip option. Multiple pins support bounded key rotation.

Like egress enforcement, `Config.TLS` cannot be combined with a custom
`RoundTripper`, because the package cannot prove that it preserves the
configured verification policy. A pin mismatch matches `ErrTLSPinMismatch`
without rendering certificates, names, or pin material.
