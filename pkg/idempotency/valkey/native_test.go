package valkey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestNewRequiresNativeClient(t *testing.T) {
	t.Parallel()

	_, err := New(nil, Options{
		Prefix:      "idempotency",
		Retention:   time.Hour,
		OwnerTokens: func() (string, error) { return "owner", nil },
	})

	assertStoreReason(t, err, idempotency.ReasonInvalidConfiguration)
}

func TestNewBuildsStoreWithNativeExecutor(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	store, err := New(client, Options{
		Prefix:      "idempotency",
		Retention:   time.Hour,
		OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if store == nil {
		t.Fatal("New() store = nil")
	}
}

func TestNativeExecutorDecodesScriptArray(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
		valkeymock.ValkeyArray(
			valkeymock.ValkeyString("ok"),
			valkeymock.ValkeyBlobString("binary\x00value"),
		),
	))
	executor := &nativeExecutor{client: client}

	reply, err := executor.Exec(context.Background(), operationInspect, "key", nil)
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if len(reply) != 2 || reply[0] != "ok" || reply[1] != "binary\x00value" {
		t.Fatalf("Exec() = %#v", reply)
	}
}

func TestNativeExecutorPropagatesClientAndMessageErrors(t *testing.T) {
	t.Parallel()

	t.Run("client", func(t *testing.T) {
		t.Parallel()
		backendErr := errors.New("connection lost")
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
		_, err := (&nativeExecutor{client: client}).Exec(
			context.Background(), operationInspect, "key", nil,
		)
		if !errors.Is(err, backendErr) {
			t.Fatalf("Exec() error = %v", err)
		}
	})

	t.Run("message", func(t *testing.T) {
		t.Parallel()
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
			valkeymock.ValkeyArray(valkeymock.ValkeyArray(valkeymock.ValkeyString("nested"))),
		))
		_, err := (&nativeExecutor{client: client}).Exec(
			context.Background(), operationInspect, "key", nil,
		)
		if err == nil {
			t.Fatal("Exec() error = nil")
		}
	})
}

func TestNativeExecutorRejectsUnknownOperation(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	_, err := (&nativeExecutor{client: client}).Exec(
		context.Background(), operation("future"), "key", nil,
	)
	if err == nil {
		t.Fatal("Exec() error = nil")
	}
}

func TestLeaseMutationScriptsReadValkeyClock(t *testing.T) {
	t.Parallel()

	scripts := map[operation]string{
		operationAcquire:   acquireScript,
		operationHeartbeat: heartbeatScript,
		operationComplete:  completeScript,
		operationFail:      failScript,
		operationRelease:   releaseScript,
		operationExpire:    expireScript,
	}
	for operation, script := range scripts {
		if !strings.Contains(script, "redis.call('TIME')") {
			t.Fatalf("%s script does not read authoritative Valkey time", operation)
		}
	}
}

func TestNativeCheckRequiresValkey9AndNoEviction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		info   string
		policy string
		reason idempotency.Reason
	}{
		"safe": {
			info: "# Server\r\nvalkey_version:9.0.3\r\n", policy: "noeviction",
		},
		"old version": {
			info: "# Server\r\nvalkey_version:8.1.0\r\n", policy: "noeviction",
			reason: idempotency.ReasonUnsafeBackend,
		},
		"eviction": {
			info: "# Server\r\nvalkey_version:9.0.3\r\n", policy: "allkeys-lru",
			reason: idempotency.ReasonUnsafeBackend,
		},
		"malformed version": {
			info: "# Server\r\nvalkey_version:future\r\n", policy: "noeviction",
			reason: idempotency.ReasonUnsafeBackend,
		},
		"missing version": {
			info: "# Server\r\nredis_version:9.0.3\r\n", policy: "noeviction",
			reason: idempotency.ReasonUnsafeBackend,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := valkeymock.NewClient(gomock.NewController(t))
			client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
				valkeymock.Result(valkeymock.ValkeyBlobString(test.info)),
			)
			if test.reason == "" || test.info == "# Server\r\nvalkey_version:9.0.3\r\n" {
				client.EXPECT().Do(
					gomock.Any(),
					valkeymock.Match("CONFIG", "GET", "maxmemory-policy"),
				).Return(valkeymock.Result(valkeymock.ValkeyArray(
					valkeymock.ValkeyBlobString("maxmemory-policy"),
					valkeymock.ValkeyBlobString(test.policy),
				)))
			}

			err := (&nativeExecutor{client: client}).Check(context.Background())
			if test.reason == "" {
				if err != nil {
					t.Fatalf("Check() error = %v", err)
				}
				return
			}
			assertStoreReason(t, err, test.reason)
		})
	}
}

func TestNativeCheckPropagatesInspectionFailures(t *testing.T) {
	t.Parallel()

	t.Run("info", func(t *testing.T) {
		t.Parallel()
		backendErr := errors.New("connection lost")
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
		err := (&nativeExecutor{client: client}).Check(context.Background())
		if !errors.Is(err, backendErr) {
			t.Fatalf("Check() error = %v", err)
		}
	})

	t.Run("config", func(t *testing.T) {
		t.Parallel()
		backendErr := errors.New("CONFIG denied")
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
			valkeymock.Result(valkeymock.ValkeyBlobString("valkey_version:9.0.3\r\n")),
		)
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
		err := (&nativeExecutor{client: client}).Check(context.Background())
		if !errors.Is(err, backendErr) {
			t.Fatalf("Check() error = %v", err)
		}
	})
}

func TestOpenReturnsOnlyCheckedStores(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("valkey_version:9.0.3\r\n")),
	)
	client.EXPECT().Do(
		gomock.Any(),
		valkeymock.Match("CONFIG", "GET", "maxmemory-policy"),
	).Return(valkeymock.Result(valkeymock.ValkeyArray(
		valkeymock.ValkeyBlobString("maxmemory-policy"),
		valkeymock.ValkeyBlobString("noeviction"),
	)))

	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency", Retention: time.Hour,
		OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil || store == nil {
		t.Fatalf("Open() = %#v, %v", store, err)
	}
}

func TestOpenRejectsInvalidOptionsBeforeInspection(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	_, err := Open(context.Background(), client, Options{})
	assertStoreReason(t, err, idempotency.ReasonInvalidConfiguration)
}

func TestOpenRejectsUnsafeInspectedBackend(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("valkey_version:8.1.0\r\n")),
	)
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency", Retention: time.Hour,
		OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if store != nil {
		t.Fatalf("Open() store = %#v", store)
	}
	assertStoreReason(t, err, idempotency.ReasonUnsafeBackend)
}
