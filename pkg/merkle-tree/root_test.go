package merkletree_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestProfilesMakeRootConventionsExplicit(t *testing.T) {
	t.Parallel()

	canonical := merkletree.CanonicalProfile()
	if canonical.ID() != merkletree.ProfileCanonicalBinary {
		t.Fatalf("canonical profile ID = %v", canonical.ID())
	}
	if canonical.Version() != 1 {
		t.Fatalf("canonical profile version = %d", canonical.Version())
	}
	if canonical.Algorithm() != merkletree.HashSHA256 {
		t.Fatalf("canonical hash algorithm = %v", canonical.Algorithm())
	}

	rfc, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC 9162 profile: %v", err)
	}
	if rfc.ID() != merkletree.ProfileRFC9162 || rfc.Version() != 1 {
		t.Fatalf("RFC 9162 identity = %v version %d", rfc.ID(), rfc.Version())
	}
	if rfc.Algorithm() != merkletree.HashSHA256 {
		t.Fatalf("RFC 9162 hash algorithm = %v", rfc.Algorithm())
	}

	_, err = merkletree.RFC9162Profile(merkletree.HashAlgorithm(255))
	if !errors.Is(err, merkletree.ErrUnsupportedAlgorithm) {
		t.Fatalf("unsupported RFC 9162 algorithm error = %v", err)
	}
}

func TestComputeRootMatchesRFC9162TreeHash(t *testing.T) {
	t.Parallel()

	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC 9162 profile: %v", err)
	}

	for size := 0; size <= 65; size++ {
		leaves := make([]merkletree.RawLeaf, size)
		raw := make([][]byte, size)
		for index := range size {
			value := []byte{byte(index), byte(index >> 8), 0xff}
			raw[index] = value
			leaves[index] = merkletree.NewRawLeaf(value)
		}

		root, rootErr := merkletree.ComputeRoot(
			context.Background(),
			profile,
			leaves,
			merkletree.DefaultLimits(),
		)
		if rootErr != nil {
			t.Fatalf("size %d: compute root: %v", size, rootErr)
		}

		want := referenceTreeHash(raw)
		if got := root.Digest().Bytes(); !equalBytes(got, want) {
			t.Fatalf("size %d: digest = %x, want %x", size, got, want)
		}
		if root.ProfileID() != merkletree.ProfileRFC9162 {
			t.Fatalf("size %d: profile ID = %v", size, root.ProfileID())
		}
		if root.ProfileVersion() != 1 {
			t.Fatalf("size %d: profile version = %d", size, root.ProfileVersion())
		}
		if root.Algorithm() != merkletree.HashSHA256 {
			t.Fatalf("size %d: hash algorithm = %v", size, root.Algorithm())
		}
		if root.Digest().Algorithm() != merkletree.HashSHA256 {
			t.Fatalf(
				"size %d: digest hash algorithm = %v",
				size,
				root.Digest().Algorithm(),
			)
		}
		if root.TreeSize() != uint64(size) {
			t.Fatalf("size %d: tree size = %d", size, root.TreeSize())
		}
	}
}

func TestCanonicalProfileUsesDocumentedRFC9162Shape(t *testing.T) {
	t.Parallel()

	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}

	canonical, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("canonical root: %v", err)
	}

	rfcProfile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC 9162 profile: %v", err)
	}
	rfc, err := merkletree.ComputeRoot(
		context.Background(),
		rfcProfile,
		leaves,
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("RFC 9162 root: %v", err)
	}

	if !equalBytes(canonical.Digest().Bytes(), rfc.Digest().Bytes()) {
		t.Fatalf(
			"canonical digest = %x, RFC 9162 digest = %x",
			canonical.Digest().Bytes(),
			rfc.Digest().Bytes(),
		)
	}
	if canonical.ProfileID() == rfc.ProfileID() {
		t.Fatal("canonical and RFC 9162 roots must retain distinct profile identities")
	}
}

