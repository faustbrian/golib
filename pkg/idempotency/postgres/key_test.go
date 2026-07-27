package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	idempotencypostgres "github.com/faustbrian/golib/pkg/idempotency/postgres"
)

func TestRecordKeyDigestIsStableOpaqueAndIndependent(t *testing.T) {
	key, err := idempotency.NewKey(
		"postal.import.v1",
		"postal",
		"provider-fence",
		"worker",
		"posti:FI",
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	digest := idempotencypostgres.RecordKeyDigest(key)
	if len(digest) != sha256.Size ||
		hex.EncodeToString(digest) !=
			"82c9addbc295f6ed7d8da3508b4319521268c1d9df508ed32fde0843fef7bfee" {
		t.Fatalf("RecordKeyDigest() = %x", digest)
	}

	digest[0] ^= 0xff
	fresh := idempotencypostgres.RecordKeyDigest(key)
	if hex.EncodeToString(fresh) !=
		"82c9addbc295f6ed7d8da3508b4319521268c1d9df508ed32fde0843fef7bfee" {
		t.Fatal("RecordKeyDigest() returned shared mutable state")
	}

	other, err := idempotency.NewKey(
		"postal.import.v1",
		"postal",
		"provider-fence",
		"worker",
		"posti:F",
	)
	if err != nil {
		t.Fatalf("NewKey(other) error = %v", err)
	}
	if hex.EncodeToString(idempotencypostgres.RecordKeyDigest(other)) ==
		hex.EncodeToString(fresh) {
		t.Fatal("RecordKeyDigest() conflated distinct key parts")
	}
}
