package merkletree

// ProfileID is the stable identity of a Merkle-tree convention. Profile
// identity is part of every Root and must be checked at interoperability
// boundaries.
type ProfileID uint8

const (
	// ProfileCanonicalBinary is the package-owned canonical binary profile. In
	// version 1 it uses ordered raw leaves, SHA-256, RFC 9162 leaf and branch
	// domain separation, the RFC 9162 largest-lower-power-of-two shape, and
	// SHA-256 of the empty string as its empty root.
	ProfileCanonicalBinary ProfileID = iota + 1

	// ProfileRFC9162 is the Certificate Transparency Version 2.0 Merkle Tree
	// Hash profile from RFC 9162.
	ProfileRFC9162
)

// HashAlgorithm is a stable hash algorithm identifier. It is not a general
// assertion that any algorithm satisfying a Go interface is cryptographically
// safe.
type HashAlgorithm uint8

const (
	// HashSHA256 is SHA-256 with a 32-byte digest. For RFC 9162 it corresponds
	// to hash algorithm registry value 0x00.
	HashSHA256 HashAlgorithm = iota + 1
)

// Profile is an immutable, comparable Merkle convention descriptor. Its zero
// value is invalid and fails with ErrUnsupportedProfile.
type Profile struct {
	id        ProfileID
	version   uint16
	algorithm HashAlgorithm
}

// CanonicalProfile returns version 1 of the package-owned canonical binary
// profile. The hash algorithm is fixed to SHA-256.
func CanonicalProfile() Profile {
	return Profile{
		id:        ProfileCanonicalBinary,
		version:   1,
		algorithm: HashSHA256,
	}
}

// RFC9162Profile returns version 1 of the RFC 9162 profile for algorithm.
// RFC 9162 assigns SHA-256 registry value 0x00; no other registered algorithm
// is currently supported.
func RFC9162Profile(algorithm HashAlgorithm) (Profile, error) {
	if algorithm != HashSHA256 {
		return Profile{}, ErrUnsupportedAlgorithm
	}

	return Profile{
		id:        ProfileRFC9162,
		version:   1,
		algorithm: algorithm,
	}, nil
}

// ID returns the stable profile identity.
func (profile Profile) ID() ProfileID {
	return profile.id
}

// Version returns the profile version.
func (profile Profile) Version() uint16 {
	return profile.version
}

// Algorithm returns the profile's immutable hash algorithm.
func (profile Profile) Algorithm() HashAlgorithm {
	return profile.algorithm
}

func (profile Profile) validate() error {
	switch profile.id {
	case ProfileCanonicalBinary:
		if profile == CanonicalProfile() {
			return nil
		}
	case ProfileRFC9162:
		rfc, err := RFC9162Profile(profile.algorithm)
		if err == nil && profile == rfc {
			return nil
		}
	}

	return ErrUnsupportedProfile
}
