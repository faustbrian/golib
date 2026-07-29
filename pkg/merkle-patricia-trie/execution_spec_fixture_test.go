package mpt_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
	"golang.org/x/crypto/sha3"
)

const executionFixtureDirectory = "testdata/execution-spec-tests"

const executionFixtureLicenseChecksum = "311a5b206cfa2f48af8084c8fb96b417e6b0034362a1319fc3757826d6042427"

var executionFixtureChecksums = map[string]string{
	"blockchain_tests/frontier/examples/test_block_intermediate_state.json":           "411035cccfee534648135073873e94aba2e58c2d2eb09aa41a377305b3da1c2f",
	"blockchain_tests/berlin/eip2930_access_list/test_eip2930_tx_validity.json":       "860c386fcb930553b7a6487e805bcfbd65d430ca5548e3d8bcddf05c62220670",
	"blockchain_tests/london/eip1559_fee_market_change/test_eip1559_tx_validity.json": "d12a301b9cd3aa4a1978a336e24874dbc70b4ae4d3ff1fc3e8f4944457571b91",
	"blockchain_tests/cancun/eip4844_blobs/test_blobhash_multiple_txs_in_block.json":  "0910707d823e54042a6f8be1dd87822856390172d1f2ff7924caf6dabfc9cf9e",
	"blockchain_tests/prague/eip7702_set_code_tx/test_eip_7702.json":                  "7e1b668d91043606afedba88775e825f0fded12f0c4ba98d8c452a023221750e",
}

type executionBlockchainFixture struct {
	Network            string                             `json:"network"`
	Pre                map[string]executionFixtureAccount `json:"pre"`
	PostState          map[string]executionFixtureAccount `json:"postState"`
	GenesisBlockHeader executionFixtureHeader             `json:"genesisBlockHeader"`
	Blocks             []executionFixtureBlock            `json:"blocks"`
}

type executionFixtureAccount struct {
	Nonce   string            `json:"nonce"`
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Storage map[string]string `json:"storage"`
}

type executionFixtureHeader struct {
	StateRoot        string `json:"stateRoot"`
	TransactionsTrie string `json:"transactionsTrie"`
}

type executionFixtureBlock struct {
	RLP             string                 `json:"rlp"`
	ExpectException string                 `json:"expectException"`
	BlockHeader     executionFixtureHeader `json:"blockHeader"`
	Transactions    []map[string]any       `json:"transactions"`
}

func TestExecutionSpecFixtureChecksums(t *testing.T) {
	t.Parallel()

	license, err := os.ReadFile(
		filepath.Join(executionFixtureDirectory, "LICENSE"),
	)
	if err != nil {
		t.Fatalf("read fixture LICENSE: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(license)); got != executionFixtureLicenseChecksum {
		t.Fatalf(
			"LICENSE SHA-256 = %s, want %s",
			got,
			executionFixtureLicenseChecksum,
		)
	}

	for name, want := range executionFixtureChecksums {
		name, want := name, want
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contents := readExecutionFixture(t, name)
			if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != want {
				t.Fatalf("SHA-256 = %s, want %s", got, want)
			}
		})
	}
}

func TestExecutionSpecStateAndTransactionRoots(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(executionFixtureChecksums))
	for name := range executionFixtureChecksums {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixtures := decodeExecutionFixtures(t, name)
			fixtureNames := make([]string, 0, len(fixtures))
			for fixtureName := range fixtures {
				fixtureNames = append(fixtureNames, fixtureName)
			}
			slices.Sort(fixtureNames)
			for _, fixtureName := range fixtureNames {
				fixture := fixtures[fixtureName]
				t.Run(fixtureName, func(t *testing.T) {
					t.Parallel()
					assertExecutionFixtureRoots(t, fixture)
				})
			}
		})
	}
}

