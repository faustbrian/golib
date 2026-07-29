package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestAccountValueAndStateTrieUseCanonicalEthereumEncoding(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	var balance [32]byte
	balance[30] = 0x01
	balance[31] = 0x80
	storageRoot := mpt.EmptyRoot()
	var codeHash [32]byte
	codeHash[0] = 0xcc

	value, err := mpt.NewAccountValue(0x0102, balance, storageRoot, codeHash, limits)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	wantEncoding, err := rlp.Encode(rlp.List(
		rlp.String([]byte{0x01, 0x02}),
		rlp.String([]byte{0x01, 0x80}),
		rlp.String(storageRoot[:]),
		rlp.String(codeHash[:]),
	), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("RLP Encode() error = %v", err)
	}
	if got := value.Bytes(); !slices.Equal(got, wantEncoding) {
		t.Fatalf("account encoding = %x, want %x", got, wantEncoding)
	}
	owned := value.Bytes()
	owned[0] ^= 0xff
	if slices.Equal(owned, value.Bytes()) {
		t.Fatal("AccountValue.Bytes() returned aliased bytes")
	}

	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	var address [20]byte
	address[19] = 0xaa
	updated, err := state.UpdateAccount(context.Background(), address, value)
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if has, err := updated.HasAccount(context.Background(), address); err != nil || !has {
		t.Fatalf("HasAccount() = (%t, %v)", has, err)
	}
	account, err := updated.GetAccount(context.Background(), address)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if account.Nonce() != 0x0102 || account.Balance() != balance ||
		account.StorageRoot() != storageRoot || account.CodeHash() != codeHash {
		t.Fatalf("GetAccount() = %#v", account)
	}

	generic, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	generic, err = generic.Update(context.Background(), address[:], wantEncoding)
	if err != nil {
		t.Fatalf("SecureTrie.Update() error = %v", err)
	}
	stateRoot, err := updated.Root()
	if err != nil {
		t.Fatalf("StateTrie.Root() error = %v", err)
	}
	genericRoot, err := generic.Root()
	if err != nil {
		t.Fatalf("SecureTrie.Root() error = %v", err)
	}
	if stateRoot != genericRoot {
		t.Fatalf("state root = %x, generic secure root = %x", stateRoot, genericRoot)
	}
	if root, err := state.Root(); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("old StateTrie.Root() = (%x, %v)", root, err)
	}

	proof, err := updated.ProveAccount(context.Background(), address)
	if err != nil {
		t.Fatalf("ProveAccount() error = %v", err)
	}
	proven, err := mpt.VerifyAccountProof(
		context.Background(), stateRoot, address, value.Bytes(), proof, limits,
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof() error = %v", err)
	}
	if proven.Nonce() != account.Nonce() || proven.Balance() != account.Balance() {
		t.Fatalf("proven account = %#v, loaded account = %#v", proven, account)
	}

	deleted, err := updated.DeleteAccount(context.Background(), address)
	if err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if root, err := deleted.Root(); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("deleted StateTrie.Root() = (%x, %v)", root, err)
	}
}

