package rules

import (
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	validation "github.com/faustbrian/golib/pkg/validation"
)

var (
	uuidPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)
	hostLabelPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

// URL requires an absolute HTTP or HTTPS URL with a host.
func URL() validation.Validator[string] {
	return stringPredicate("url", func(value string) bool {
		parsed, err := url.ParseRequestURI(value)
		return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
	})
}

// Hostname requires an RFC-style DNS hostname bounded to 253 bytes.
func Hostname() validation.Validator[string] {
	return stringPredicate("hostname", func(value string) bool {
		value = strings.TrimSuffix(value, ".")
		if value == "" || len(value) > 253 {
			return false
		}
		for _, label := range strings.Split(value, ".") {
			if !hostLabelPattern.MatchString(label) {
				return false
			}
		}
		return true
	})
}

// IP requires an IPv4 or IPv6 literal.
func IP() validation.Validator[string] {
	return stringPredicate("ip", func(value string) bool { return net.ParseIP(value) != nil })
}

// CIDR requires an IPv4 or IPv6 prefix.
func CIDR() validation.Validator[string] {
	return stringPredicate("cidr", func(value string) bool {
		_, _, err := net.ParseCIDR(value)
		return err == nil
	})
}

// Email requires a bare mailbox address rather than a display-name form.
func Email() validation.Validator[string] {
	return stringPredicate("email", func(value string) bool {
		address, err := mail.ParseAddress(value)
		return err == nil && address.Address == value && strings.Contains(value, "@")
	})
}

// UUID requires a canonical RFC 9562 UUID string.
func UUID() validation.Validator[string] {
	return stringPredicate("uuid", uuidPattern.MatchString)
}

// Identifier requires a bounded ASCII application identifier.
func Identifier() validation.Validator[string] {
	return stringPredicate("identifier", identifierPattern.MatchString)
}

func stringPredicate(code string, predicate func(string) bool) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(ctx validation.Context, value string) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if predicate(value) {
			return pass(ctx)
		}
		return fail(ctx, code, nil)
	})
}
