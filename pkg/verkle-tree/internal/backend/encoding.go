package backend

import (
	"errors"
	"fmt"

	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
)

const (
	// CommitmentSize is the fixed canonical Banderwagon commitment length.
	CommitmentSize = banderwagon.CompressedSize

	commitmentSize = CommitmentSize
	scalarSize     = fr.Bytes
)

var (
	errInvalidCommitment = errors.New("invalid commitment")
	errInvalidScalar     = errors.New("invalid scalar")
)

type commitment struct {
	element banderwagon.Element
}

type scalar struct {
	element fr.Element
}

func encodeCommitment(value commitment) [commitmentSize]byte {
	return value.element.Bytes()
}

func commitmentToScalar(value commitment) scalar {
	var mapped fr.Element
	value.element.MapToScalarField(&mapped)

	return scalar{element: mapped}
}

func decodeCommitment(encoded []byte) (commitment, error) {
	if len(encoded) != commitmentSize {
		return commitment{}, fmt.Errorf("%w: encoded length", errInvalidCommitment)
	}

	var canonical [commitmentSize]byte
	copy(canonical[:], encoded)
	if canonical == [commitmentSize]byte{} {
		return commitment{}, fmt.Errorf("%w: identity", errInvalidCommitment)
	}

	var element banderwagon.Element
	if err := element.SetBytes(canonical[:]); err != nil {
		return commitment{}, fmt.Errorf("%w: point encoding", errInvalidCommitment)
	}

	return commitment{element: element}, nil
}

func decodeOpeningProofPoint(encoded []byte) (commitment, error) {
	if len(encoded) != commitmentSize {
		return commitment{}, fmt.Errorf("%w: encoded length", errInvalidCommitment)
	}

	var canonical [commitmentSize]byte
	copy(canonical[:], encoded)
	if canonical == [commitmentSize]byte{} {
		var identity banderwagon.Element
		identity.SetIdentity()

		return commitment{element: identity}, nil
	}

	return decodeCommitment(canonical[:])
}

func encodeScalar(value scalar) [scalarSize]byte {
	return value.element.BytesLE()
}

func decodeScalar(encoded []byte) (scalar, error) {
	if len(encoded) != scalarSize {
		return scalar{}, fmt.Errorf("%w: encoded length", errInvalidScalar)
	}

	var canonical [scalarSize]byte
	copy(canonical[:], encoded)

	var element fr.Element
	if _, err := element.SetBytesLECanonical(canonical[:]); err != nil {
		return scalar{}, fmt.Errorf("%w: field encoding", errInvalidScalar)
	}

	return scalar{element: element}, nil
}