func TestComputeRootRejectsInvalidConfigurationAndResourceClaims(t *testing.T) {
	t.Parallel()

	validProfile := merkletree.CanonicalProfile()
	validLimits := merkletree.Limits{
		MaxLeaves:     2,
		MaxLeafBytes:  3,
		MaxTotalBytes: 4,
	}

	tests := map[string]struct {
		profile merkletree.Profile
		leaves  []merkletree.RawLeaf
		limits  merkletree.Limits
		want    error
		kind    merkletree.ResourceKind
	}{
		"zero profile": {
			profile: merkletree.Profile{},
			limits:  validLimits,
			want:    merkletree.ErrUnsupportedProfile,
		},
		"zero limits": {
			profile: validProfile,
			limits:  merkletree.Limits{},
			want:    merkletree.ErrInvalidLimits,
		},
		"too many leaves": {
			profile: validProfile,
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf(nil),
				merkletree.NewRawLeaf(nil),
				merkletree.NewRawLeaf(nil),
			},
			limits: validLimits,
			want:   merkletree.ErrResourceExhausted,
			kind:   merkletree.ResourceLeaves,
		},
		"leaf too large": {
			profile: validProfile,
			leaves:  []merkletree.RawLeaf{merkletree.NewRawLeaf([]byte("four"))},
			limits:  validLimits,
			want:    merkletree.ErrResourceExhausted,
			kind:    merkletree.ResourceLeafBytes,
		},
		"total too large": {
			profile: validProfile,
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("abc")),
				merkletree.NewRawLeaf([]byte("de")),
			},
			limits: validLimits,
			want:   merkletree.ErrResourceExhausted,
			kind:   merkletree.ResourceTotalBytes,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := merkletree.ComputeRoot(
				context.Background(),
				test.profile,
				test.leaves,
				test.limits,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}

			if test.kind == 0 {
				return
			}

			var resourceError *merkletree.ResourceError
			if !errors.As(err, &resourceError) {
				t.Fatalf("error %T does not expose ResourceError", err)
			}
			if resourceError.Kind != test.kind {
				t.Fatalf("resource kind = %v, want %v", resourceError.Kind, test.kind)
			}
			if resourceError.Limit == 0 || resourceError.Actual <= resourceError.Limit {
				t.Fatalf(
					"resource values actual=%d limit=%d do not describe an excess",
					resourceError.Actual,
					resourceError.Limit,
				)
			}
			if !strings.Contains(resourceError.Error(), "limit") {
				t.Fatalf("resource error does not describe its limit: %v", resourceError)
			}
		})
	}
}

func TestLimitsValidateEachFieldAndAllowExactByteBounds(t *testing.T) {
	t.Parallel()

	valid := merkletree.Limits{
		MaxLeaves:     1,
		MaxLeafBytes:  4,
		MaxTotalBytes: 4,
	}
	for name, mutate := range map[string]func(*merkletree.Limits){
		"leaves": func(limits *merkletree.Limits) {
			limits.MaxLeaves = 0
		},
		"leaf bytes": func(limits *merkletree.Limits) {
			limits.MaxLeafBytes = 0
		},
		"total bytes": func(limits *merkletree.Limits) {
			limits.MaxTotalBytes = 0
		},
	} {
		limits := valid
		mutate(&limits)
		if _, err := merkletree.ComputeRoot(
			context.Background(),
			merkletree.CanonicalProfile(),
			nil,
			limits,
		); !errors.Is(err, merkletree.ErrInvalidLimits) {
			t.Fatalf("%s zero error = %v", name, err)
		}
	}

	if _, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{merkletree.NewRawLeaf([]byte("four"))},
		valid,
	); err != nil {
		t.Fatalf("exact byte bounds: %v", err)
	}

	accumulation := merkletree.Limits{
		MaxLeaves:     3,
		MaxLeafBytes:  2,
		MaxTotalBytes: 5,
	}
	_, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf([]byte("aa")),
			merkletree.NewRawLeaf([]byte("bb")),
			merkletree.NewRawLeaf([]byte("cc")),
		},
		accumulation,
	)
	if !resourceErrorKind(err, merkletree.ResourceTotalBytes) {
		t.Fatalf("accumulated byte error = %v", err)
	}
}

func resourceErrorKind(err error, kind merkletree.ResourceKind) bool {
	var resourceError *merkletree.ResourceError

	return errors.As(err, &resourceError) && resourceError.Kind == kind
}

