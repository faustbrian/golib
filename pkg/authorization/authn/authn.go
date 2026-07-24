// Package authn maps immutable authenticated principals into authorization
// subjects without making authentication depend on authorization.
package authn

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

const defaultMaxGroups = 100

var (
	ErrNilPrincipal       = errors.New("authorization authentication principal is nil")
	ErrAnonymousPrincipal = errors.New("authorization authentication principal is anonymous")
	ErrInvalidPrincipal   = errors.New("authorization authentication principal is invalid")
	ErrInvalidConfig      = errors.New("authorization authentication mapping config is invalid")
	ErrMissingClaim       = errors.New("authorization authentication claim is missing")
	ErrUnsupportedClaim   = errors.New("authorization authentication claim type is unsupported")
	ErrInvalidGroups      = errors.New("authorization authentication groups are invalid")
	ErrGroupLimitExceeded = errors.New("authorization authentication group limit exceeded")
)

// Principal is implemented by authentication Principal values and keeps
// this adapter independent of authentication's package lifecycle.
type Principal interface {
	IsAnonymous() bool
	Subject() string
	Claims() map[string]any
}

type Config struct {
	Kind            authorization.SubjectKind
	AttributeClaims map[authorization.AttributeName]string
	GroupsClaim     string
	MaxGroups       int
}

func Subject(principal Principal, config Config) (authorization.Subject, error) {
	if principal == nil {
		return authorization.Subject{}, ErrNilPrincipal
	}
	if principal.IsAnonymous() {
		return authorization.Subject{}, ErrAnonymousPrincipal
	}
	if principal.Subject() == "" {
		return authorization.Subject{}, ErrInvalidPrincipal
	}
	if config.Kind == "" || config.MaxGroups < 0 {
		return authorization.Subject{}, ErrInvalidConfig
	}
	if config.MaxGroups == 0 {
		config.MaxGroups = defaultMaxGroups
	}
	for attribute, claim := range config.AttributeClaims {
		if attribute == "" || claim == "" {
			return authorization.Subject{}, ErrInvalidConfig
		}
	}

	claims := principal.Claims()
	attributes := make(authorization.Attributes, len(config.AttributeClaims))
	for attribute, claim := range config.AttributeClaims {
		raw, exists := claims[claim]
		if !exists {
			return authorization.Subject{}, fmt.Errorf("claim %q: %w", claim, ErrMissingClaim)
		}
		value, err := claimValue(raw)
		if err != nil {
			return authorization.Subject{}, fmt.Errorf("claim %q: %w", claim, err)
		}
		attributes[attribute] = value
	}

	groups := []authorization.SubjectID(nil)
	if config.GroupsClaim != "" {
		raw, exists := claims[config.GroupsClaim]
		if !exists {
			return authorization.Subject{}, fmt.Errorf("claim %q: %w", config.GroupsClaim, ErrMissingClaim)
		}
		values, ok := stringCollection(raw)
		if !ok {
			return authorization.Subject{}, ErrInvalidGroups
		}
		if len(values) > config.MaxGroups {
			return authorization.Subject{}, ErrGroupLimitExceeded
		}
		groups = make([]authorization.SubjectID, len(values))
		for index, value := range values {
			if value == "" {
				return authorization.Subject{}, ErrInvalidGroups
			}
			groups[index] = authorization.SubjectID(value)
		}
	}

	return authorization.Subject{
		Kind: config.Kind, ID: authorization.SubjectID(principal.Subject()),
		Groups: groups, Attributes: attributes,
	}, nil
}

func stringCollection(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		strings := make([]string, len(values))
		for index, value := range values {
			stringValue, ok := value.(string)
			if !ok {
				return nil, false
			}
			strings[index] = stringValue
		}

		return strings, true
	default:
		return nil, false
	}
}

func claimValue(raw any) (authorization.Value, error) {
	switch value := raw.(type) {
	case nil:
		return authorization.NullValue(), nil
	case string:
		return authorization.StringValue(value), nil
	case bool:
		return authorization.BoolValue(value), nil
	case int:
		return authorization.IntValue(int64(value)), nil
	case int8:
		return authorization.IntValue(int64(value)), nil
	case int16:
		return authorization.IntValue(int64(value)), nil
	case int32:
		return authorization.IntValue(int64(value)), nil
	case int64:
		return authorization.IntValue(value), nil
	case uint:
		return unsignedValue(uint64(value))
	case uint8:
		return authorization.IntValue(int64(value)), nil
	case uint16:
		return authorization.IntValue(int64(value)), nil
	case uint32:
		return authorization.IntValue(int64(value)), nil
	case uint64:
		return unsignedValue(value)
	case float32:
		return authorization.FloatValue(float64(value))
	case float64:
		return authorization.FloatValue(value)
	case json.Number:
		if integer, err := strconv.ParseInt(value.String(), 10, 64); err == nil {
			return authorization.IntValue(integer), nil
		}
		floating, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return authorization.Value{}, ErrUnsupportedClaim
		}
		return authorization.FloatValue(floating)
	case time.Time:
		return authorization.TimeValue(value), nil
	case netip.Addr:
		return authorization.IPValue(value), nil
	case []string:
		return authorization.StringSetValue(value), nil
	case []any:
		strings, ok := stringCollection(value)
		if !ok {
			return authorization.Value{}, ErrUnsupportedClaim
		}
		return authorization.StringSetValue(strings), nil
	default:
		return authorization.Value{}, ErrUnsupportedClaim
	}
}

func unsignedValue(value uint64) (authorization.Value, error) {
	if value > math.MaxInt64 {
		return authorization.Value{}, ErrUnsupportedClaim
	}
	return authorization.IntValue(int64(value)), nil
}
