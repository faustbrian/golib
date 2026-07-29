package mpt

import (
	"context"
	"fmt"
	"slices"
)

// Proof is an immutable ordered sequence of canonical RLP nodes. The root node
// is first; embedded children are carried by their parent and are not repeated.
type Proof struct {
	nodes [][]byte
}

// ProofFromNodes copies transport-decoded proof nodes after enforcing count and
// byte limits. Canonicality and path binding are checked during verification.
func ProofFromNodes(nodes [][]byte, limits Limits) (Proof, error) {
	if err := validateTrieLimits(limits); err != nil {
		return Proof{}, err
	}
	if len(nodes) > limits.MaxProofNodes {
		return Proof{}, fmt.Errorf("%w: proof node bound exceeded", ErrResourceLimit)
	}
	total := 0
	copied := make([][]byte, len(nodes))
	for index, encoded := range nodes {
		if len(encoded) > limits.MaxProofBytes-total {
			return Proof{}, fmt.Errorf("%w: proof byte bound exceeded", ErrResourceLimit)
		}
		total += len(encoded)
		copied[index] = append([]byte(nil), encoded...)
	}
	return Proof{nodes: copied}, nil
}

// Nodes returns owned copies of the ordered encoded proof nodes.
func (proof Proof) Nodes() [][]byte {
	nodes := make([][]byte, len(proof.nodes))
	for index, encoded := range proof.nodes {
		nodes[index] = append([]byte(nil), encoded...)
	}
	return nodes
}

// Prove returns the Ethereum-compatible ordered proof path for a raw key.
func (trie RawTrie) Prove(ctx context.Context, key []byte) (Proof, error) {
	return proveSnapshot(ctx, trie.snapshot, key, false)
}

// Prove returns the Ethereum-compatible ordered proof path for a secure key.
func (trie SecureTrie) Prove(ctx context.Context, key []byte) (Proof, error) {
	return proveSnapshot(ctx, trie.snapshot, key, true)
}

func proveSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	key []byte,
	secure bool,
) (Proof, error) {
	if err := validateOperation(ctx, snapshot, key, nil, false); err != nil {
		return Proof{}, err
	}
	if snapshot.root == nil {
		return Proof{}, nil
	}
	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	path, err := keyPath(key, secure, &budget)
	if err != nil {
		return Proof{}, err
	}
	state := traversalState{
		ctx:       ctx,
		maxDepth:  snapshot.limits.MaxTraversalDepth,
		nodesLeft: snapshot.limits.MaxTraversalNodes,
		readsLeft: snapshot.limits.MaxNodeReads,
		reader:    snapshot.reader,
		pending:   snapshot.pending,
		budget:    &budget,
	}
	current := snapshot.root
	depth := 0
	rootNode := true
	nodes := make([][]byte, 0)
	total := 0

	for current != nil {
		if err := state.visit(depth); err != nil {
			return Proof{}, err
		}
		if hashed, ok := current.(hashNode); ok {
			current, err = state.resolve(Root(hashed))
			if err != nil {
				return Proof{}, err
			}
			continue
		}
		encoded, _, encodeErr := encodeNodeBounded(
			ctx,
			current,
			snapshot.limits.MaxEncodingNodes,
			&budget,
		)
		if encodeErr != nil {
			return Proof{}, encodeErr
		}
		if rootNode || len(encoded) >= RootBytes {
			if len(nodes) == snapshot.limits.MaxProofNodes {
				return Proof{}, fmt.Errorf("%w: proof node bound exceeded", ErrResourceLimit)
			}
			if len(encoded) > snapshot.limits.MaxProofBytes-total {
				return Proof{}, fmt.Errorf("%w: proof byte bound exceeded", ErrResourceLimit)
			}
			total += len(encoded)
			nodes = append(nodes, append([]byte(nil), encoded...))
		}
		rootNode = false

		switch typed := current.(type) {
		case *leafNode:
			return Proof{nodes: nodes}, nil
		case *extensionNode:
			if !hasPrefix(path, typed.path) {
				return Proof{nodes: nodes}, nil
			}
			path = path[len(typed.path):]
			current = typed.child
			depth++
		case *branchNode:
			if len(path) == 0 {
				return Proof{nodes: nodes}, nil
			}
			current = typed.children[path[0]]
			path = path[1:]
			depth++
		}
	}
	return Proof{nodes: nodes}, nil
}

// VerifyRawMembership verifies an exact raw-key value claim under root.
func VerifyRawMembership(
	ctx context.Context,
	root Root,
	key, expectedValue []byte,
	proof Proof,
	limits Limits,
) error {
	return verifyClaim(ctx, root, key, expectedValue, true, false, proof, limits)
}

// VerifyRawAbsence verifies an exact raw-key absence claim under root.
func VerifyRawAbsence(
	ctx context.Context,
	root Root,
	key []byte,
	proof Proof,
	limits Limits,
) error {
	return verifyClaim(ctx, root, key, nil, false, false, proof, limits)
}

