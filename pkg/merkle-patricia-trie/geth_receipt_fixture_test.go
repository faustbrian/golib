package mpt_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

const gethReceiptFixtureDirectory = "testdata/go-ethereum"

const gethReceiptFixtureLicenseChecksum = "3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986"

var gethReceiptFixtureChecksums = map[string]string{
	"cmd/evm/testdata/1/exp.json":   "50296a4d39478d0e2a08db144a53a0d1c33a54fbdd5604f5f3d406e82f991758",
	"cmd/evm/testdata/13/exp2.json": "b3835420252fd3eb61676728ad83b4c35469d007c14e5e0abb2cfd117c192b58",
	"cmd/evm/testdata/28/exp.json":  "d4547eeaa055b7e3c90a1cddbfb616fc193be356034a2a239731fc74cc5c678e",
	"cmd/evm/testdata/33/exp.json":  "8aa6a6530afca88899c105908e032387c51a2fa6019f4002726320a7c97270f4",
}

type gethTransitionFixture struct {
	Result gethTransitionResult `json:"result"`
}

type gethTransitionResult struct {
	ReceiptsRoot string               `json:"receiptsRoot"`
	Receipts     []gethFixtureReceipt `json:"receipts"`
}

type gethFixtureReceipt struct {
	Type              string           `json:"type"`
	Root              string           `json:"root"`
	Status            string           `json:"status"`
	CumulativeGasUsed string           `json:"cumulativeGasUsed"`
	LogsBloom         string           `json:"logsBloom"`
	Logs              []gethFixtureLog `json:"logs"`
}

type gethFixtureLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

func TestGethReceiptFixtureChecksums(t *testing.T) {
	t.Parallel()

	assertFixtureChecksum(
		t,
		filepath.Join(gethReceiptFixtureDirectory, "COPYING"),
		gethReceiptFixtureLicenseChecksum,
	)
	for name, checksum := range gethReceiptFixtureChecksums {
		name, checksum := name, checksum
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertFixtureChecksum(
				t,
				filepath.Join(gethReceiptFixtureDirectory, name),
				checksum,
			)
		})
	}
}

func TestGethReceiptRoots(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(gethReceiptFixtureChecksums))
	for name := range gethReceiptFixtureChecksums {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := decodeGethReceiptFixture(t, name)
			transactions := make(
				[]mpt.EncodedTransactionValue,
				len(fixture.Result.Receipts),
			)
			receipts := make(
				[]mpt.EncodedReceiptValue,
				len(fixture.Result.Receipts),
			)
			for index, receipt := range fixture.Result.Receipts {
				transactions[index], receipts[index] =
					gethFixtureEnvelope(t, receipt)
			}
			root, err := mpt.ReceiptRoot(
				context.Background(),
				transactions,
				receipts,
				mpt.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("ReceiptRoot() error = %v", err)
			}
			assertRootHex(t, root, fixture.Result.ReceiptsRoot)
		})
	}
}

func gethFixtureEnvelope(
	t *testing.T,
	receipt gethFixtureReceipt,
) (mpt.EncodedTransactionValue, mpt.EncodedReceiptValue) {
	t.Helper()

	payload := encodeGethFixtureReceipt(t, receipt)
	if receipt.Type == "" {
		transaction, err := mpt.LegacyTransactionValue(
			[]byte{0xc0},
			mpt.DefaultLimits(),
		)
		if err != nil {
			t.Fatalf("LegacyTransactionValue() error = %v", err)
		}
		encodedReceipt, err := mpt.LegacyReceiptValue(
			payload,
			mpt.DefaultLimits(),
		)
		if err != nil {
			t.Fatalf("LegacyReceiptValue() error = %v", err)
		}
		return transaction, encodedReceipt
	}

	envelopeType := fixtureUint64(t, receipt.Type)
	if envelopeType == 0 || envelopeType > 4 {
		t.Fatalf("unsupported receipt type %q", receipt.Type)
	}
	profile := mpt.LondonProfile
	switch envelopeType {
	case 3:
		profile = mpt.CancunProfile
	case 4:
		profile = mpt.PragueProfile
	}
	transaction, err := mpt.TypedTransactionValue(
		profile,
		byte(envelopeType),
		[]byte{0xc0},
		mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue() error = %v", err)
	}
	encodedReceipt, err := mpt.TypedReceiptValue(
		profile,
		byte(envelopeType),
		payload,
		mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue() error = %v", err)
	}
	return transaction, encodedReceipt
}

func encodeGethFixtureReceipt(
	t *testing.T,
	receipt gethFixtureReceipt,
) []byte {
	t.Helper()

	first := receiptIntegerBytes(t, receipt.Status)
	if receipt.Root != "" && receipt.Root != "0x" {
		first = fixtureFixedBytes(t, receipt.Root, 32)
	}
	logs := make([]rlp.Value, len(receipt.Logs))
	for index, log := range receipt.Logs {
		topics := make([]rlp.Value, len(log.Topics))
		for topicIndex, topic := range log.Topics {
			topics[topicIndex] = rlp.String(
				fixtureFixedBytes(t, topic, 32),
			)
		}
		logs[index] = rlp.List(
			rlp.String(fixtureFixedBytes(t, log.Address, 20)),
			rlp.List(topics...),
			rlp.String(fixtureHexBytes(t, log.Data)),
		)
	}
	encoded, err := rlp.Encode(
		rlp.List(
			rlp.String(first),
			rlp.String(
				receiptIntegerBytes(t, receipt.CumulativeGasUsed),
			),
			rlp.String(fixtureFixedBytes(t, receipt.LogsBloom, 256)),
			rlp.List(logs...),
		),
		rlp.Limits{
			MaxEncodedBytes: mpt.DefaultLimits().MaxValueBytes,
			MaxDepth:        8,
			MaxItems:        1 << 16,
		},
	)
	if err != nil {
		t.Fatalf("encode receipt RLP: %v", err)
	}
	return encoded
}

func receiptIntegerBytes(t *testing.T, value string) []byte {
	t.Helper()

	encoded := fixtureHexBytes(t, value)
	for len(encoded) > 0 && encoded[0] == 0 {
		encoded = encoded[1:]
	}
	return encoded
}

func decodeGethReceiptFixture(
	t *testing.T,
	name string,
) gethTransitionFixture {
	t.Helper()

	contents, err := os.ReadFile(
		filepath.Join(gethReceiptFixtureDirectory, name),
	)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var fixture gethTransitionFixture
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if fixture.Result.ReceiptsRoot == "" ||
		len(fixture.Result.Receipts) == 0 {
		t.Fatalf("%s has no receipt-root evidence", name)
	}
	return fixture
}

func assertFixtureChecksum(t *testing.T, name, want string) {
	t.Helper()

	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(contents))
	if !strings.EqualFold(got, want) {
		t.Fatalf("%s SHA-256 = %s, want %s", name, got, want)
	}
}