func TestStateTrieDoesNotApplyEmptyAccountLifecycleRules(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	value, err := mpt.NewAccountValue(
		0, [32]byte{}, mpt.EmptyRoot(), mpt.EmptyCodeHash(), limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue(empty account) error = %v", err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	var address [20]byte
	state, err = state.UpdateAccount(context.Background(), address, value)
	if err != nil {
		t.Fatalf("UpdateAccount(empty account) error = %v", err)
	}
	if root, err := state.Root(); err != nil || root == mpt.EmptyRoot() {
		t.Fatalf("empty account was silently deleted: root = %x, error = %v", root, err)
	}
}

func TestStorageTrieCanonicalizesWordsAndDeletesZero(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	storage, err := mpt.NewStorageTrie(limits)
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	var slot [32]byte
	slot[31] = 7
	var word [32]byte
	word[31] = 0x7f

	updated, err := storage.UpdateSlot(context.Background(), slot, word)
	if err != nil {
		t.Fatalf("UpdateSlot(0x7f) error = %v", err)
	}
	if has, err := updated.HasSlot(context.Background(), slot); err != nil || !has {
		t.Fatalf("HasSlot() = (%t, %v)", has, err)
	}
	if got, err := updated.GetSlot(context.Background(), slot); err != nil || got != word {
		t.Fatalf("GetSlot() = (%x, %v), want %x", got, err, word)
	}

	generic, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	generic, err = generic.Update(context.Background(), slot[:], []byte{0x7f})
	if err != nil {
		t.Fatalf("SecureTrie.Update() error = %v", err)
	}
	storageRoot, err := updated.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root() error = %v", err)
	}
	genericRoot, err := generic.Root()
	if err != nil {
		t.Fatalf("SecureTrie.Root() error = %v", err)
	}
	if storageRoot != genericRoot {
		t.Fatalf("storage root = %x, generic secure root = %x", storageRoot, genericRoot)
	}

	word[31] = 0x80
	updated, err = updated.UpdateSlot(context.Background(), slot, word)
	if err != nil {
		t.Fatalf("UpdateSlot(0x80) error = %v", err)
	}
	generic, err = generic.Update(context.Background(), slot[:], []byte{0x81, 0x80})
	if err != nil {
		t.Fatalf("SecureTrie.Update(0x80) error = %v", err)
	}
	storageRoot, err = updated.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root(0x80) error = %v", err)
	}
	genericRoot, err = generic.Root()
	if err != nil {
		t.Fatalf("SecureTrie.Root(0x80) error = %v", err)
	}
	if storageRoot != genericRoot {
		t.Fatalf("0x80 storage root = %x, generic = %x", storageRoot, genericRoot)
	}

	proof, err := updated.ProveSlot(context.Background(), slot)
	if err != nil {
		t.Fatalf("ProveSlot() error = %v", err)
	}
	accountValue, err := mpt.NewAccountValue(
		0, [32]byte{}, storageRoot, mpt.EmptyCodeHash(), limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	var address [20]byte
	state, err = state.UpdateAccount(context.Background(), address, accountValue)
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	account, err := state.GetAccount(context.Background(), address)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if err := mpt.VerifyStorageProof(
		context.Background(), account, slot, []byte{0x80}, proof, limits,
	); err != nil {
		t.Fatalf("VerifyStorageProof() error = %v", err)
	}

	cleared, err := updated.UpdateSlot(context.Background(), slot, [32]byte{})
	if err != nil {
		t.Fatalf("UpdateSlot(zero) error = %v", err)
	}
	if _, err := cleared.GetSlot(context.Background(), slot); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("GetSlot(after zero) error = %v", err)
	}
	if root, err := cleared.Root(); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("cleared StorageTrie.Root() = (%x, %v)", root, err)
	}
}

