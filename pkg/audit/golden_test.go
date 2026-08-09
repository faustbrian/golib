package audit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestCanonicalGoldenRecordAndIndependentChainDigest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "golden-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.approved", Outcome: audit.OutcomeSucceeded,
		ReasonCode: "policy_match", Description: "approved automatically",
		Actor: audit.ActorInput{Kind: audit.ActorService, ID: "billing", AuthenticationMethod: "workload_identity",
			DelegatedBy: &audit.ActorInput{Kind: audit.ActorHuman, ID: "user-42", AuthenticationMethod: "passkey"}},
		Subject:    audit.SubjectInput{Type: "invoice", ID: "invoice-7"},
		Context:    audit.ContextInput{TenantID: "tenant-1", CorrelationID: "corr-1", SourceService: "billing", Environment: "production"},
		Changes:    audit.ChangeSetInput{Before: map[string]string{"status": "pending"}, After: map[string]string{"status": "approved"}},
		Policy:     audit.PolicyMetadata{PolicyID: "approval", Version: "2026-08-01"},
		Attributes: map[string]string{"app.channel": "automatic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	chain, _ := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	record, err = chain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := audit.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/golden-record-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	golden = bytes.TrimSpace(golden)
	if !bytes.Equal(encoded, golden) {
		t.Fatalf("canonical record changed\n got: %s\nwant: %s", encoded, golden)
	}

	digestHex := hex.EncodeToString(record.Integrity().Digest())
	unsigned := strings.Replace(string(golden), `,"digest":"`+digestHex+`"`, "", 1)
	if unsigned == string(golden) {
		t.Fatal("golden digest field was not found")
	}
	want := sha256.Sum256([]byte(unsigned))
	if !bytes.Equal(record.Integrity().Digest(), want[:]) {
		t.Fatalf("chain digest = %x, independent digest = %x", record.Integrity().Digest(), want)
	}
}
