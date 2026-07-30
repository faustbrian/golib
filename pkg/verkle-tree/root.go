package verkletree

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

// RootSize is the exact canonical root-container length.
const RootSize = uint32(backend.RootSize)

// RootDecodingLimits bounds hostile canonical root decoding. A zero
// MaxPointDecodes rejects non-empty roots before point decoding.
type RootDecodingLimits struct {
	MaxRootBytes    uint32
	MaxPointDecodes uint32
}

// Root is one immutable profile-bound root. Its zero value rejects use.
type Root struct {
	value backend.Root
}

// DecodeRoot validates and defensively owns one exact root encoding.
func DecodeRoot(
	ctx context.Context,
	encoded []byte,
	limits RootDecodingLimits,
) (Root, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Root{}, err
	}
	if limits.MaxRootBytes == 0 {
		return Root{}, ErrInvalidLimits
	}
	value, err := backend.DecodeRoot(ctx, encoded, backend.RootLimits{
		MaxRootBytes:    limits.MaxRootBytes,
		MaxPointDecodes: limits.MaxPointDecodes,
	})
	if err != nil {
		return Root{}, translateRootDecodingError(err)
	}

	return Root{value: value}, nil
}

func translateRootDecodingError(err error) error {
	var resourceErr *backend.RootResourceError
	if errors.As(err, &resourceErr) {
		resource := ResourceRootBytes
		if resourceErr.Resource == backend.RootResourcePointDecodes {
			resource = ResourcePointDecodes
		}

		return &ResourceError{
			Resource: resource,
			Limit:    resourceErr.Limit,
			Actual:   resourceErr.Actual,
		}
	}
	if errors.Is(err, ErrUnsupportedProfile) {
		return fmt.Errorf("decode root: %w", ErrUnsupportedProfile)
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf(
			"decode root: %w: %w",
			ErrCancelled,
			err,
		)
	}

	return fmt.Errorf("decode root: %w", ErrInvalidRoot)
}

// Bytes returns the exact canonical root encoding by value.
func (root Root) Bytes() ([RootSize]byte, error) {
	encoded, err := root.value.Bytes()
	if err != nil {
		return [RootSize]byte{}, ErrInvalidRoot
	}

	return encoded, nil
}

// Profile returns the immutable profile bound to the root.
func (root Root) Profile() (Profile, error) {
	profile, err := root.value.Profile()
	if err != nil {
		return Profile{}, ErrInvalidRoot
	}

	return profile, nil
}

// IsEmpty reports whether root is the explicit empty-tree root.
func (root Root) IsEmpty() (bool, error) {
	empty, err := root.value.IsEmpty()
	if err != nil {
		return false, ErrInvalidRoot
	}

	return empty, nil
}
