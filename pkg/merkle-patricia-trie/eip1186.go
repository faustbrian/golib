package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// Account is a canonically decoded Ethereum state-trie account established by
// a state-trie lookup or proof under a supplied root.
type Account struct {
	nonce       uint64
	balance     [RootBytes]byte
	storageRoot Root
	codeHash    [RootBytes]byte
	verified    bool
}

// StorageProofClaim is one immutable EIP-1186 storage membership or absence
// claim. Construct claims with StorageMembershipClaim or StorageAbsenceClaim.
type StorageProofClaim struct {
	slot          [RootBytes]byte
	expectedValue []byte
	proof         Proof
	present       bool
	valid         bool
}

// StorageMembershipClaim constructs an exact non-zero storage-value claim.
// expectedValue is copied and must be a minimal unsigned big-endian integer.
func StorageMembershipClaim(
	slot [RootBytes]byte,
	expectedValue []byte,
	proof Proof,
) StorageProofClaim {
	return StorageProofClaim{
		slot:          slot,
		expectedValue: append([]byte(nil), expectedValue...),
		proof:         proof,
		present:       true,
		valid:         true,
	}
}

// StorageAbsenceClaim constructs an exact absent-slot claim.
func StorageAbsenceClaim(
	slot [RootBytes]byte,
	proof Proof,
) StorageProofClaim {
	return StorageProofClaim{slot: slot, proof: proof, valid: true}
}

// Nonce returns the account's unsigned 64-bit transaction nonce.
func (account Account) Nonce() uint64 {
	return account.nonce
}

// Balance returns the account's unsigned 256-bit balance as a big-endian word.
func (account Account) Balance() [RootBytes]byte {
	return account.balance
}

// StorageRoot returns the proven account's storage-trie root.
func (account Account) StorageRoot() Root {
	return account.storageRoot
}

// CodeHash returns the proven account's 32-byte code hash.
func (account Account) CodeHash() [RootBytes]byte {
	return account.codeHash
}

// VerifyAccountProof verifies accountEncoding under stateRoot at the secure
// Keccak-256 path of address, then returns its canonical account fields.
func VerifyAccountProof(
	ctx context.Context,
	stateRoot Root,
	address [20]byte,
	accountEncoding []byte,
	proof Proof,
	limits Limits,
) (Account, error) {
	if err := VerifySecureMembership(
		ctx,
		stateRoot,
		address[:],
		accountEncoding,
		proof,
		limits,
	); err != nil {
		return Account{}, err
	}
	return decodeAccount(accountEncoding, limits)
}

// VerifyAccountAbsence verifies that address is absent under stateRoot.
func VerifyAccountAbsence(
	ctx context.Context,
	stateRoot Root,
	address [20]byte,
	proof Proof,
	limits Limits,
) error {
	return VerifySecureAbsence(ctx, stateRoot, address[:], proof, limits)
}

// VerifyStorageProof verifies one canonical 32-byte slot key against the
// storage root of a verified account. expectedValue is a minimal unsigned
// big-endian integer; an empty value verifies slot absence.
func VerifyStorageProof(
	ctx context.Context,
	account Account,
	slot [RootBytes]byte,
	expectedValue []byte,
	proof Proof,
	limits Limits,
) error {
	if !account.verified {
		return ErrInvalidAccount
	}
	if len(expectedValue) > RootBytes ||
		(len(expectedValue) != 0 && expectedValue[0] == 0) {
		return ErrInvalidStorageValue
	}
	if len(expectedValue) == 0 {
		return VerifySecureAbsence(
			ctx,
			account.storageRoot,
			slot[:],
			proof,
			limits,
		)
	}
	encoded := encodeStorageInteger(expectedValue)
	return VerifySecureMembership(
		ctx,
		account.storageRoot,
		slot[:],
		encoded,
		proof,
		limits,
	)
}