func TestComputeRootHonorsCancellation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ctx    context.Context
		leaves []merkletree.RawLeaf
	}{
		"before work": {
			ctx: canceledContext(),
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("private leaf")),
			},
		},
		"before leaf hashing": {
			ctx: newCancelAfterErrCalls(2),
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("first")),
			},
		},
		"during leaf validation": {
			ctx: newCancelAfterErrCalls(1),
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("first")),
			},
		},
		"during perfect subtree merge": {
			ctx: newCancelAfterErrCalls(5),
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("first")),
				merkletree.NewRawLeaf([]byte("second")),
			},
		},
		"during final non-power-of-two merge": {
			ctx: newCancelAfterErrCalls(8),
			leaves: []merkletree.RawLeaf{
				merkletree.NewRawLeaf([]byte("first")),
				merkletree.NewRawLeaf([]byte("second")),
				merkletree.NewRawLeaf([]byte("third")),
			},
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := merkletree.ComputeRoot(
				test.ctx,
				merkletree.CanonicalProfile(),
				test.leaves,
				merkletree.DefaultLimits(),
			)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestComputeRootRejectsNilContext(t *testing.T) {
	t.Parallel()

	var ctx context.Context
	_, err := merkletree.ComputeRoot(
		ctx,
		merkletree.CanonicalProfile(),
		nil,
		merkletree.DefaultLimits(),
	)
	if !errors.Is(err, merkletree.ErrInvalidContext) {
		t.Fatalf("error = %v, want ErrInvalidContext", err)
	}
}

func TestLeafAndDigestBytesNeverAliasCallerMemory(t *testing.T) {
	t.Parallel()

	input := []byte("leaf")
	leaf := merkletree.NewRawLeaf(input)
	input[0] = 'L'

	first, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{leaf},
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("first root: %v", err)
	}

	exposedLeaf := leaf.Bytes()
	exposedLeaf[0] = 'X'
	second, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{leaf},
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("second root: %v", err)
	}
	if !equalBytes(first.Digest().Bytes(), second.Digest().Bytes()) {
		t.Fatal("mutating caller or returned leaf bytes changed the root")
	}

	exposedDigest := first.Digest().Bytes()
	exposedDigest[0] ^= 0xff
	if equalBytes(exposedDigest, first.Digest().Bytes()) {
		t.Fatal("mutating returned digest bytes changed the root")
	}
}

func TestNewRawLeafWithLimitBoundsBeforeCopying(t *testing.T) {
	t.Parallel()

	input := []byte("leaf")
	leaf, err := merkletree.NewRawLeafWithLimit(input, uint64(len(input)))
	if err != nil {
		t.Fatalf("bounded leaf: %v", err)
	}
	input[0] = 'L'
	if got := string(leaf.Bytes()); got != "leaf" {
		t.Fatalf("bounded leaf bytes = %q", got)
	}

	_, err = merkletree.NewRawLeafWithLimit(input, 0)
	if !errors.Is(err, merkletree.ErrInvalidLimits) {
		t.Fatalf("zero limit error = %v", err)
	}

	_, err = merkletree.NewRawLeafWithLimit(input, 3)
	if !errors.Is(err, merkletree.ErrResourceExhausted) {
		t.Fatalf("oversized leaf error = %v", err)
	}
	var resourceError *merkletree.ResourceError
	if !errors.As(err, &resourceError) {
		t.Fatalf("oversized leaf error type = %T", err)
	}
	if resourceError.Kind != merkletree.ResourceLeafBytes ||
		resourceError.Limit != 3 ||
		resourceError.Actual != 4 {
		t.Fatalf("oversized leaf resource error = %#v", resourceError)
	}
}

func referenceTreeHash(leaves [][]byte) []byte {
	switch len(leaves) {
	case 0:
		sum := sha256.Sum256(nil)
		return sum[:]
	case 1:
		input := append([]byte{0}, leaves[0]...)
		sum := sha256.Sum256(input)
		return sum[:]
	default:
		split := largestPowerOfTwoBelow(len(leaves))
		left := referenceTreeHash(leaves[:split])
		right := referenceTreeHash(leaves[split:])
		input := make([]byte, 1, 1+len(left)+len(right))
		input[0] = 1
		input = append(input, left...)
		input = append(input, right...)
		sum := sha256.Sum256(input)
		return sum[:]
	}
}

func largestPowerOfTwoBelow(value int) int {
	result := 1
	for result<<1 < value {
		result <<= 1
	}
	return result
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type cancelAfterErrCalls struct {
	remaining int
	done      chan struct{}
}

func (ctx *cancelAfterErrCalls) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *cancelAfterErrCalls) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *cancelAfterErrCalls) Err() error {
	if ctx.remaining == 0 {
		select {
		case <-ctx.done:
		default:
			close(ctx.done)
		}

		return context.Canceled
	}

	ctx.remaining--

	return nil
}

func (ctx *cancelAfterErrCalls) Value(any) any {
	return nil
}

func newCancelAfterErrCalls(remaining int) *cancelAfterErrCalls {
	return &cancelAfterErrCalls{
		remaining: remaining,
		done:      make(chan struct{}),
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}
