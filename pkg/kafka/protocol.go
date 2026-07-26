package kafka

import (
	"errors"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

var ErrInvalidProtocolPolicy = errors.New("kafka: protocol policy is invalid")

// ProtocolPolicy controls Kafka request-version negotiation without exposing
// franz-go version types. An empty MinimumVersion uses franz-go's negotiated
// request versions without a package-imposed downgrade floor.
type ProtocolPolicy struct {
	MinimumVersion string
}

// Validate reports whether the configured minimum is a Kafka release known to
// the pinned franz-go version table.
func (policy ProtocolPolicy) Validate() error {
	if policy.MinimumVersion == "" {
		return nil
	}
	if policy.MinimumVersion != strings.TrimSpace(policy.MinimumVersion) ||
		!validKafkaText(policy.MinimumVersion, 16) ||
		kversion.FromString(policy.MinimumVersion) == nil {
		return ErrInvalidProtocolPolicy
	}

	return nil
}

func clientProtocolOptions(policy ProtocolPolicy) []kgo.Opt {
	if policy.MinimumVersion == "" {
		return nil
	}

	return []kgo.Opt{kgo.MinVersions(kversion.FromString(policy.MinimumVersion))}
}
