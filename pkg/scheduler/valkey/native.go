// Package valkey provides atomic fenced leases backed by Valkey 9 or newer.
package valkey

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	valkeygo "github.com/valkey-io/valkey-go"
)

type nativeExecutor struct {
	client valkeygo.Client
}

// New constructs a Valkey lease store without performing server checks.
func New(client valkeygo.Client, prefix string) (*Store, error) {
	if client == nil {
		return nil, lease.ErrInvalid
	}
	return newStore(&nativeExecutor{client: client}, prefix)
}

// Open constructs a store and verifies the server version and eviction policy.
func Open(ctx context.Context, client valkeygo.Client, prefix string) (*Store, error) {
	store, err := New(client, prefix)
	if err != nil {
		return nil, err
	}
	if err := store.Check(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (executor *nativeExecutor) Exec(
	ctx context.Context,
	op operation,
	leaseKey string,
	counterKey string,
	args []string,
) ([]string, error) {
	script, ok := scripts[op]
	if !ok {
		return nil, errors.New("scheduler valkey: unknown operation")
	}
	values, err := script.Exec(ctx, executor.client, []string{leaseKey, counterKey}, args).ToArray()
	if err != nil {
		return nil, err
	}
	reply := make([]string, len(values))
	for index := range values {
		reply[index], err = values[index].ToString()
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}

func (executor *nativeExecutor) Check(ctx context.Context) error {
	info, err := executor.client.Do(ctx, executor.client.B().Info().Section("server").Build()).ToString()
	if err != nil {
		return err
	}
	major, err := valkeyMajor(info)
	if err != nil || major < 9 {
		return fmt.Errorf("scheduler valkey: version 9 or newer required: %w", err)
	}
	config, err := executor.client.Do(
		ctx,
		executor.client.B().ConfigGet().Parameter("maxmemory-policy").Build(),
	).AsStrMap()
	if err != nil {
		return err
	}
	if config["maxmemory-policy"] != "noeviction" {
		return errors.New("scheduler valkey: maxmemory-policy must be noeviction")
	}
	return nil
}

func valkeyMajor(info string) (int, error) {
	for _, line := range strings.Split(info, "\n") {
		version, found := strings.CutPrefix(strings.TrimSpace(line), "valkey_version:")
		if !found {
			continue
		}
		major, _, _ := strings.Cut(version, ".")
		return strconv.Atoi(major)
	}
	return 0, errors.New("version missing")
}
