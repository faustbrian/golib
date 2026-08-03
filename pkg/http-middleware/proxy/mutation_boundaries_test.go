package proxy

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestExactProxyPolicyBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	trusted := make([]netip.Prefix, maximumTrustedPrefixes)
	for index := range trusted {
		trusted[index] = netip.MustParsePrefix("10.0.0.0/8")
	}
	for name, policy := range map[string]Policy{
		"minimum hops":         {MaxHops: 1},
		"maximum hops":         {MaxHops: 128},
		"minimum header bytes": {MaxHeaderBytes: 1},
		"maximum header bytes": {MaxHeaderBytes: 1 << 20},
		"maximum trusted":      {Trusted: trusted},
	} {
		if _, err := New(policy); err != nil {
			t.Fatalf("New(%s exact bound) error = %v", name, err)
		}
	}
}

func TestClientSelectionStopsAtIndependentTrustBounds(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	trustedPeer := netip.MustParseAddr("10.0.0.1")
	if got := selectClient(trustedPeer, nil, trusted); got != trustedPeer {
		t.Fatalf("empty hop selection = %s", got)
	}
	untrustedPeer := netip.MustParseAddr("192.0.2.1")
	hops := []netip.Addr{netip.MustParseAddr("198.51.100.1")}
	if got := selectClient(untrustedPeer, hops, trusted); got != untrustedPeer {
		t.Fatalf("untrusted peer selection = %s", got)
	}
}

func TestForwardedFieldAcceptsExactValueLength(t *testing.T) {
	t.Parallel()

	exact := strings.Repeat("a", 4096)
	value, present, valid := forwardedField(
		http.Header{"X-Test": {exact}}, "X-Test", 1, len(exact),
	)
	if value != exact || !present || !valid {
		t.Fatalf("forwardedField(exact) = length %d, %v, %v", len(value), present, valid)
	}
}

func TestExactHostAndPrefixLengthBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	host := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ":65535"
	if len(host) != 255 || !validHost(host) {
		t.Fatalf("exact host length = %d, valid = %v", len(host), validHost(host))
	}
	prefix := "/" + strings.Repeat("a", 255)
	if len(prefix) != 256 || !validPrefix(prefix) {
		t.Fatalf("exact prefix length = %d, valid = %v", len(prefix), validPrefix(prefix))
	}
}

func TestHostAndPrefixRejectIndependentFieldBounds(t *testing.T) {
	t.Parallel()

	if validHost("example\thost") {
		t.Fatal("host with a control character was accepted")
	}
	if validPrefix("/" + strings.Repeat("a", 256)) {
		t.Fatal("257-byte prefix was accepted")
	}
}

func TestForwardedValueAcceptsEmptyAndEscapedQuotedValues(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		`""`:     "",
		`"a\\b"`: `a\b`,
	} {
		if got, ok := forwardedValue(raw); !ok || got != want {
			t.Fatalf("forwardedValue(%q) = %q, %v", raw, got, ok)
		}
	}
	if _, ok := forwardedValue(`"a\`); ok {
		t.Fatal("trailing quoted escape was accepted")
	}
}
