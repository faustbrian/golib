// Package profile owns the exact package-defined profile descriptor without
// creating an import cycle between the public facade and internal machinery.
package profile

import "errors"

// ErrUnsupported identifies an unknown, zero, or internally inconsistent
// Verkle profile.
var ErrUnsupported = errors.New("unsupported Verkle profile")

// ID is the stable identity of a complete Verkle convention.
type ID uint8

const (
	// BandersnatchIPA256V0 identifies the package-owned pre-v1 profile.
	BandersnatchIPA256V0 ID = iota + 1
)

const bandersnatchIPA256V0Name = "verkletree-bandersnatch-ipa-256-v0"

// Profile is an immutable, comparable complete Verkle convention descriptor.
type Profile struct {
	id              ID
	name            string
	version         uint16
	branchingWidth  uint16
	keySize         uint16
	stemSize        uint16
	valueSize       uint16
	encodingVersion uint16
}

// BandersnatchIPA256V0Profile returns the only package-defined profile.
func BandersnatchIPA256V0Profile() Profile {
	return Profile{
		id:              BandersnatchIPA256V0,
		name:            bandersnatchIPA256V0Name,
		version:         0,
		branchingWidth:  256,
		keySize:         32,
		stemSize:        31,
		valueSize:       32,
		encodingVersion: 1,
	}
}

// ID returns the stable identity of the complete profile definition.
func (profile Profile) ID() ID {
	return profile.id
}

// Name returns the immutable profile name.
func (profile Profile) Name() string {
	return profile.name
}

// Version returns the profile definition version.
func (profile Profile) Version() uint16 {
	return profile.version
}

// BranchingWidth returns the fixed number of positions in an inner node.
func (profile Profile) BranchingWidth() uint16 {
	return profile.branchingWidth
}

// KeySize returns the required key length in bytes.
func (profile Profile) KeySize() uint16 {
	return profile.keySize
}

// StemSize returns the fixed key-stem length in bytes.
func (profile Profile) StemSize() uint16 {
	return profile.stemSize
}

// ValueSize returns the required value length in bytes.
func (profile Profile) ValueSize() uint16 {
	return profile.valueSize
}

// EncodingVersion returns the canonical container-encoding version.
func (profile Profile) EncodingVersion() uint16 {
	return profile.encodingVersion
}

// Validate rejects every value except the exact package-owned definition.
func (profile Profile) Validate() error {
	if profile != BandersnatchIPA256V0Profile() {
		return ErrUnsupported
	}

	return nil
}
