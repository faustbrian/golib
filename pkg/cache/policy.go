package cache

import "time"

// TTLPolicy defines the fresh and stale lifetime of positive records.
type TTLPolicy struct {
	TTL      time.Duration
	StaleFor time.Duration
	Sliding  bool
}

// Validate reports whether the TTL and stale window are well formed.
func (p TTLPolicy) Validate() error {
	if p.TTL <= 0 || p.StaleFor < 0 {
		return ErrInvalidTTL
	}
	return nil
}
