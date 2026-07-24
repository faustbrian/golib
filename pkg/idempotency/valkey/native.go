package valkey

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/idempotency"
	valkeygo "github.com/valkey-io/valkey-go"
)

type nativeExecutor struct {
	client valkeygo.Client
}

// New constructs a store without checking server version or eviction policy.
// Production startup should normally use Open.
func New(client valkeygo.Client, options Options) (*Store, error) {
	if client == nil {
		return nil, configurationError("client")
	}
	return newStore(&nativeExecutor{client: client}, options)
}

// Open constructs a store and rejects unsafe Valkey versions or eviction policy.
func Open(ctx context.Context, client valkeygo.Client, options Options) (*Store, error) {
	store, err := New(client, options)
	if err != nil {
		return nil, err
	}
	if err := store.Check(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (e *nativeExecutor) Exec(ctx context.Context, operation operation, key string, args []string) ([]string, error) {
	script := nativeScripts[operation]
	if script == nil {
		return nil, errors.New("idempotency valkey: unknown script")
	}
	messages, err := script.Exec(ctx, e.client, []string{key}, args).ToArray()
	if err != nil {
		return nil, err
	}
	reply := make([]string, len(messages))
	for index := range messages {
		reply[index], err = messages[index].ToString()
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}

func (e *nativeExecutor) Check(ctx context.Context) error {
	info, err := e.client.Do(ctx, e.client.B().Info().Section("server").Build()).ToString()
	if err != nil {
		return err
	}
	major, err := valkeyMajor(info)
	if err != nil || major < 9 {
		return &idempotency.Error{
			Reason: idempotency.ReasonUnsafeBackend,
			Field:  "version",
			Cause:  err,
		}
	}
	config, err := e.client.Do(
		ctx,
		e.client.B().ConfigGet().Parameter("maxmemory-policy").Build(),
	).AsStrMap()
	if err != nil {
		return err
	}
	if config["maxmemory-policy"] != "noeviction" {
		return &idempotency.Error{
			Reason: idempotency.ReasonUnsafeBackend,
			Field:  "maxmemory_policy",
		}
	}
	return nil
}

func valkeyMajor(info string) (int, error) {
	for _, line := range strings.Split(info, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "valkey_version:")
		if !found {
			continue
		}
		major, _, _ := strings.Cut(value, ".")
		return strconv.Atoi(major)
	}
	return 0, errors.New("idempotency valkey: version missing")
}
