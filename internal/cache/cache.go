// Package cache stores shaped responses.
package cache

import (
	"context"
	"sync"
	"time"
)

// Cache holds bytes under a key for a while.
//
// Bytes and not a struct. The value stored is the response exactly as it will
// be written, so a hit costs one copy and no encoding, and the thing in the
// cache is the thing that was measured against the size budget.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Close() error
}

// Memory is an in process Cache for local runs and for the tests.
//
// Expiry is checked on read rather than swept by a goroutine. A sweeper is
// the right choice when memory has to be reclaimed promptly and this is not
// that: entries are small, the TTL is minutes, and a background goroutine in
// a cache this size is a lifecycle to get wrong for no benefit.
type Memory struct {
	mu    sync.RWMutex
	items map[string]entry
	now   func() time.Time
}

type entry struct {
	value   []byte
	expires time.Time
}

func NewMemory() *Memory {
	return &Memory{items: map[string]entry{}, now: time.Now}
}

// SetClock replaces the clock so a TTL can expire without waiting.
func (m *Memory) SetClock(f func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = f
}

func (m *Memory) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.RLock()
	e, ok := m.items[key]
	now := m.now()
	m.mu.RUnlock()

	if !ok || now.After(e.expires) {
		return nil, false, nil
	}
	// A copy, so a caller that writes to what it was given cannot corrupt
	// the entry for everybody behind it.
	out := make([]byte, len(e.value))
	copy(out, e.value)
	return out, true, nil
}

func (m *Memory) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cp := make([]byte, len(value))
	copy(cp, value)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = entry{value: cp, expires: m.now().Add(ttl)}
	return nil
}

func (m *Memory) Close() error { return nil }

// Len is the number of entries held, expired or not. For the tests.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}