func TestStateAndStorageProfilesRejectInvalidUse(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	invalidLimits := limits
	invalidLimits.MaxNodeReads = 0
	if _, err := mpt.NewAccountValue(
		0, [32]byte{}, mpt.EmptyRoot(), mpt.EmptyCodeHash(), invalidLimits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewAccountValue(invalid limits) error = %v", err)
	}
	limits.MaxValueBytes = 1
	if _, err := mpt.NewAccountValue(
		0, [32]byte{}, mpt.EmptyRoot(), mpt.EmptyCodeHash(), limits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewAccountValue(over limit) error = %v", err)
	}

	var state mpt.StateTrie
	if _, err := state.Root(); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.Root() error = %v", err)
	}
	var address [20]byte
	if _, err := state.GetAccount(context.Background(), address); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.GetAccount() error = %v", err)
	}
	if has, err := state.HasAccount(context.Background(), address); !errors.Is(err, mpt.ErrUninitialized) || has {
		t.Fatalf("zero StateTrie.HasAccount() = (%t, %v)", has, err)
	}
	if _, err := state.UpdateAccount(
		context.Background(), address, mpt.EncodedAccountValue{},
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.UpdateAccount() error = %v", err)
	}
	if _, err := state.DeleteAccount(context.Background(), address); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.DeleteAccount() error = %v", err)
	}
	if _, err := state.ProveAccount(context.Background(), address); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.ProveAccount() error = %v", err)
	}
	if _, err := state.Commit(context.Background(), memory.New()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.Commit() error = %v", err)
	}
	if _, err := state.Rebuild(context.Background()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.Rebuild() error = %v", err)
	}
	if _, err := state.RecoverNode(
		context.Background(), mpt.Root{}, nil,
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StateTrie.RecoverNode() error = %v", err)
	}
	if _, err := mpt.NewStateTrie(invalidLimits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewStateTrie(invalid limits) error = %v", err)
	}
	if _, err := mpt.LoadStateTrie(mpt.EmptyRoot(), nil, mpt.DefaultLimits()); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("LoadStateTrie(nil store) error = %v", err)
	}
	emptyState, err := mpt.NewStateTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	if has, err := emptyState.HasAccount(context.Background(), address); err != nil || has {
		t.Fatalf("empty StateTrie.HasAccount() = (%t, %v)", has, err)
	}
	if _, err := emptyState.UpdateAccount(
		context.Background(), address, mpt.EncodedAccountValue{},
	); !errors.Is(err, mpt.ErrInvalidAccount) {
		t.Fatalf("StateTrie.UpdateAccount(zero value) error = %v", err)
	}
	validValue, err := mpt.NewAccountValue(
		0, [32]byte{}, mpt.EmptyRoot(), mpt.EmptyCodeHash(), mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	var nilContext context.Context
	if _, err := emptyState.UpdateAccount(nilContext, address, validValue); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("StateTrie.UpdateAccount(nil context) error = %v", err)
	}
	var storage mpt.StorageTrie
	if _, err := storage.Root(); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.Root() error = %v", err)
	}
	var slot [32]byte
	if _, err := storage.GetSlot(context.Background(), slot); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.GetSlot() error = %v", err)
	}
	if has, err := storage.HasSlot(context.Background(), slot); !errors.Is(err, mpt.ErrUninitialized) || has {
		t.Fatalf("zero StorageTrie.HasSlot() = (%t, %v)", has, err)
	}
	if _, err := storage.UpdateSlot(
		context.Background(), slot, [32]byte{31: 1},
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.UpdateSlot() error = %v", err)
	}
	if _, err := storage.DeleteSlot(context.Background(), slot); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.DeleteSlot() error = %v", err)
	}
	if _, err := storage.ProveSlot(context.Background(), slot); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.ProveSlot() error = %v", err)
	}
	if _, err := storage.Commit(context.Background(), memory.New()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.Commit() error = %v", err)
	}
	if _, err := storage.Rebuild(context.Background()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.Rebuild() error = %v", err)
	}
	if _, err := storage.RecoverNode(
		context.Background(), mpt.Root{}, nil,
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero StorageTrie.RecoverNode() error = %v", err)
	}
	if _, err := mpt.NewStorageTrie(invalidLimits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewStorageTrie(invalid limits) error = %v", err)
	}
	if _, err := mpt.LoadStorageTrie(mpt.EmptyRoot(), nil, mpt.DefaultLimits()); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("LoadStorageTrie(nil store) error = %v", err)
	}
	emptyStorage, err := mpt.NewStorageTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	if has, err := emptyStorage.HasSlot(context.Background(), slot); err != nil || has {
		t.Fatalf("empty StorageTrie.HasSlot() = (%t, %v)", has, err)
	}
}

