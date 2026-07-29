package verkletree

// ProfileID is the stable identity of a complete Verkle convention. It binds
// the tree layout, commitment construction, generator set, transcript, and
// canonical encodings; it is not a caller-composable algorithm identifier.
type ProfileID uint8

const (
	// ProfileBandersnatchIPA256V0 identifies the package-owned experimental
	// 256-wide Bandersnatch/Banderwagon Pedersen-plus-IPA profile. It is not a
	// stable or Ethereum-compatible profile.
	ProfileBandersnatchIPA256V0 ProfileID = iota + 1
)

const (
	bandersnatchIPA256V0Name = "verkletree-bandersnatch-ipa-256-v0"
)

// Profile is an immutable, comparable Verkle convention descriptor. Its zero
// value is invalid and Validate reports ErrUnsupportedProfile. Callers cannot
// compose widths, curves, generators, transcripts, or encodings at runtime.
type Profile struct {
	id              ProfileID
	name            string
	version         uint16
	branchingWidth  uint16
	keySize         uint16
	stemSize        uint16
	valueSize       uint16
	encodingVersion uint16
	experimental    bool
}

// ExperimentalBandersnatchIPA256V0 returns the package-owned experimental
// profile. Its identity fixes a 256-wide tree, 32-byte keys split into a
// 31-byte stem and one-byte suffix, 32-byte values, the Bandersnatch/
// Banderwagon Pedersen-plus-IPA construction, the eth_verkle_oct_2021
// generator set, and the verkle transcript. It MUST NOT be treated as stable,
// audited, production-ready, or Ethereum-compatible.
func ExperimentalBandersnatchIPA256V0() Profile {
	return Profile{
		id:              ProfileBandersnatchIPA256V0,
		name:            bandersnatchIPA256V0Name,
		version:         0,
		branchingWidth:  256,
		keySize:         32,
		stemSize:        31,
		valueSize:       32,
		encodingVersion: 1,
		experimental:    true,
	}
}

// ID returns the stable identity of the complete profile definition.
func (profile Profile) ID() ProfileID {
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

// Experimental reports whether the profile is prohibited from stable or
// production-readiness claims.
func (profile Profile) Experimental() bool {
	return profile.experimental
}

// BranchingWidth returns the fixed number of positions in an inner node.
func (profile Profile) BranchingWidth() uint16 {
	return profile.branchingWidth
}

// KeySize returns the required key length in bytes.
func (profile Profile) KeySize() uint16 {
	return profile.keySize
}

// StemSize returns the key-stem length in bytes. The remaining key byte is the
// leaf suffix.
func (profile Profile) StemSize() uint16 {
	return profile.stemSize
}

// ValueSize returns the required value length in bytes.
func (profile Profile) ValueSize() uint16 {
	return profile.valueSize
}

// EncodingVersion returns the canonical container-encoding version reserved
// for roots, nodes, proofs, witnesses, and snapshots in this profile.
func (profile Profile) EncodingVersion() uint16 {
	return profile.encodingVersion
}

// Validate rejects the zero value and any profile that is not the exact
// package-owned definition. Validation performs no cryptographic work.
func (profile Profile) Validate() error {
	if profile != ExperimentalBandersnatchIPA256V0() {
		return ErrUnsupportedProfile
	}

	return nil
}
