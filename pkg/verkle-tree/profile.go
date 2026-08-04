package verkletree

import internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"

// ProfileID is the stable identity of a complete Verkle convention. It binds
// the tree layout, commitment construction, generator set, transcript, and
// canonical encodings; it is not a caller-composable algorithm identifier.
type ProfileID = internalprofile.ID

const (
	// ProfileBandersnatchIPA256V0 identifies the package-owned pre-v1 256-wide
	// Bandersnatch/Banderwagon Pedersen-plus-IPA profile. It is not an Ethereum
	// protocol profile.
	ProfileBandersnatchIPA256V0 = internalprofile.BandersnatchIPA256V0
)

// Profile is an immutable, comparable Verkle convention descriptor. Its zero
// value is invalid and Validate reports ErrUnsupportedProfile. Callers cannot
// compose widths, curves, generators, transcripts, or encodings at runtime.
type Profile = internalprofile.Profile

// BandersnatchIPA256V0 returns the package-owned pre-v1 profile. Its identity
// fixes a 256-wide tree, 32-byte keys split into a
// 31-byte stem and one-byte suffix, 32-byte values, the Bandersnatch/
// Banderwagon Pedersen-plus-IPA construction, the eth_verkle_oct_2021
// generator set, and the verkle transcript. Conformance and production-use
// boundaries are documented separately; it is not an Ethereum protocol
// profile.
func BandersnatchIPA256V0() Profile {
	return internalprofile.BandersnatchIPA256V0Profile()
}