// VerifySecureMembership verifies an exact secure-key value claim under root.
func VerifySecureMembership(
	ctx context.Context,
	root Root,
	key, expectedValue []byte,
	proof Proof,
	limits Limits,
) error {
	return verifyClaim(ctx, root, key, expectedValue, true, true, proof, limits)
}

// VerifySecureAbsence verifies an exact secure-key absence claim under root.
func VerifySecureAbsence(
	ctx context.Context,
	root Root,
	key []byte,
	proof Proof,
	limits Limits,
) error {
	return verifyClaim(ctx, root, key, nil, false, true, proof, limits)
}

func verifyClaim(
	ctx context.Context,
	root Root,
	key, expectedValue []byte,
	wantPresent bool,
	secure bool,
	proof Proof,
	limits Limits,
) error {
	if err := validateTrieLimits(limits); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(key) > limits.MaxKeyBytes {
		return fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
	}
	if wantPresent &&
		(len(expectedValue) == 0 || len(expectedValue) > limits.MaxValueBytes) {
		return fmt.Errorf("%w: invalid expected value", ErrInvalidValue)
	}
	if err := validateProofLimits(proof, limits); err != nil {
		return err
	}
	if root == EmptyRoot() {
		if len(proof.nodes) != 0 {
			return fmt.Errorf("%w: surplus nodes for empty root", ErrMalformedProof)
		}
		if wantPresent {
			return ErrFailedProof
		}
		return nil
	}
	if len(proof.nodes) == 0 {
		return ErrIncompleteProof
	}

	budget := workBudget{hashesLeft: limits.MaxHashOperations}
	path, _ := keyPath(key, secure, &budget)
	index := 0
	current, err := proofNode(proof, &index, root, true, &budget)
	if err != nil {
		return err
	}
	depth := 0
	nodesLeft := limits.MaxTraversalNodes

	for {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if depth > limits.MaxTraversalDepth || nodesLeft == 0 {
			return fmt.Errorf("%w: proof traversal bound exceeded", ErrResourceLimit)
		}
		nodesLeft--
		if hashed, ok := current.(hashNode); ok {
			current, err = proofNode(
				proof, &index, Root(hashed), false, &budget,
			)
			if err != nil {
				return err
			}
			continue
		}

		switch typed := current.(type) {
		case *leafNode:
			present := slices.Equal(typed.path, path)
			return finishProofClaim(
				proof, index, present, typed.value, expectedValue, wantPresent,
			)
		case *extensionNode:
			if !hasPrefix(path, typed.path) {
				return finishProofClaim(
					proof, index, false, nil, expectedValue, wantPresent,
				)
			}
			path = path[len(typed.path):]
			if hashed, ok := typed.child.(hashNode); ok {
				childHash := Root(hashed)
				child, childErr := proofNode(
					proof, &index, childHash, false, &budget,
				)
				if childErr != nil {
					return childErr
				}
				if _, ok := child.(*branchNode); !ok {
					return fmt.Errorf(
						"%w: extension child is not a branch",
						ErrMalformedProof,
					)
				}
				current = child
			} else {
				current = typed.child
			}
			depth++
		case *branchNode:
			if len(path) == 0 {
				return finishProofClaim(
					proof,
					index,
					len(typed.value) != 0,
					typed.value,
					expectedValue,
					wantPresent,
				)
			}
			child := typed.children[path[0]]
			path = path[1:]
			if child == nil {
				return finishProofClaim(
					proof, index, false, nil, expectedValue, wantPresent,
				)
			}
			current = child
			depth++
		}
	}
}

func proofNode(
	proof Proof,
	index *int,
	expected Root,
	root bool,
	budget *workBudget,
) (node, error) {
	if *index >= len(proof.nodes) {
		return nil, ErrIncompleteProof
	}
	encoded := proof.nodes[*index]
	actual, err := budget.hash(encoded)
	if err != nil {
		return nil, err
	}
	if actual != expected {
		if root {
			return nil, ErrWrongRoot
		}
		return nil, fmt.Errorf("%w: node hash mismatch", ErrMalformedProof)
	}
	decoded, err := decodeNode(encoded)
	if err != nil || decoded == nil {
		return nil, fmt.Errorf("%w: invalid canonical node", ErrMalformedProof)
	}
	*index++
	return decoded, nil
}

func finishProofClaim(
	proof Proof,
	consumed int,
	present bool,
	value, expectedValue []byte,
	wantPresent bool,
) error {
	if consumed != len(proof.nodes) {
		return fmt.Errorf("%w: surplus proof nodes", ErrMalformedProof)
	}
	if present != wantPresent {
		return ErrFailedProof
	}
	if present && !slices.Equal(value, expectedValue) {
		return ErrFailedProof
	}
	return nil
}

func validateProofLimits(proof Proof, limits Limits) error {
	if len(proof.nodes) > limits.MaxProofNodes {
		return fmt.Errorf("%w: proof node bound exceeded", ErrResourceLimit)
	}
	total := 0
	for _, encoded := range proof.nodes {
		if len(encoded) > limits.MaxProofBytes-total {
			return fmt.Errorf("%w: proof byte bound exceeded", ErrResourceLimit)
		}
		total += len(encoded)
	}
	return nil
}
