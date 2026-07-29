package merkletree

import (
	"context"
	"crypto/sha256"
	"math/bits"
)

const (
	defaultMaxLeaves     = uint64(1 << 24)
	defaultMaxLeafBytes  = uint64(16 << 20)
	defaultMaxTotalBytes = uint64(1 << 30)
)

// Limits bounds work and allocation derived from raw leaves. Every field must
// be nonzero. Limits is copied by value and is immutable during an operation.
type Limits struct {
	MaxLeaves     uint64
	MaxLeafBytes  uint64
	MaxTotalBytes uint64
}

// DefaultLimits returns the package defaults: 2^24 leaves, 16 MiB per leaf,
// and 1 GiB total raw leaf bytes. Callers processing untrusted input should
// normally choose tighter application-specific limits.
func DefaultLimits() Limits {
	return Limits{
		MaxLeaves:     defaultMaxLeaves,
		MaxLeafBytes:  defaultMaxLeafBytes,
		MaxTotalBytes: defaultMaxTotalBytes,
	}
}

func (limits Limits) validate() error {
	if limits.MaxLeaves == 0 ||
		limits.MaxLeafBytes == 0 ||
		limits.MaxTotalBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// ComputeRoot computes the deterministic root for ordered raw leaves without
// retaining the full tree. It checks ctx between leaves and node merges,
// validates all resource claims before derived allocation, and never retains
// or returns aliases to caller-owned bytes.
//
// The work is O(total leaf bytes + leaf count), and temporary memory is
// O(log(leaf count)) digests. A successful result binds the selected profile,
// profile version, algorithm, and exact tree size.
func ComputeRoot(
	ctx context.Context,
	profile Profile,
	leaves []RawLeaf,
	limits Limits,
) (Root, error) {
	if ctx == nil {
		return Root{}, ErrInvalidContext
	}
	if err := profile.validate(); err != nil {
		return Root{}, err
	}
	if err := limits.validate(); err != nil {
		return Root{}, err
	}
	if err := ctx.Err(); err != nil {
		return Root{}, err
	}

	treeSize := uint64(len(leaves))
	if treeSize > limits.MaxLeaves {
		return Root{}, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.MaxLeaves,
			Actual: treeSize,
		}
	}

	var totalBytes uint64
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return Root{}, err
		}

		leafBytes := uint64(len(leaf.value))
		if leafBytes > limits.MaxLeafBytes {
			return Root{}, &ResourceError{
				Kind:   ResourceLeafBytes,
				Limit:  limits.MaxLeafBytes,
				Actual: leafBytes,
			}
		}
		if leafBytes > limits.MaxTotalBytes-totalBytes {
			return Root{}, &ResourceError{
				Kind:   ResourceTotalBytes,
				Limit:  limits.MaxTotalBytes,
				Actual: saturatedAdd(totalBytes, leafBytes),
			}
		}
		totalBytes += leafBytes
	}

	if treeSize == 0 {
		empty := sha256.Sum256(nil)

		return newRoot(profile, treeSize, empty), nil
	}

	stack := make([][sha256.Size]byte, 0, bits.Len64(treeSize))
	for index, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return Root{}, err
		}

		stack = append(stack, hashLeaf(leaf.value))
		completedSize := uint64(index) + 1
		for completedSize&1 == 0 {
			if err := ctx.Err(); err != nil {
				return Root{}, err
			}

			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, hashBranch(left, right))
			completedSize >>= 1
		}
	}

	rootDigest := stack[len(stack)-1]
	for index := len(stack) - 2; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return Root{}, err
		}

		rootDigest = hashBranch(stack[index], rootDigest)
	}

	return newRoot(profile, treeSize, rootDigest), nil
}

func hashLeaf(value []byte) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(value)

	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))

	return result
}

func hashBranch(
	left [sha256.Size]byte,
	right [sha256.Size]byte,
) [sha256.Size]byte {
	var input [1 + 2*sha256.Size]byte
	input[0] = 1
	copy(input[1:1+sha256.Size], left[:])
	copy(input[1+sha256.Size:], right[:])

	return sha256.Sum256(input[:])
}

func newRoot(
	profile Profile,
	treeSize uint64,
	value [sha256.Size]byte,
) Root {
	return Root{
		profileID:      profile.id,
		profileVersion: profile.version,
		algorithm:      profile.algorithm,
		treeSize:       treeSize,
		digest: Digest{
			algorithm: profile.algorithm,
			value:     value,
		},
	}
}

func saturatedAdd(left, right uint64) uint64 {
	sum, carry := bits.Add64(left, right, 0)
	if carry != 0 {
		return ^uint64(0)
	}

	return sum
}
