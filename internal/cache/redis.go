package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is the Cache the service runs on.
//
// A cache is the one dependency a request should survive losing. Every method
// here returns its error, and the caller treats an error as a miss rather
// than as a failed request: a Redis outage should make the app slow, not
// broken. That decision is in the handler, where it is visible, and not
// swallowed here.
type Redis struct {
	client *redis.Client
}

func NewRedis(addr string) (*Redis, error) {
	opts, err := redis.ParseURL(addr)
	if err != nil {
		// Allow a bare host:port as well as a URL, because that is what
		// people put in an environment variable.
		opts = &redis.Options{Addr: addr}
	}
	// Short timeouts on purpose. A cache that takes 500 ms to answer is worse
	// than no cache: the request pays the wait and then does the work anyway.
	opts.DialTimeout = 500 * time.Millisecond
	opts.ReadTimeout = 150 * time.Millisecond
	opts.WriteTimeout = 150 * time.Millisecond

	c := redis.NewClient(opts)
	return &Redis{client: c}, nil
}

func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache get: %w", err)
	}
	return b, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

func (r *Redis) Close() error { return r.client.Close() }

var _ Cache = (*Redis)(nil)
var _ Cache = (*Memory)(nil)
