package verkletree

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/authstate"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

func TestFacadeResourceErrorMappings(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		err  error
		want Resource
	}
	tests := make([]testCase, 0, 54)
	add := func(name string, err error, want Resource) {
		tests = append(tests, testCase{name, err, want})
	}
	add("material keys", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourceKeys, Limit: 1, Actual: 2}, ResourceKeys)
	add("material stems", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourceStemPaths, Limit: 1, Actual: 2}, ResourceStemPaths)
	add("material reads", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourceNodeReads, Limit: 1, Actual: 2}, ResourceNodeReads)
	add("material commitments", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourcePathCommitments, Limit: 1, Actual: 2}, ResourcePathCommitments)
	add("material paths", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourcePathBytes, Limit: 1, Actual: 2}, ResourcePathBytes)
	add("material memory", &authstate.ProofMaterialResourceError{Resource: authstate.ProofMaterialResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("verifier queries", &authstate.AggregateVerifierQueryResourceError{Resource: authstate.AggregateVerifierQueryResourceQueries, Limit: 1, Actual: 2}, ResourceQueries)
	add("verifier memory", &authstate.AggregateVerifierQueryResourceError{Resource: authstate.AggregateVerifierQueryResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("proof claims", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourceClaims, Limit: 1, Actual: 2}, ResourceClaims)
	add("proof stems", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourceStemPaths, Limit: 1, Actual: 2}, ResourceStemPaths)
	add("proof commitments", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourcePathCommitments, Limit: 1, Actual: 2}, ResourcePathCommitments)
	add("proof derivations", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourcePathDerivations, Limit: 1, Actual: 2}, ResourcePathDerivations)
	add("proof paths", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourcePathBytes, Limit: 1, Actual: 2}, ResourcePathBytes)
	add("proof memory", &authstate.TreeProofResourceError{Resource: authstate.TreeProofResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("encoding bytes", &authstate.TreeProofEncodingResourceError{Resource: authstate.TreeProofEncodingResourceBytes, Limit: 1, Actual: 2}, ResourceProofBytes)
	add("encoding memory", &authstate.TreeProofEncodingResourceError{Resource: authstate.TreeProofEncodingResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("decoding bytes", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourceBytes, Limit: 1, Actual: 2}, ResourceProofBytes)
	add("decoding claims", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourceClaims, Limit: 1, Actual: 2}, ResourceClaims)
	add("decoding stems", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourceStemPaths, Limit: 1, Actual: 2}, ResourceStemPaths)
	add("decoding commitments", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourcePathCommitments, Limit: 1, Actual: 2}, ResourcePathCommitments)
	add("decoding derivations", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourcePathDerivations, Limit: 1, Actual: 2}, ResourcePathDerivations)
	add("decoding paths", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourcePathBytes, Limit: 1, Actual: 2}, ResourcePathBytes)
	add("decoding points", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourcePointDecodes, Limit: 1, Actual: 2}, ResourcePointDecodes)
	add("decoding scalars", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourceScalarDecodes, Limit: 1, Actual: 2}, ResourceScalarDecodes)
	add("decoding memory", &authstate.TreeProofDecodingResourceError{Resource: authstate.TreeProofDecodingResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("prover keys", &committedtree.AggregateProverQueryResourceError{Resource: committedtree.AggregateProverQueryResourceKeys, Limit: 1, Actual: 2}, ResourceKeys)
	add("prover queries", &committedtree.AggregateProverQueryResourceError{Resource: committedtree.AggregateProverQueryResourceQueries, Limit: 1, Actual: 2}, ResourceQueries)
	add("prover reads", &committedtree.AggregateProverQueryResourceError{Resource: committedtree.AggregateProverQueryResourceNodeReads, Limit: 1, Actual: 2}, ResourceNodeReads)
	add("prover memory", &committedtree.AggregateProverQueryResourceError{Resource: committedtree.AggregateProverQueryResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("opening generators", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceGeneratorDerivations, Limit: 1, Actual: 2}, ResourceGeneratorDerivations)
	add("opening points", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourcePrecomputedPoints, Limit: 1, Actual: 2}, ResourcePrecomputedPoints)
	add("opening queries", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceQueries, Limit: 1, Actual: 2}, ResourceQueries)
	add("opening scalars", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceScalarDecodes, Limit: 1, Actual: 2}, ResourceScalarDecodes)
	add("opening msm", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceMSMTerms, Limit: 1, Actual: 2}, ResourceMSMTerms)
	add("opening memory", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("opening workers", &backend.AggregateOpeningResourceError{Resource: backend.AggregateOpeningResourceWorkers, Limit: 1, Actual: 2}, ResourceWorkers)
	add("opening proof bytes", &backend.OpeningProofResourceError{Resource: backend.OpeningProofResourceBytes, Limit: 1, Actual: 2}, ResourceProofBytes)
	add("opening proof points", &backend.OpeningProofResourceError{Resource: backend.OpeningProofResourcePointDecodes, Limit: 1, Actual: 2}, ResourcePointDecodes)
	add("opening proof scalars", &backend.OpeningProofResourceError{Resource: backend.OpeningProofResourceScalarDecodes, Limit: 1, Actual: 2}, ResourceScalarDecodes)
	add("state entries", &authstate.ResourceError{Resource: authstate.ResourceEntries, Limit: 1, Actual: 2}, ResourceEntries)
	add("state updates", &authstate.ResourceError{Resource: authstate.ResourceBatchUpdates, Limit: 1, Actual: 2}, ResourceBatchUpdates)
	add("state memory", &authstate.ResourceError{Resource: authstate.ResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("tree entries", &committedtree.ResourceError{Resource: committedtree.ResourceEntries, Limit: 1, Actual: 2}, ResourceEntries)
	add("tree stems", &committedtree.ResourceError{Resource: committedtree.ResourceStems, Limit: 1, Actual: 2}, ResourceStems)
	add("tree nodes", &committedtree.ResourceError{Resource: committedtree.ResourceNodes, Limit: 1, Actual: 2}, ResourceNodes)
	add("tree edges", &committedtree.ResourceError{Resource: committedtree.ResourceEdges, Limit: 1, Actual: 2}, ResourceEdges)
	add("tree commitments", &committedtree.ResourceError{Resource: committedtree.ResourceCommitments, Limit: 1, Actual: 2}, ResourceCommitments)
	add("tree mappings", &committedtree.ResourceError{Resource: committedtree.ResourceFieldMappings, Limit: 1, Actual: 2}, ResourceFieldMappings)
	add("tree terms", &committedtree.ResourceError{Resource: committedtree.ResourceCommitmentTerms, Limit: 1, Actual: 2}, ResourceCommitmentTerms)
	add("tree memory", &committedtree.ResourceError{Resource: committedtree.ResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)
	add("commit generators", &backend.CommitmentResourceError{Resource: backend.CommitmentResourceGeneratorDerivations, Limit: 1, Actual: 2}, ResourceGeneratorDerivations)
	add("commit scalars", &backend.CommitmentResourceError{Resource: backend.CommitmentResourceScalarDecodes, Limit: 1, Actual: 2}, ResourceScalarDecodes)
	add("commit msm", &backend.CommitmentResourceError{Resource: backend.CommitmentResourceMSMTerms, Limit: 1, Actual: 2}, ResourceMSMTerms)
	add("commit memory", &backend.CommitmentResourceError{Resource: backend.CommitmentResourceTemporaryBytes, Limit: 1, Actual: 2}, ResourceTemporaryBytes)

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := translateResourceError(test.err)
			var resourceErr *ResourceError
			if !errors.As(got, &resourceErr) ||
				resourceErr.Resource != test.want ||
				resourceErr.Limit != 1 ||
				resourceErr.Actual != 2 ||
				resourceErr.Error() == "" ||
				!errors.Is(resourceErr, ErrResourceExhausted) {
				t.Fatalf("resource error = %#v (%v)", resourceErr, got)
			}
		})
	}
	if got := translateResourceError(errors.New("different")); got != nil {
		t.Fatalf("unrelated resource error = %v", got)
	}
}

