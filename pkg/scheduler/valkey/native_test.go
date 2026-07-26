package valkey

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestValkeyMajor(t *testing.T) {
	t.Parallel()

	major, err := valkeyMajor("# Server\r\nvalkey_version:9.1.0\r\n")
	if err != nil || major != 9 {
		t.Fatalf("valkeyMajor() = %d, %v", major, err)
	}
	if _, err := valkeyMajor("redis_version:9.0.0"); err == nil {
		t.Fatal("valkeyMajor() error = nil")
	}
	if _, err := valkeyMajor("valkey_version:future"); err == nil {
		t.Fatal("valkeyMajor(invalid) error = nil")
	}
}

func TestNewBuildsNativeStore(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	store, err := New(client, "scheduler")
	if err != nil || store == nil {
		t.Fatalf("New() = %#v, %v", store, err)
	}
}

func TestNativeExecutorDecodesAndRejectsReplies(t *testing.T) {
	t.Parallel()

	t.Run("array", func(t *testing.T) {
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
			valkeymock.ValkeyArray(valkeymock.ValkeyString("ok"), valkeymock.ValkeyBlobString("value")),
		))
		reply, err := (&nativeExecutor{client: client}).Exec(context.Background(), operationInspect, "lease", "counter", nil)
		if err != nil || len(reply) != 2 || reply[1] != "value" {
			t.Fatalf("Exec() = %#v, %v", reply, err)
		}
	})
	t.Run("backend", func(t *testing.T) {
		backend := errors.New("backend")
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backend))
		_, err := (&nativeExecutor{client: client}).Exec(context.Background(), operationInspect, "lease", "counter", nil)
		if !errors.Is(err, backend) {
			t.Fatalf("Exec() error = %v", err)
		}
	})
	t.Run("nested message", func(t *testing.T) {
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
			valkeymock.ValkeyArray(valkeymock.ValkeyArray(valkeymock.ValkeyString("nested"))),
		))
		if _, err := (&nativeExecutor{client: client}).Exec(context.Background(), operationInspect, "lease", "counter", nil); err == nil {
			t.Fatal("Exec() error = nil")
		}
	})
	t.Run("operation", func(t *testing.T) {
		client := valkeymock.NewClient(gomock.NewController(t))
		if _, err := (&nativeExecutor{client: client}).Exec(context.Background(), operation(255), "lease", "counter", nil); err == nil {
			t.Fatal("Exec(unknown) error = nil")
		}
	})
}

func TestNativeCheckAndOpen(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		info      string
		policy    string
		infoErr   error
		configErr error
		wantErr   bool
	}{
		"safe":           {info: "valkey_version:9.0.1\r\n", policy: "noeviction"},
		"old":            {info: "valkey_version:8.0.1\r\n", wantErr: true},
		"bad version":    {info: "valkey_version:future\r\n", wantErr: true},
		"eviction":       {info: "valkey_version:9.0.1\r\n", policy: "allkeys-lru", wantErr: true},
		"info failure":   {infoErr: errors.New("info"), wantErr: true},
		"config failure": {info: "valkey_version:9.0.1\r\n", configErr: errors.New("config"), wantErr: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := valkeymock.NewClient(gomock.NewController(t))
			if test.infoErr != nil {
				client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(test.infoErr))
			} else {
				client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
					valkeymock.Result(valkeymock.ValkeyBlobString(test.info)),
				)
				if test.info == "valkey_version:9.0.1\r\n" {
					if test.configErr != nil {
						client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(test.configErr))
					} else {
						client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(valkeymock.ValkeyArray(
							valkeymock.ValkeyBlobString("maxmemory-policy"), valkeymock.ValkeyBlobString(test.policy),
						)))
					}
				}
			}
			store, err := Open(context.Background(), client, "scheduler")
			if test.wantErr && err == nil {
				t.Fatal("Open() error = nil")
			}
			if !test.wantErr && (err != nil || store == nil) {
				t.Fatalf("Open() = %#v, %v", store, err)
			}
		})
	}
	if _, err := Open(context.Background(), nil, "scheduler"); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Open(nil) error = %v", err)
	}
}

func TestNewRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, "scheduler"); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("New(nil) error = %v, want ErrInvalid", err)
	}
}
