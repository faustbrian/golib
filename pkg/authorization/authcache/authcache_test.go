package authcache

import (
	"context"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/backend/memory"
)

func TestManifestCodecRoundTripsStrictPolicyFormat(t *testing.T) {
	t.Parallel()

	manifest := cacheManifest(2)
	codec := ManifestCodec{MaxEncodedSize: 4096}
	encoded, err := codec.Encode(manifest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || decoded.Revision != 2 {
		t.Fatalf("Decode() = (%+v, %v)", decoded, err)
	}

	if _, err := codec.Encode(policy.Manifest{}); !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("Encode(invalid) error = %v", err)
	}
	tiny := ManifestCodec{MaxEncodedSize: 1}
	if _, err := tiny.Encode(manifest); !errors.Is(err, ErrManifestTooLarge) {
		t.Errorf("Encode(oversized) error = %v", err)
	}
	if _, err := tiny.Decode(encoded); !errors.Is(err, ErrManifestTooLarge) {
		t.Errorf("Decode(oversized) error = %v", err)
	}
	if _, err := codec.Decode([]byte(`{}`)); !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("Decode(invalid) error = %v", err)
	}
	if _, err := (ManifestCodec{}).Encode(manifest); err != nil {
		t.Errorf("default ManifestCodec.Encode() error = %v", err)
	}
}

func TestNewManifestCacheUsesIsolatedRevisionKeys(t *testing.T) {
	t.Parallel()

	clock := cache.SystemClock{}
	backend, err := memory.New(memory.Config{MaxEntries: 10, MaxBytes: 1 << 20, Clock: clock})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	manifestCache, err := New(Config{
		Namespace: "service", Backend: backend, Clock: clock,
		TTL: cache.TTLPolicy{TTL: time.Minute},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := manifestCache.Close(); err != nil {
			t.Fatalf("close manifest cache: %v", err)
		}
	})
	manifest := cacheManifest(3)
	if err := manifestCache.Set(context.Background(), 3, manifest); err != nil {
		t.Fatalf("Cache.Set() error = %v", err)
	}
	result, err := manifestCache.Get(context.Background(), 3)
	if err != nil || result.State != cache.Hit || result.Value.Revision != 3 {
		t.Fatalf("Cache.Get() = (%+v, %v)", result, err)
	}

	if _, err := New(Config{}); err == nil {
		t.Error("New(invalid config) error = nil")
	}
	if _, err := New(Config{Namespace: "INVALID", Backend: backend, Clock: clock, TTL: cache.TTLPolicy{TTL: time.Minute}}); err == nil {
		t.Error("New(invalid namespace) error = nil")
	}
}

func TestRevisionKeyEncoderAndRepositoryLoader(t *testing.T) {
	t.Parallel()

	encoder := RevisionKeyEncoder{}
	encoded, err := encoder.EncodeKey(42)
	if err != nil || string(encoded) != "42" {
		t.Fatalf("EncodeKey(42) = (%q, %v)", encoded, err)
	}
	if _, err := encoder.EncodeKey(0); !errors.Is(err, ErrInvalidRevision) {
		t.Errorf("EncodeKey(0) error = %v", err)
	}
	if _, err := RepositoryLoader(nil); !errors.Is(err, ErrNilRepository) {
		t.Errorf("RepositoryLoader(nil) error = %v", err)
	}

	repository := &repositoryStub{manifest: cacheManifest(5)}
	loader, err := RepositoryLoader(repository)
	if err != nil {
		t.Fatalf("RepositoryLoader() error = %v", err)
	}
	loaded, err := loader(context.Background(), 5)
	if err != nil || !loaded.Found || loaded.Value.Revision != 5 {
		t.Fatalf("loader(5) = (%+v, %v)", loaded, err)
	}
	loaded, err = loader(context.Background(), 4)
	if err != nil || loaded.Found {
		t.Fatalf("loader(4) = (%+v, %v), want miss", loaded, err)
	}
	repository.err = errors.New("repository failed")
	if _, err := loader(context.Background(), 5); !errors.Is(err, repository.err) {
		t.Errorf("loader failure = %v", err)
	}
}

type repositoryStub struct {
	manifest policy.Manifest
	err      error
}

func (repository *repositoryStub) Load(context.Context) (policy.Manifest, error) {
	return repository.manifest, repository.err
}

func (*repositoryStub) Update(context.Context, authorization.Revision, policy.Manifest) (policy.Manifest, error) {
	return policy.Manifest{}, errors.New("not implemented")
}

func cacheManifest(revision authorization.Revision) policy.Manifest {
	return policy.Manifest{
		Format: policy.FormatV1, Revision: revision,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies:  []policy.Record{},
	}
}
