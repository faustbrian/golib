package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// Account is a canonically decoded Ethereum state-trie account proven under a
// supplied state root. Its integer fields use minimal unsigned big-endian
// bytes; empty bytes represent zero.
type Account struct {
	nonce       []byte
	balance     []byte
	storageRoot Root
	codeHash    [RootBytes]byte
	verified    bool
}

// Nonce returns an owned minimal unsigned big-endian nonce.
func (account Account) Nonce() []byte {
	return append([]byte(nil), account.nonce...)
}

// Balance returns an owned minimal unsigned big-endian balance.
func (account Account) Balance() []byte {
	return append([]byte(nil), account.balance...)
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
	nonce := fields[0].Bytes()
	balance := fields[1].Bytes()
	if !canonicalUint256(nonce) || !canonicalUint256(balance) {
		return Account{}, fmt.Errorf("%w: non-canonical account integer", ErrInvalidAccount)
	}
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
	return len(value) <= RootBytes && (len(value) == 0 || value[0] != 0)
}
