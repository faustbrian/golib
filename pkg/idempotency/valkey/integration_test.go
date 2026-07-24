package valkey

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	valkeygo "github.com/valkey-io/valkey-go"
)

func TestValkey9Conformance(t *testing.T) {
	client := integrationClient(t)
	runValkeyConformance(t, client, "idempotency-conformance")
}

func TestValkey9ClusterConformance(t *testing.T) {
	addresses := os.Getenv("VALKEY_CLUSTER_ADDRS")
	if addresses == "" {
		t.Skip("VALKEY_CLUSTER_ADDRS is required for Valkey 9 cluster tests")
	}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress: strings.Split(addresses, ","),
	})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	runValkeyConformance(t, client, "idempotency-cluster-conformance")
}

func runValkeyConformance(t *testing.T, client valkeygo.Client, prefix string) {
	t.Helper()
	idempotencytest.RunStoreConformance(t, func(t testing.TB) idempotencytest.StoreFixture {
		t.Helper()
		key, err := idempotency.NewKey("conformance", "tenant", "operation", "caller", t.Name())
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		fingerprint, err := idempotency.NewFingerprint("v1", []byte("canonical request"))
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}
		tokens := idempotencytest.NewTokenSource("valkey-owner")
		store, err := Open(context.Background(), client, Options{
			Prefix: prefix, Retention: time.Hour, OwnerTokens: tokens.Next,
		})
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		storageKey := recordKey(prefix, key)
		t.Cleanup(func() {
			if err := client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error(); err != nil {
				t.Errorf("DEL cleanup error = %v", err)
			}
		})

		return idempotencytest.StoreFixture{
			Store:       store,
			Key:         key,
			Fingerprint: fingerprint,
			Advance: func(duration time.Duration) {
				if err := client.Do(
					context.Background(),
					client.B().Hincrby().Key(storageKey).Field(fieldLeaseExpiresAt).
						Increment(-duration.Milliseconds()).Build(),
				).Error(); err != nil {
					t.Fatalf("HINCRBY lease error = %v", err)
				}
			},
		}
	})
}

func TestValkeyTTLsAndBinaryReplay(t *testing.T) {
	client := integrationClient(t)
	key, err := idempotency.NewKey("integration", "tenant", "binary", "caller", t.Name())
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("binary request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	tokens := idempotencytest.NewTokenSource("binary-owner")
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-integration", Retention: 2 * time.Minute, OwnerTokens: tokens.Next,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	storageKey := recordKey("idempotency-integration", key)
	t.Cleanup(func() {
		_ = client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error()
	})

	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	assertPTTL(t, client, storageKey, 3*time.Minute)
	binaryResult := []byte{0xff, 0x00, 0x01, 'x'}
	_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(),
		Result:    binaryResult,
		Metadata:  map[string]string{"content-type": "application/octet-stream"},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertPTTL(t, client, storageKey, 2*time.Minute)
	replay, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() replay error = %v", err)
	}
	if replay.Outcome != idempotency.OutcomeReplayed ||
		string(replay.Record.Result) != string(binaryResult) {
		t.Fatalf("replay = %#v", replay)
	}
}

func TestValkeyLeaseTimestampsUseServerTime(t *testing.T) {
	client := integrationClient(t)
	key, err := idempotency.NewKey(
		"integration", "tenant", "server-clock", "caller", t.Name(),
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("server clock"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-integration", Retention: time.Minute,
		OwnerTokens: idempotencytest.NewTokenSource("server-clock-owner").Next,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	storageKey := recordKey("idempotency-integration", key)
	t.Cleanup(func() {
		_ = client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error()
	})

	before := serverTime(t, client)
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	after := serverTime(t, client)
	created := acquired.Record.CreatedAt
	if created.UnixMilli() < before.UnixMilli() ||
		created.UnixMilli() > after.UnixMilli() ||
		!acquired.Record.HeartbeatAt.Equal(created) ||
		!acquired.Record.UpdatedAt.Equal(created) ||
		!acquired.Record.LeaseExpiresAt.Equal(created.Add(time.Minute)) {
		t.Fatalf(
			"Acquire() timestamps = %#v, server interval = [%v, %v]",
			acquired.Record, before, after,
		)
	}
}

func TestValkeyClientLossFailsClosed(t *testing.T) {
	address := integrationAddress(t)
	client, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	tokens := idempotencytest.NewTokenSource("closed-owner")
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-integration", Retention: time.Minute, OwnerTokens: tokens.Next,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	client.Close()
	key, err := idempotency.NewKey("integration", "tenant", "closed", "caller", t.Name())
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("closed request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}

	result, err := service.Begin(context.Background(), idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{Key: key, Fingerprint: fingerprint, Lease: time.Minute},
	})
	if err == nil || result.Execute || result.Outcome != idempotency.OutcomeUnavailable {
		t.Fatalf("Begin() = %#v, %v", result, err)
	}
}

func TestValkeyOpenRejectsEvictingServer(t *testing.T) {
	client := integrationClient(t)
	setMaxmemoryPolicy(t, client, "allkeys-lru")
	t.Cleanup(func() {
		setMaxmemoryPolicy(t, client, "noeviction")
	})

	_, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-integration", Retention: time.Minute,
		OwnerTokens: func() (string, error) { return "unused-owner", nil },
	})
	assertStoreReason(t, err, idempotency.ReasonUnsafeBackend)
}

