package international

import (
	"fmt"
	"regexp"
	"slices"
	"time"
)

var sha256Pattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

// Provenance records enough information to reproduce and review a dataset.
type Provenance struct {
	Dataset         string    `json:"dataset"`
	Source          string    `json:"source"`
	RetrievedAt     time.Time `json:"retrieved_at"`
	UpstreamVersion string    `json:"upstream_version"`
	License         string    `json:"license"`
	SHA256          string    `json:"sha256"`
	Generator       string    `json:"generator"`
	Transformations []string  `json:"transformations"`
}

// Validate verifies required provenance fields without network access.
func (provenance Provenance) Validate() error {
	switch {
	case provenance.Dataset == "":
		return invalidProvenance("dataset is required")
	case provenance.Source == "":
		return invalidProvenance("source is required")
	case provenance.RetrievedAt.IsZero():
		return invalidProvenance("retrieval date is required")
	case provenance.UpstreamVersion == "":
		return invalidProvenance("upstream version is required")
	case provenance.License == "":
		return invalidProvenance("license is required")
	case !sha256Pattern.MatchString(provenance.SHA256):
		return invalidProvenance("SHA-256 must be lowercase hexadecimal")
	case provenance.Generator == "":
		return invalidProvenance("generator is required")
	case len(provenance.Transformations) == 0:
		return invalidProvenance("transformations are required")
	default:
		return nil
	}
}

// Equal reports whether all immutable provenance fields match.
func (provenance Provenance) Equal(other Provenance) bool {
	return provenance.Dataset == other.Dataset &&
		provenance.Source == other.Source &&
		provenance.RetrievedAt.Equal(other.RetrievedAt) &&
		provenance.UpstreamVersion == other.UpstreamVersion &&
		provenance.License == other.License &&
		provenance.SHA256 == other.SHA256 &&
		provenance.Generator == other.Generator &&
		slices.Equal(provenance.Transformations, other.Transformations)
}

func invalidProvenance(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProvenance, reason)
}
