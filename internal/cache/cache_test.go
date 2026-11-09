package cache_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/usmanvirk/mobile-bff/internal/cache"
	"github.com/usmanvirk/mobile-bff/internal/cache/cachetest"
)

func prefix(t *testing.T) string {
	return fmt.Sprintf("test:%d:%s:", time.Now().UnixNano(), t.Name())
}

func TestMemoryConformance(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cache.Cache, string) {
		return cache.NewMemory(), prefix(t)
	})
}

// A real Redis or nothing. What is worth testing here is the client's
// handling of a missing key, binary values and expiry, and a mock of Redis
// would be a mock of exactly that.
func TestRedisConformance(t *testing.T) {
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		t.Skip("REDIS_URL is not set")
	}
	cachetest.Run(t, func(t *testing.T) (cache.Cache, string) {
		r, err := cache.NewRedis(addr)
		if err != nil {
			t.Fatalf("connecting: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := r.Ping(ctx); err != nil {
			t.Fatalf("pinging redis: %v", err)
		}
		t.Cleanup(func() { _ = r.Close() })
		return r, prefix(t)
	})
}

// TestMemoryExpiryIsCheckedOnRead uses the clock rather than waiting, which
// the shared suite cannot do because it has to work against Redis too.
func TestMemoryExpiryIsCheckedOnRead(t *testing.T) {
	m := cache.NewMemory()
	base := time.Now()
	m.SetClock(func() time.Time { return base })

	if err := m.Set(context.Background(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	m.SetClock(func() time.Time { return base.Add(59 * time.Second) })
	if _, hit, _ := m.Get(context.Background(), "k"); !hit {
		t.Fatal("the entry expired a second early")
	}
	m.SetClock(func() time.Time { return base.Add(61 * time.Second) })
	if _, hit, _ := m.Get(context.Background(), "k"); hit {
		t.Fatal("the entry outlived its ttl")
	}
}

// TestRedisTimeoutsAreShort. A cache that takes half a second to answer is
// worse than no cache, because the request pays the wait and then does the
// work anyway.
func TestRedisPointedAtNothingFailsFast(t *testing.T) {
	// 10.255.255.1 is in a range reserved for documentation and does not
	// answer, so the dial has to time out rather than be refused.
	r, err := cache.NewRedis("10.255.255.1:6379")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	start := time.Now()
	_, hit, err := r.Get(context.Background(), "k")
	elapsed := time.Since(start)

	if hit {
		t.Fatal("a cache that cannot be reached reported a hit")
	}
	if err == nil {
		t.Fatal("a cache that cannot be reached reported success")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("an unreachable cache took %s to say so, which every request would pay", elapsed)
	}
}