func assertExecutionFixtureRoots(
	t *testing.T,
	fixture executionBlockchainFixture,
) {
	t.Helper()

	genesisRoot := allocationRoot(t, fixture.Pre)
	assertRootHex(t, genesisRoot, fixture.GenesisBlockHeader.StateRoot)

	expectedPostRoot := fixture.GenesisBlockHeader.StateRoot
	for blockIndex, block := range fixture.Blocks {
		if block.ExpectException != "" {
			continue
		}
		if block.BlockHeader.StateRoot == "" ||
			block.BlockHeader.TransactionsTrie == "" {
			t.Fatalf("block %d has incomplete root commitments", blockIndex)
		}
		expectedPostRoot = block.BlockHeader.StateRoot
		got := transactionRootFromBlock(t, fixture.Network, block)
		assertRootHex(t, got, block.BlockHeader.TransactionsTrie)
	}
	if fixture.PostState == nil {
		t.Fatal("fixture has no postState allocation")
	}
	assertRootHex(t, allocationRoot(t, fixture.PostState), expectedPostRoot)
}

func allocationRoot(
	t *testing.T,
	allocation map[string]executionFixtureAccount,
) mpt.Root {
	t.Helper()

	limits := mpt.DefaultLimits()
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	addresses := make([]string, 0, len(allocation))
	for address := range allocation {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	for _, encodedAddress := range addresses {
		account := allocation[encodedAddress]
		addressBytes := fixtureFixedBytes(t, encodedAddress, 20)
		var address [20]byte
		copy(address[:], addressBytes)
		storageRoot := allocationStorageRoot(t, account.Storage)
		code := fixtureHexBytes(t, account.Code)
		codeHash := legacyKeccak256(code)
		value, valueErr := mpt.NewAccountValue(
			fixtureUint64(t, account.Nonce),
			fixtureWord(t, account.Balance),
			storageRoot,
			codeHash,
			limits,
		)
		if valueErr != nil {
			t.Fatalf("NewAccountValue(%s) error = %v", encodedAddress, valueErr)
		}
		state, err = state.UpdateAccount(
			context.Background(), address, value,
		)
		if err != nil {
			t.Fatalf("UpdateAccount(%s) error = %v", encodedAddress, err)
		}
	}
	root, err := state.Root()
	if err != nil {
		t.Fatalf("StateTrie.Root() error = %v", err)
	}
	return root
}

func allocationStorageRoot(
	t *testing.T,
	allocation map[string]string,
) mpt.Root {
	t.Helper()

	storage, err := mpt.NewStorageTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	slots := make([]string, 0, len(allocation))
	for slot := range allocation {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	for _, encodedSlot := range slots {
		storage, err = storage.UpdateSlot(
			context.Background(),
			fixtureWord(t, encodedSlot),
			fixtureWord(t, allocation[encodedSlot]),
		)
		if err != nil {
			t.Fatalf("UpdateSlot(%s) error = %v", encodedSlot, err)
		}
	}
	root, err := storage.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root() error = %v", err)
	}
	return root
}

func transactionRootFromBlock(
	t *testing.T,
	network string,
	block executionFixtureBlock,
) mpt.Root {
	t.Helper()

	blockBytes := fixtureHexBytes(t, block.RLP)
	decoded, err := rlp.Decode(blockBytes, rlp.Limits{
		MaxEncodedBytes: 16 << 20,
		MaxDepth:        32,
		MaxItems:        1 << 16,
	})
	if err != nil {
		t.Fatalf("decode block RLP: %v", err)
	}
	blockElements := decoded.Elements()
	if decoded.Kind() != rlp.KindList || len(blockElements) < 3 {
		t.Fatalf("block RLP has invalid arity %d", len(blockElements))
	}
	transactions := blockElements[1]
	if transactions.Kind() != rlp.KindList {
		t.Fatal("block transactions are not an RLP list")
	}
	transactionItems := transactions.Elements()
	if len(transactionItems) != len(block.Transactions) {
		t.Fatalf(
			"block transaction count = %d, decoded fixture count = %d",
			len(transactionItems), len(block.Transactions),
		)
	}
	values := make([]mpt.EncodedTransactionValue, 0, len(transactionItems))
	for index, item := range transactionItems {
		value, valueErr := transactionFixtureValue(t, network, item)
		if valueErr != nil {
			t.Fatalf("transaction %d: %v", index, valueErr)
		}
		values = append(values, value)
	}
	root, err := mpt.TransactionRoot(
		context.Background(), values, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	return root
}

func transactionFixtureValue(
	t *testing.T,
	network string,
	value rlp.Value,
) (mpt.EncodedTransactionValue, error) {
	t.Helper()

	if value.Kind() == rlp.KindList {
		encoded, err := rlp.Encode(value, rlp.DefaultLimits())
		if err != nil {
			return mpt.EncodedTransactionValue{}, err
		}
		return mpt.LegacyTransactionValue(encoded, mpt.DefaultLimits())
	}
	envelope := value.Bytes()
	if len(envelope) < 2 {
		return mpt.EncodedTransactionValue{}, fmt.Errorf(
			"typed transaction envelope is truncated",
		)
	}
	return mpt.TypedTransactionValue(
		executionFixtureProfile(t, network),
		envelope[0],
		envelope[1:],
		mpt.DefaultLimits(),
	)
}

func executionFixtureProfile(t *testing.T, network string) mpt.ForkProfile {
	t.Helper()

	switch network {
	case "Berlin":
		return mpt.BerlinProfile
	case "London":
		return mpt.LondonProfile
	case "Paris":
		return mpt.ParisProfile
	case "Shanghai":
		return mpt.ShanghaiProfile
	case "Cancun":
		return mpt.CancunProfile
	case "Prague":
		return mpt.PragueProfile
	default:
		t.Fatalf("unsupported typed-transaction fixture network %q", network)
		return 0
	}
}

func decodeExecutionFixtures(
	t *testing.T,
	name string,
) map[string]executionBlockchainFixture {
	t.Helper()

	var fixtures map[string]executionBlockchainFixture
	if err := json.Unmarshal(readExecutionFixture(t, name), &fixtures); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return fixtures
}

func readExecutionFixture(t *testing.T, name string) []byte {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join(executionFixtureDirectory, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
}

func fixtureUint64(t *testing.T, value string) uint64 {
	t.Helper()

	trimmed := strings.TrimPrefix(value, "0x")
	if trimmed == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(trimmed, 16, 64)
	if err != nil {
		t.Fatalf("decode uint64 %q: %v", value, err)
	}
	return parsed
}

func fixtureWord(t *testing.T, value string) [32]byte {
	t.Helper()

	decoded := fixtureHexBytes(t, value)
	if len(decoded) > 32 {
		t.Fatalf("word %q is %d bytes", value, len(decoded))
	}
	var word [32]byte
	copy(word[len(word)-len(decoded):], decoded)
	return word
}

func fixtureFixedBytes(t *testing.T, value string, size int) []byte {
	t.Helper()

	decoded := fixtureHexBytes(t, value)
	if len(decoded) != size {
		t.Fatalf("fixed value %q is %d bytes, want %d", value, len(decoded), size)
	}
	return decoded
}

func fixtureHexBytes(t *testing.T, value string) []byte {
	t.Helper()

	trimmed := strings.TrimPrefix(value, "0x")
	if len(trimmed)%2 != 0 {
		trimmed = "0" + trimmed
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		t.Fatalf("decode hex %q: %v", value, err)
	}
	return decoded
}

func legacyKeccak256(value []byte) [32]byte {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(value)
	var result [32]byte
	copy(result[:], hasher.Sum(nil))
	return result
}

func assertRootHex(t *testing.T, got mpt.Root, expected string) {
	t.Helper()

	want, err := mpt.RootFromBytes(fixtureFixedBytes(t, expected, 32))
	if err != nil {
		t.Fatalf("RootFromBytes() error = %v", err)
	}
	if got != want {
		t.Fatalf("root = %x, want %x", got, want)
	}
}
