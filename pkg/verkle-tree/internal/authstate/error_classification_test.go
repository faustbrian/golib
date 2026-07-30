package authstate

import (
	"errors"
	"testing"
)

func TestFacadeErrorClassifiers(t *testing.T) {
	t.Parallel()

	other := errors.New("different")
	if !IsDuplicateKeyError(errDuplicateKey) ||
		IsDuplicateKeyError(other) ||
		!IsProofVerificationError(errProofVerification) ||
		IsProofVerificationError(other) ||
		!IsInvalidProofError(errInvalidTreeProof) ||
		!IsInvalidProofError(errInvalidStemPath) ||
		!IsInvalidProofError(errInvalidPathCommitment) ||
		IsInvalidProofError(other) ||
		!IsInvalidProofEncodingError(errInvalidTreeProofEncoding) ||
		IsInvalidProofEncodingError(other) {
		t.Fatal("facade error classifier mismatch")
	}
}
