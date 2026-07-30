package mpt_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

const eip1186FixtureChecksum = "9ef6b4b3a4b740172a6372c717277de725ace3d8ac0af958d4087bcc13f6d000"

type eip1186Fixture struct {
	Format             int      `json:"format"`
	StateRoot          string   `json:"stateRoot"`
	Address            string   `json:"address"`
	Account            string   `json:"account"`
	AccountProof       []string `json:"accountProof"`
	StorageRoot        string   `json:"storageRoot"`
	Slot               string   `json:"slot"`
	StorageValue       string   `json:"storageValue"`
	StorageProof       []string `json:"storageProof"`
	AbsentAddress      string   `json:"absentAddress"`
	AbsentAccountProof []string `json:"absentAccountProof"`
	AbsentSlot         string   `json:"absentSlot"`
	AbsentStorageProof []string `json:"absentStorageProof"`
}

func TestEIP1186RegressionFixture(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/local-regressions/eip1186.json")
	if err != nil {
		t.Fatalf("read EIP-1186 fixture: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got !=
		eip1186FixtureChecksum {
		t.Fatalf("EIP-1186 fixture checksum = %s", got)
	}
	var fixture eip1186Fixture
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatalf("decode EIP-1186 fixture: %v", err)
	}
	if fixture.Format != 1 {
		t.Fatalf("EIP-1186 fixture format = %d", fixture.Format)
	}

	limits := mpt.DefaultLimits()
	stateRoot := fixtureRoot(t, fixture.StateRoot)
	storageRoot := fixtureRoot(t, fixture.StorageRoot)
	address := [20]byte(fixtureFixedBytes(t, fixture.Address, 20))
	accountEncoding := fixtureHexBytes(t, fixture.Account)
	accountProof := fixtureProof(t, fixture.AccountProof, limits)
	account, err := mpt.VerifyAccountProof(
		context.Background(),
		stateRoot,
		address,
		accountEncoding,
		accountProof,
		limits,
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof(fixture) error = %v", err)
	}
	if account.StorageRoot() != storageRoot {
		t.Fatalf(
			"account storage root = %x, want %x",
			account.StorageRoot(),
			storageRoot,
		)
	}

	slot := [32]byte(fixtureFixedBytes(t, fixture.Slot, 32))
	if err := mpt.VerifyStorageProof(
		context.Background(),
		account,
		slot,
		fixtureHexBytes(t, fixture.StorageValue),
		fixtureProof(t, fixture.StorageProof, limits),
		limits,
	); err != nil {
		t.Fatalf("VerifyStorageProof(fixture) error = %v", err)
	}

	absentAddress := [20]byte(
		fixtureFixedBytes(t, fixture.AbsentAddress, 20),
	)
	if err := mpt.VerifyAccountAbsence(
		context.Background(),
		stateRoot,
		absentAddress,
		fixtureProof(t, fixture.AbsentAccountProof, limits),
		limits,
	); err != nil {
		t.Fatalf("VerifyAccountAbsence(fixture) error = %v", err)
	}

	absentSlot := [32]byte(fixtureFixedBytes(t, fixture.AbsentSlot, 32))
	if err := mpt.VerifyStorageProof(
		context.Background(),
		account,
		absentSlot,
		nil,
		fixtureProof(t, fixture.AbsentStorageProof, limits),
		limits,
	); err != nil {
		t.Fatalf("VerifyStorageProof(absence fixture) error = %v", err)
	}
}

func fixtureRoot(t *testing.T, encoded string) mpt.Root {
	t.Helper()
	root, err := mpt.RootFromBytes(fixtureFixedBytes(t, encoded, mpt.RootBytes))
	if err != nil {
		t.Fatalf("decode fixture root: %v", err)
	}
	return root
}

func fixtureProof(
	t *testing.T,
	encoded []string,
	limits mpt.Limits,
) mpt.Proof {
	t.Helper()
	nodes := make([][]byte, len(encoded))
	for index, node := range encoded {
		decoded, err := hex.DecodeString(node)
		if err != nil {
			t.Fatalf("decode fixture proof node %d: %v", index, err)
		}
		nodes[index] = decoded
	}
	proof, err := mpt.ProofFromNodes(nodes, limits)
	if err != nil {
		t.Fatalf("construct fixture proof: %v", err)
	}
	return proof
}
