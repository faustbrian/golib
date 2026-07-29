package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestEIP1186AccountAndStorageProofs(t *testing.T) {
	t.Parallel()

	var slot [32]byte
	slot[31] = 7
	storageValue := []byte{0x2a}
	encodedStorageValue := mustRLPString(t, storageValue)
	storageTrie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie(storage) error = %v", err)
	}
	storageTrie, err = storageTrie.Update(
		context.Background(), slot[:], encodedStorageValue,
	)
	if err != nil {
		t.Fatalf("storage Update() error = %v", err)
	}
	storageRoot := mustSecureRoot(t, storageTrie)

	var address [20]byte
	address[19] = 0xaa
	var codeHash [32]byte
	codeHash[0] = 0xcc
	accountEncoding := mustAccountRLP(
		t, []byte{1}, []byte{2}, storageRoot, codeHash,
	)
	stateTrie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie(state) error = %v", err)
	}
	stateTrie, err = stateTrie.Update(
		context.Background(), address[:], accountEncoding,
	)
	if err != nil {
		t.Fatalf("state Update() error = %v", err)
	}
	stateRoot := mustSecureRoot(t, stateTrie)
	accountProof, err := stateTrie.Prove(context.Background(), address[:])
	if err != nil {
		t.Fatalf("account Prove() error = %v", err)
	}

	account, err := mpt.VerifyAccountProof(
		context.Background(),
		stateRoot,
		address,
		accountEncoding,
		accountProof,
		mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof() error = %v", err)
	}
	if !slices.Equal(account.Nonce(), []byte{1}) ||
		!slices.Equal(account.Balance(), []byte{2}) ||
		account.StorageRoot() != storageRoot ||
		account.CodeHash() != codeHash {
		t.Fatalf("decoded account = %#v", account)
	}
	nonce := account.Nonce()
	nonce[0] = 9
	if slices.Equal(nonce, account.Nonce()) {
		t.Fatal("Account.Nonce() returned aliased bytes")
	}

	storageProof, err := storageTrie.Prove(context.Background(), slot[:])
	if err != nil {
		t.Fatalf("storage Prove() error = %v", err)
	}
	if err := mpt.VerifyStorageProof(
		context.Background(),
		account,
		slot,
		storageValue,
		storageProof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyStorageProof() error = %v", err)
	}
}

func TestEIP1186AbsentAccountAndStorageSlot(t *testing.T) {
	t.Parallel()

	stateTrie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie(state) error = %v", err)
	}
	var address [20]byte
	accountProof, err := stateTrie.Prove(context.Background(), address[:])
	if err != nil {
		t.Fatalf("account Prove() error = %v", err)
	}
	if err := mpt.VerifyAccountAbsence(
		context.Background(),
		mustSecureRoot(t, stateTrie),
		address,
		accountProof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyAccountAbsence() error = %v", err)
	}

	var codeHash [32]byte
	accountEncoding := mustAccountRLP(
		t, nil, nil, mpt.EmptyRoot(), codeHash,
	)
	populatedState, err := stateTrie.Update(
		context.Background(), address[:], accountEncoding,
	)
	if err != nil {
		t.Fatalf("state Update() error = %v", err)
	}
	proof, err := populatedState.Prove(context.Background(), address[:])
	if err != nil {
		t.Fatalf("account Prove() error = %v", err)
	}
	account, err := mpt.VerifyAccountProof(
		context.Background(),
		mustSecureRoot(t, populatedState),
		address,
		accountEncoding,
		proof,
		mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof() error = %v", err)
	}
	storageTrie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie(storage) error = %v", err)
	}
	var slot [32]byte
	storageProof, err := storageTrie.Prove(context.Background(), slot[:])
	if err != nil {
		t.Fatalf("storage Prove() error = %v", err)
	}
	if err := mpt.VerifyStorageProof(
		context.Background(),
		account,
		slot,
		nil,
		storageProof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyStorageProof(absence) error = %v", err)
	}
}

func TestEIP1186RejectsMalformedAccountAndStorageClaims(t *testing.T) {
	t.Parallel()

	var address [20]byte
	stateTrie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	stateTrie, err = stateTrie.Update(
		context.Background(), address[:], []byte{0x80},
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	proof, err := stateTrie.Prove(context.Background(), address[:])
	if err != nil {
		t.Fatalf("Prove() error = %v", err)
	}
	if _, err := mpt.VerifyAccountProof(
		context.Background(),
		mustSecureRoot(t, stateTrie),
		address,
		[]byte{0x80},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidAccount) {
		t.Fatalf("VerifyAccountProof(malformed) error = %v", err)
	}

	var account mpt.Account
	var slot [32]byte
	if err := mpt.VerifyStorageProof(
		context.Background(),
		account,
		slot,
		[]byte{0, 1},
		mpt.Proof{},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidAccount) {
		t.Fatalf("VerifyStorageProof(zero account) error = %v", err)
	}

	validAccountEncoding := mustAccountRLP(
		t, nil, nil, mpt.EmptyRoot(), [32]byte{},
	)
	validState, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie(valid state) error = %v", err)
	}
	validState, err = validState.Update(
		context.Background(), address[:], validAccountEncoding,
	)
	if err != nil {
		t.Fatalf("valid state Update() error = %v", err)
	}
	validProof, err := validState.Prove(context.Background(), address[:])
	if err != nil {
		t.Fatalf("valid account Prove() error = %v", err)
	}
	validAccount, err := mpt.VerifyAccountProof(
		context.Background(),
		mustSecureRoot(t, validState),
		address,
		validAccountEncoding,
		validProof,
		mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("valid VerifyAccountProof() error = %v", err)
	}
	if err := mpt.VerifyStorageProof(
		context.Background(),
		validAccount,
		slot,
		[]byte{0, 1},
		mpt.Proof{},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidStorageValue) {
		t.Fatalf("VerifyStorageProof(non-canonical value) error = %v", err)
	}
}

func mustAccountRLP(
	t *testing.T,
	nonce, balance []byte,
	storageRoot mpt.Root,
	codeHash [32]byte,
) []byte {
	t.Helper()
	encoded, err := rlp.Encode(
		rlp.List(
			rlp.String(nonce),
			rlp.String(balance),
			rlp.String(storageRoot[:]),
			rlp.String(codeHash[:]),
		),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode account RLP: %v", err)
	}
	return encoded
}

func mustRLPString(t *testing.T, value []byte) []byte {
	t.Helper()
	encoded, err := rlp.Encode(rlp.String(value), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("encode RLP string: %v", err)
	}
	return encoded
}
