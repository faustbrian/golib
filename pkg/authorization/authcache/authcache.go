// Package authcache provides explicit advisory cache adapters for portable
// policy manifests. Cached manifests never replace repository verification.
package authcache

import (
	"context"
	"errors"
	"strconv"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	cache "github.com/faustbrian/golib/pkg/cache"
)

const (
	defaultMaxEncodedSize = 1 << 20
	defaultMaxKeySize     = 256
)

var (
	ErrManifestTooLarge = errors.New("authorization cached manifest is too large")
	ErrInvalidRevision  = errors.New("authorization cached revision is invalid")
	ErrNilRepository    = errors.New("authorization cached repository is nil")
)

type ManifestCodec struct {
	MaxEncodedSize int
}

func (codec ManifestCodec) Encode(manifest policy.Manifest) ([]byte, error) {
	encoded, err := policy.Encode(manifest)
	if err != nil {
		return nil, err
	}
	if len(encoded) > codec.limit() {
		return nil, ErrManifestTooLarge
	}
	return encoded, nil
}

func (codec ManifestCodec) Decode(encoded []byte) (policy.Manifest, error) {
	if len(encoded) > codec.limit() {
		return policy.Manifest{}, ErrManifestTooLarge
	}
	return policy.Decode(encoded)
}

func (codec ManifestCodec) limit() int {
	if codec.MaxEncodedSize <= 0 {
		return defaultMaxEncodedSize
	}
	return codec.MaxEncodedSize
}

type RevisionKeyEncoder struct{}

func (RevisionKeyEncoder) EncodeKey(revision authorization.Revision) ([]byte, error) {
	if revision == 0 {
		return nil, ErrInvalidRevision
	}
	return []byte(strconv.FormatUint(uint64(revision), 10)), nil
}

type Config struct {
	Namespace  string
	Backend    cache.Backend
	TTL        cache.TTLPolicy
	Clock      cache.Clock
	Observer   cache.Observer
	MaxValue   int
	MaxBatch   int
	MaxKeySize int
}

func New(config Config) (*cache.Cache[authorization.Revision, policy.Manifest], error) {
	if config.MaxValue == 0 {
		config.MaxValue = defaultMaxEncodedSize
	}
	if config.MaxKeySize == 0 {
		config.MaxKeySize = defaultMaxKeySize
	}
	keys, err := cache.NewKeySpace(
		config.Namespace,
		"authorization-policy",
		1,
		RevisionKeyEncoder{},
		config.MaxKeySize,
	)
	if err != nil {
		return nil, err
	}
	return cache.New(cache.Config[authorization.Revision, policy.Manifest]{
		Backend:  config.Backend,
		Keys:     keys,
		Codec:    ManifestCodec{MaxEncodedSize: config.MaxValue},
		TTL:      config.TTL,
		Clock:    config.Clock,
		MaxValue: config.MaxValue,
		MaxBatch: config.MaxBatch,
		Observer: config.Observer,
	})
}

func RepositoryLoader(
	repository policy.Repository,
) (cache.Loader[authorization.Revision, policy.Manifest], error) {
	if repository == nil {
		return nil, ErrNilRepository
	}
	return func(
		ctx context.Context,
		revision authorization.Revision,
	) (cache.LoadResult[policy.Manifest], error) {
		manifest, err := repository.Load(ctx)
		if err != nil {
			return cache.LoadResult[policy.Manifest]{}, err
		}
		if manifest.Revision != revision {
			return cache.LoadResult[policy.Manifest]{}, nil
		}
		return cache.LoadResult[policy.Manifest]{Value: manifest, Found: true}, nil
	}, nil
}