func TestValkeyReplicaPromotionPreservesOwnership(t *testing.T) {
	addresses := strings.Split(os.Getenv("VALKEY_FAILOVER_ADDRS"), ",")
	if len(addresses) != 2 || addresses[0] == "" || addresses[1] == "" {
		t.Skip("VALKEY_FAILOVER_ADDRS must contain primary and replica addresses")
	}
	primary, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress: []string{addresses[0]}, DisableRetry: true,
	})
	if err != nil {
		t.Fatalf("valkey.NewClient(primary) error = %v", err)
	}
	defer primary.Close()
	replica, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress: []string{addresses[1]}, DisableRetry: true,
	})
	if err != nil {
		t.Fatalf("valkey.NewClient(replica) error = %v", err)
	}
	defer replica.Close()
	waitForValkeyCondition(t, "replica link", func() (bool, error) {
		info, err := replica.Do(
			context.Background(),
			replica.B().Info().Section("replication").Build(),
		).ToString()
		return strings.Contains(info, "master_link_status:up"), err
	})

	store, err := Open(context.Background(), primary, Options{
		Prefix: "idempotency-failover", Retention: time.Minute,
		OwnerTokens: idempotencytest.NewTokenSource("failover-owner").Next,
	})
	if err != nil {
		t.Fatalf("Open(primary) error = %v", err)
	}
	key, err := idempotency.NewKey(
		"failover", "tenant", "promotion", "caller", t.Name(),
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("failover request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(primary) error = %v", err)
	}
	storageKey := recordKey("idempotency-failover", key)
	t.Cleanup(func() {
		_ = replica.Do(
			context.Background(), replica.B().Del().Key(storageKey).Build(),
		).Error()
	})
	replicated, err := primary.Do(
		context.Background(),
		primary.B().Arbitrary("WAIT").Args("1", "5000").Build(),
	).AsInt64()
	if err != nil || replicated != 1 {
		t.Fatalf("WAIT = %d, %v, want one replica", replicated, err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = primary.Do(
		shutdownContext,
		primary.B().Arbitrary("SHUTDOWN").Args("NOSAVE").Build(),
	).Error()
	primary.Close()
	if err := replica.Do(
		context.Background(),
		replica.B().Arbitrary("REPLICAOF").Args("NO", "ONE").Build(),
	).Error(); err != nil {
		t.Fatalf("REPLICAOF NO ONE error = %v", err)
	}
	waitForValkeyCondition(t, "replica promotion", func() (bool, error) {
		info, err := replica.Do(
			context.Background(),
			replica.B().Info().Section("replication").Build(),
		).ToString()
		return strings.Contains(info, "role:master"), err
	})

	promoted, err := Open(context.Background(), replica, Options{
		Prefix: "idempotency-failover", Retention: time.Minute,
		OwnerTokens: idempotencytest.NewTokenSource("promoted-owner").Next,
	})
	if err != nil {
		t.Fatalf("Open(promoted replica) error = %v", err)
	}
	record, err := promoted.Inspect(context.Background(), key)
	if err != nil || record.State != idempotency.StateAcquired ||
		record.Ownership() != acquired.Record.Ownership() {
		t.Fatalf("Inspect(promoted replica) = %#v, %v", record, err)
	}
	completed, err := promoted.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: record.Ownership(), Result: []byte("recovered"),
	})
	if err != nil || completed.State != idempotency.StateCompleted ||
		string(completed.Result) != "recovered" {
		t.Fatalf("Complete(promoted replica) = %#v, %v", completed, err)
	}
}

func waitForValkeyCondition(
	t *testing.T,
	description string,
	condition func() (bool, error),
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastError error
	for time.Now().Before(deadline) {
		ready, err := condition()
		if ready && err == nil {
			return
		}
		lastError = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", description, lastError)
}

func integrationClient(t testing.TB) valkeygo.Client {
	t.Helper()
	address := integrationAddress(t)
	client, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	info, err := client.Do(context.Background(), client.B().Info().Section("server").Build()).ToString()
	if err != nil {
		t.Fatalf("INFO server error = %v", err)
	}
	if !strings.Contains(info, "valkey_version:9.") {
		t.Fatalf("integration server is not Valkey 9: %s", info)
	}
	return client
}

func integrationAddress(t testing.TB) string {
	t.Helper()
	address := os.Getenv("VALKEY_ADDR")
	if address == "" {
		t.Skip("VALKEY_ADDR is required for Valkey 9 integration tests")
	}
	return address
}

func assertPTTL(t *testing.T, client valkeygo.Client, key string, want time.Duration) {
	t.Helper()
	ttl, err := client.Do(context.Background(), client.B().Pttl().Key(key).Build()).AsInt64()
	if err != nil {
		t.Fatalf("PTTL error = %v", err)
	}
	minimum := want.Milliseconds() - 2_000
	if ttl < minimum || ttl > want.Milliseconds() {
		t.Fatalf("PTTL = %dms, want between %dms and %dms", ttl, minimum, want.Milliseconds())
	}
}

func setMaxmemoryPolicy(t *testing.T, client valkeygo.Client, policy string) {
	t.Helper()
	if err := client.Do(
		context.Background(),
		client.B().ConfigSet().ParameterValue().ParameterValue(
			"maxmemory-policy", policy,
		).Build(),
	).Error(); err != nil {
		t.Fatalf("CONFIG SET maxmemory-policy error = %v", err)
	}
}

func serverTime(t *testing.T, client valkeygo.Client) time.Time {
	t.Helper()
	parts, err := client.Do(context.Background(), client.B().Time().Build()).ToArray()
	if err != nil || len(parts) != 2 {
		t.Fatalf("TIME reply = %#v, %v", parts, err)
	}
	seconds, err := parts[0].AsInt64()
	if err != nil {
		t.Fatalf("TIME seconds error = %v", err)
	}
	microseconds, err := parts[1].AsInt64()
	if err != nil {
		t.Fatalf("TIME microseconds error = %v", err)
	}
	return time.Unix(seconds, microseconds*int64(time.Microsecond)).UTC()
}