// VerifyStorageProofs verifies one or more independent EIP-1186 slot proofs
// against a verified account. It validates the complete claim set before
// traversal, rejects duplicate or conflicting slots, and applies proof count
// and byte limits to the aggregate transport payload.
func VerifyStorageProofs(
	ctx context.Context,
	account Account,
	claims []StorageProofClaim,
	limits Limits,
) error {
	if err := validateTrieLimits(limits); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if !account.verified {
		return ErrInvalidAccount
	}
	if len(claims) == 0 || len(claims) > limits.MaxProofKeys {
		return fmt.Errorf("%w: invalid storage proof count", ErrInvalidProofClaim)
	}

	seen := make(map[[RootBytes]byte]struct{}, len(claims))
	totalNodes := 0
	totalBytes := 0
	hashOperations := 0
	for _, claim := range claims {
		if !claim.valid {
			return ErrInvalidProofClaim
		}
		if _, duplicate := seen[claim.slot]; duplicate {
			return ErrDuplicateProofKey
		}
		seen[claim.slot] = struct{}{}
		if claim.present &&
			(len(claim.expectedValue) == 0 ||
				len(claim.expectedValue) > RootBytes ||
				claim.expectedValue[0] == 0) {
			return ErrInvalidStorageValue
		}
		if len(claim.proof.nodes) > limits.MaxProofNodes-totalNodes {
			return fmt.Errorf("%w: proof node bound exceeded", ErrResourceLimit)
		}
		totalNodes += len(claim.proof.nodes)
		if len(claim.proof.nodes)+1 >
			limits.MaxHashOperations-hashOperations {
			return fmt.Errorf("%w: hash operation bound exceeded", ErrResourceLimit)
		}
		hashOperations += len(claim.proof.nodes) + 1
		for _, encoded := range claim.proof.nodes {
			if len(encoded) > limits.MaxProofBytes-totalBytes {
				return fmt.Errorf("%w: proof byte bound exceeded", ErrResourceLimit)
			}
			totalBytes += len(encoded)
		}
	}

	for _, claim := range claims {
		expectedValue := claim.expectedValue
		if !claim.present {
			expectedValue = nil
		}
		if err := VerifyStorageProof(
			ctx,
			account,
			claim.slot,
			expectedValue,
			claim.proof,
			limits,
		); err != nil {
			return err
		}
	}
	return nil
}

func encodeStorageInteger(value []byte) []byte {
	if len(value) == 1 && value[0] < 0x80 {
		return []byte{value[0]}
	}
	encoded := make([]byte, len(value)+1)
	encoded[0] = 0x80 + byte(len(value))
	copy(encoded[1:], value)
	return encoded
}

func decodeAccount(encoded []byte, limits Limits) (Account, error) {
	if err := validateTrieLimits(limits); err != nil {
		return Account{}, err
	}
	decoded, err := rlp.Decode(
		encoded,
		rlp.Limits{
			MaxEncodedBytes: limits.MaxValueBytes,
			MaxDepth:        4,
			MaxItems:        5,
		},
	)
	if err != nil {
		return Account{}, fmt.Errorf("%w: malformed account RLP", ErrInvalidAccount)
	}
	if decoded.Kind() != rlp.KindList {
		return Account{}, fmt.Errorf("%w: malformed account RLP", ErrInvalidAccount)
	}
	fields := decoded.Elements()
	if len(fields) != 4 {
		return Account{}, fmt.Errorf("%w: account field count", ErrInvalidAccount)
	}
	for _, field := range fields {
		if field.Kind() != rlp.KindString {
			return Account{}, fmt.Errorf("%w: account field is not bytes", ErrInvalidAccount)
		}
	}
	nonceBytes := fields[0].Bytes()
	balanceBytes := fields[1].Bytes()
	if !canonicalUnsigned(nonceBytes, 8) ||
		!canonicalUnsigned(balanceBytes, RootBytes) {
		return Account{}, fmt.Errorf("%w: non-canonical account integer", ErrInvalidAccount)
	}
	var nonce uint64
	for _, octet := range nonceBytes {
		nonce = nonce<<8 | uint64(octet)
	}
	var balance [RootBytes]byte
	copy(balance[RootBytes-len(balanceBytes):], balanceBytes)
	storageRoot, err := RootFromBytes(fields[2].Bytes())
	if err != nil {
		return Account{}, fmt.Errorf("%w: storage root length", ErrInvalidAccount)
	}
	codeHashBytes := fields[3].Bytes()
	if len(codeHashBytes) != RootBytes {
		return Account{}, fmt.Errorf("%w: code hash length", ErrInvalidAccount)
	}
	var codeHash [RootBytes]byte
	copy(codeHash[:], codeHashBytes)
	return Account{
		nonce:       nonce,
		balance:     balance,
		storageRoot: storageRoot,
		codeHash:    codeHash,
		verified:    true,
	}, nil
}

func canonicalUint256(value []byte) bool {
	return canonicalUnsigned(value, RootBytes)
}

func canonicalUnsigned(value []byte, maximum int) bool {
	return len(value) <= maximum && (len(value) == 0 || value[0] != 0)
}