func TestFacadeErrorTranslationFallbacks(t *testing.T) {
	t.Parallel()

	resource := &backend.AggregateOpeningResourceError{
		Resource: backend.AggregateOpeningResourceQueries,
		Limit:    1,
		Actual:   2,
	}
	if err := translateSnapshotError("snapshot", context.Canceled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("snapshot cancellation = %v", err)
	}
	if err := translateSnapshotError("snapshot", errors.New("different")); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("snapshot fallback = %v", err)
	}
	if err := translateProofError("proof", context.Canceled, false); !errors.Is(err, ErrCancelled) {
		t.Fatalf("proof cancellation = %v", err)
	}
	if err := translateProofError("proof", context.DeadlineExceeded, false); !errors.Is(err, ErrCancelled) {
		t.Fatalf("proof deadline = %v", err)
	}
	if err := translateProofError("proof", resource, false); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("proof resource = %v", err)
	}
	if err := translateProofError("proof", errors.New("different"), true); !errors.Is(err, ErrVerification) {
		t.Fatalf("proof verification = %v", err)
	}
	if err := translateProofError("proof", errors.New("different"), false); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("proof fallback = %v", err)
	}
	if _, internalErr := (authstate.TreeProof{}).Root(); internalErr == nil {
		t.Fatal("zero internal proof unexpectedly valid")
	} else if err := translateProofError("proof", internalErr, false); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("invalid tree proof = %v", err)
	}
	if err := translateProofEngineError(resource); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("proof engine resource = %v", err)
	}
	if err := translateProofEngineError(context.Canceled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("proof engine cancellation = %v", err)
	}
	if err := translateProofEngineError(errors.New("different")); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("proof engine fallback = %v", err)
	}
	for _, test := range []struct {
		name     string
		err      error
		want     error
		resource Resource
	}{
		{"root bytes", &backend.RootResourceError{Resource: backend.RootResourceBytes, Limit: 1, Actual: 2}, ErrResourceExhausted, ResourceRootBytes},
		{"root points", &backend.RootResourceError{Resource: backend.RootResourcePointDecodes, Limit: 1, Actual: 2}, ErrResourceExhausted, ResourcePointDecodes},
		{"root profile", ErrUnsupportedProfile, ErrUnsupportedProfile, 0},
		{"root cancellation", context.Canceled, ErrCancelled, 0},
		{"root deadline", context.DeadlineExceeded, ErrCancelled, 0},
		{"root malformed", errors.New("different"), ErrInvalidRoot, 0},
	} {
		err := translateRootDecodingError(test.err)
		if !errors.Is(err, test.want) {
			t.Fatalf("%s = %v", test.name, err)
		}
		if test.resource != 0 {
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) || resourceErr.Resource != test.resource {
				t.Fatalf("%s resource = %#v", test.name, resourceErr)
			}
		}
	}
}
