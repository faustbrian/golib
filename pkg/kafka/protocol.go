package kafka

import (
	"errors"
	"strconv"
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
	if policy.MinimumVersion != strings.TrimSpace(policy.MinimumVersion) {
		return ErrInvalidProtocolPolicy
	}
	if !validKafkaText(policy.MinimumVersion, 16) {
		return ErrInvalidProtocolPolicy
	}
	if kversion.FromString(policy.MinimumVersion) == nil {
		return ErrInvalidProtocolPolicy
	}

	return nil
}

func kafkaReleaseAtLeast(value string, minimumMajor int, minimumMinor int) bool {
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}

	return major > minimumMajor ||
		(major == minimumMajor && minor >= minimumMinor)
}

func clientProtocolOptions(policy ProtocolPolicy) []kgo.Opt {
	if policy.MinimumVersion == "" {
		return nil
	}

	return []kgo.Opt{kgo.MinVersions(kversion.FromString(policy.MinimumVersion))}
}
