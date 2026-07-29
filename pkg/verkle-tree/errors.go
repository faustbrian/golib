package verkletree

import "errors"

var (
	// ErrUnsupportedProfile identifies an unknown, zero, or internally
	// inconsistent Verkle profile.
	ErrUnsupportedProfile = errors.New("unsupported Verkle profile")
)