func TestStateAndStorageProfilesCommitLoadAndRebuild(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := mpt.DefaultLimits()
	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[31] = 9
	value, err := mpt.NewAccountValue(
		3, balance, mpt.EmptyRoot(), mpt.EmptyCodeHash(), limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	state, err = state.UpdateAccount(ctx, address, value)
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	stateStore := newTestNodeStore()
	state, err = state.Commit(ctx, stateStore)
	if err != nil {
		t.Fatalf("StateTrie.Commit() error = %v", err)
	}
	stateRoot, err := state.Root()
	if err != nil {
		t.Fatalf("StateTrie.Root() error = %v", err)
	}
	loadedState, err := mpt.LoadStateTrie(stateRoot, stateStore, limits)
	if err != nil {
		t.Fatalf("LoadStateTrie() error = %v", err)
	}
	if account, err := loadedState.GetAccount(ctx, address); err != nil || account.Nonce() != 3 {
		t.Fatalf("loaded GetAccount() = (%#v, %v)", account, err)
	}
	rebuiltState, err := loadedState.Rebuild(ctx)
	if err != nil {
		t.Fatalf("StateTrie.Rebuild() error = %v", err)
	}
	rebuiltStateRoot, err := rebuiltState.Root()
	if err != nil {
		t.Fatalf("rebuilt StateTrie.Root() error = %v", err)
	}
	if rebuiltStateRoot != stateRoot {
		t.Fatalf("rebuilt state root = %x, want %x", rebuiltStateRoot, stateRoot)
	}
	rootNode := append([]byte(nil), stateStore.nodes[stateRoot]...)
	delete(stateStore.nodes, stateRoot)
	if _, err := loadedState.GetAccount(ctx, address); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("missing state GetAccount() error = %v", err)
	}
	recoveredState, err := loadedState.RecoverNode(ctx, stateRoot, rootNode)
	if err != nil {
		t.Fatalf("StateTrie.RecoverNode() error = %v", err)
	}
	if _, err := recoveredState.GetAccount(ctx, address); err != nil {
		t.Fatalf("recovered GetAccount() error = %v", err)
	}

	var slot [32]byte
	slot[31] = 7
	var word [32]byte
	word[31] = 5
	storage, err := mpt.NewStorageTrie(limits)
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	storage, err = storage.UpdateSlot(ctx, slot, word)
	if err != nil {
		t.Fatalf("UpdateSlot() error = %v", err)
	}
	storageStore := newTestNodeStore()
	storage, err = storage.Commit(ctx, storageStore)
	if err != nil {
		t.Fatalf("StorageTrie.Commit() error = %v", err)
	}
	storageRoot, err := storage.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root() error = %v", err)
	}
	loadedStorage, err := mpt.LoadStorageTrie(storageRoot, storageStore, limits)
	if err != nil {
		t.Fatalf("LoadStorageTrie() error = %v", err)
	}
	if got, err := loadedStorage.GetSlot(ctx, slot); err != nil || got != word {
		t.Fatalf("loaded GetSlot() = (%x, %v), want %x", got, err, word)
	}
	rebuiltStorage, err := loadedStorage.Rebuild(ctx)
	if err != nil {
		t.Fatalf("StorageTrie.Rebuild() error = %v", err)
	}
	rebuiltStorageRoot, err := rebuiltStorage.Root()
	if err != nil {
		t.Fatalf("rebuilt StorageTrie.Root() error = %v", err)
	}
	if rebuiltStorageRoot != storageRoot {
		t.Fatalf("rebuilt storage root = %x, want %x", rebuiltStorageRoot, storageRoot)
	}
	rootNode = append([]byte(nil), storageStore.nodes[storageRoot]...)
	delete(storageStore.nodes, storageRoot)
	if _, err := loadedStorage.GetSlot(ctx, slot); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("missing storage GetSlot() error = %v", err)
	}
	recoveredStorage, err := loadedStorage.RecoverNode(ctx, storageRoot, rootNode)
	if err != nil {
		t.Fatalf("StorageTrie.RecoverNode() error = %v", err)
	}
	if _, err := recoveredStorage.GetSlot(ctx, slot); err != nil {
		t.Fatalf("recovered GetSlot() error = %v", err)
	}
}

func TestStateAndStorageProfilesRejectCanonicalTrieValuesOfWrongShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := mpt.DefaultLimits()
	var address [20]byte
	genericState, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie(state) error = %v", err)
	}
	genericState, err = genericState.Update(ctx, address[:], []byte{0x80})
	if err != nil {
		t.Fatalf("state Update() error = %v", err)
	}
	stateStore := memory.New()
	genericState, err = genericState.Commit(ctx, stateStore)
	if err != nil {
		t.Fatalf("state Commit() error = %v", err)
	}
	stateRoot, err := genericState.Root()
	if err != nil {
		t.Fatalf("state Root() error = %v", err)
	}
	state, err := mpt.LoadStateTrie(stateRoot, stateStore, limits)
	if err != nil {
		t.Fatalf("LoadStateTrie() error = %v", err)
	}
	if _, err := state.GetAccount(ctx, address); !errors.Is(err, mpt.ErrInvalidAccount) {
		t.Fatalf("GetAccount(malformed value) error = %v", err)
	}

	var slot [32]byte
	genericStorage, err := mpt.NewSecureTrie(limits)
	if err != nil {
		t.Fatalf("NewSecureTrie(storage) error = %v", err)
	}
	genericStorage, err = genericStorage.Update(ctx, slot[:], []byte{0x80})
	if err != nil {
		t.Fatalf("storage Update() error = %v", err)
	}
	storageStore := memory.New()
	genericStorage, err = genericStorage.Commit(ctx, storageStore)
	if err != nil {
		t.Fatalf("storage Commit() error = %v", err)
	}
	storageRoot, err := genericStorage.Root()
	if err != nil {
		t.Fatalf("storage Root() error = %v", err)
	}
	storage, err := mpt.LoadStorageTrie(storageRoot, storageStore, limits)
	if err != nil {
		t.Fatalf("LoadStorageTrie() error = %v", err)
	}
	if _, err := storage.GetSlot(ctx, slot); !errors.Is(err, mpt.ErrInvalidStorageValue) {
		t.Fatalf("GetSlot(zero encoding) error = %v", err)
	}
}
