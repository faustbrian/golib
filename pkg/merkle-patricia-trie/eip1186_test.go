package mpt_test

import (
	"context"
	"errors"
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
	var wantBalance [32]byte
	wantBalance[31] = 2
	if account.Nonce() != 1 ||
		account.Balance() != wantBalance ||
		account.StorageRoot() != storageRoot ||
		account.CodeHash() != codeHash {
		t.Fatalf("decoded account = %#v", account)
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

func TestEIP1186StorageProofSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := mpt.DefaultLimits()
	storageTrie, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie(storage) error = %v", err)
	}
	var firstSlot [32]byte
	firstSlot[31] = 1
	var secondSlot [32]byte
	secondSlot[31] = 2
	var absentSlot [32]byte
	absentSlot[31] = 3
	firstValue := []byte{0x2a}
	secondValue := []byte{0x01, 0x80}
	for slot, value := range map[[32]byte][]byte{
		firstSlot: firstValue, secondSlot: secondValue,
	} {
		storageTrie, err = storageTrie.Update(
			ctx,
			slot[:],
			mustRLPString(t, value),
		)
		if err != nil {
			t.Fatalf("storage Update() error = %v", err)
		}
	}

	var address [20]byte
	storageRoot := mustSecureRoot(t, storageTrie)
	accountEncoding := mustAccountRLP(
		t, nil, nil, storageRoot, [32]byte{},
	)
	stateTrie, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie(state) error = %v", err)
	}
	stateTrie, err = stateTrie.Update(ctx, address[:], accountEncoding)
	if err != nil {
		t.Fatalf("state Update() error = %v", err)
	}
	accountProof, err := stateTrie.Prove(ctx, address[:])
	if err != nil {
		t.Fatalf("state Prove() error = %v", err)
	}
	account, err := mpt.VerifyAccountProof(
		ctx,
		mustSecureRoot(t, stateTrie),
		address,
		accountEncoding,
		accountProof,
		limits,
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof() error = %v", err)
	}

	firstProof, err := storageTrie.Prove(ctx, firstSlot[:])
	if err != nil {
		t.Fatalf("Prove(first slot) error = %v", err)
	}
	secondProof, err := storageTrie.Prove(ctx, secondSlot[:])
	if err != nil {
		t.Fatalf("Prove(second slot) error = %v", err)
	}
	absentProof, err := storageTrie.Prove(ctx, absentSlot[:])
	if err != nil {
		t.Fatalf("Prove(absent slot) error = %v", err)
	}

	firstClaim := mpt.StorageMembershipClaim(
		firstSlot,
		firstValue,
		firstProof,
	)
	firstValue[0] = 0xff
	claims := []mpt.StorageProofClaim{
		firstClaim,
		mpt.StorageMembershipClaim(secondSlot, secondValue, secondProof),
		mpt.StorageAbsenceClaim(absentSlot, absentProof),
	}
	if err := mpt.VerifyStorageProofs(
		ctx,
		account,
		claims,
		limits,
	); err != nil {
		t.Fatalf("VerifyStorageProofs() error = %v", err)
	}

	duplicate := append(
		append([]mpt.StorageProofClaim(nil), claims...),
		mpt.StorageAbsenceClaim(firstSlot, firstProof),
	)
	if err := mpt.VerifyStorageProofs(
		ctx,
		account,
		duplicate,
		limits,
	); !errors.Is(err, mpt.ErrDuplicateProofKey) {
		t.Fatalf("VerifyStorageProofs(duplicate) error = %v", err)
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
