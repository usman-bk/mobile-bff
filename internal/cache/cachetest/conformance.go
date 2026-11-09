// Package cachetest is the contract every Cache has to satisfy.
package cachetest

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/usmanvirk/mobile-bff/internal/cache"
)

// Factory returns a cache and a unique key prefix, so one test cannot see
// another's entries.
type Factory func(t *testing.T) (cache.Cache, string)

func Run(t *testing.T, newCache Factory) {
	t.Helper()
	for _, tc := range []struct {
		name string
		fn   func(*testing.T, cache.Cache, string)
	}{
		{"miss on an unknown key", missOnUnknown},
		{"set then get", setThenGet},
		{"overwrite", overwrite},
		{"empty value is a hit", emptyValueIsAHit},
		{"keys do not collide", noCollision},
		{"binary safe", binarySafe},
		{"caller cannot corrupt the entry", callerCannotCorrupt},
		{"expired entry is a miss", expiry},
		{"cancelled context", cancelled},
		{"concurrent use", concurrent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, prefix := newCache(t)
			tc.fn(t, c, prefix)
		})
	}
}

func ctx() context.Context { return context.Background() }

func missOnUnknown(t *testing.T, c cache.Cache, p string) {
	v, hit, err := c.Get(ctx(), p+"nothing-here")
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatalf("a key never written reported a hit with %q", v)
	}
}

func setThenGet(t *testing.T, c cache.Cache, p string) {
	want := []byte(`{"cart":[{"sku":"S1","qty":2}]}`)
	if err := c.Set(ctx(), p+"k", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, hit, err := c.Get(ctx(), p+"k")
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("the value just written reported a miss")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func overwrite(t *testing.T, c cache.Cache, p string) {
	if err := c.Set(ctx(), p+"k", []byte("first"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx(), p+"k", []byte("second"), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, _, err := c.Get(ctx(), p+"k")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q after an overwrite", got)
	}
}

// emptyValueIsAHit separates "stored, and it is empty" from "not stored".
// A cache that cannot tell them apart cannot cache an empty search result,
// which is one of the commonest pages there is.
func emptyValueIsAHit(t *testing.T, c cache.Cache, p string) {
	if err := c.Set(ctx(), p+"empty", []byte{}, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, hit, err := c.Get(ctx(), p+"empty")
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("an empty value reported a miss, so an empty result can never be cached")
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want empty", got)
	}
}

func noCollision(t *testing.T, c cache.Cache, p string) {
	for i := 0; i < 20; i++ {
		if err := c.Set(ctx(), fmt.Sprintf("%sk%d", p, i), []byte(fmt.Sprintf("v%d", i)), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		got, hit, err := c.Get(ctx(), fmt.Sprintf("%sk%d", p, i))
		if err != nil {
			t.Fatal(err)
		}
		if !hit || string(got) != fmt.Sprintf("v%d", i) {
			t.Fatalf("key %d returned %q, hit=%v", i, got, hit)
		}
	}
}

func binarySafe(t *testing.T, c cache.Cache, p string) {
	want := []byte{0x00, 0xff, 0x0a, 0x0d, 0x1b, 'a', 0x00}
	if err := c.Set(ctx(), p+"bin", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, _, err := c.Get(ctx(), p+"bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x, want % x", got, want)
	}
}

// callerCannotCorrupt is specific to an in process cache and has to hold for
// both, or the two behave differently under exactly the bug that is hardest
// to find.
func callerCannotCorrupt(t *testing.T, c cache.Cache, p string) {
	original := []byte("hello")
	if err := c.Set(ctx(), p+"k", original, time.Minute); err != nil {
		t.Fatal(err)
	}
	original[0] = 'J' // the caller reuses its buffer

	got, _, err := c.Get(ctx(), p+"k")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("writing to the buffer after Set changed the entry to %q", got)
	}

	got[0] = 'X' // and a reader scribbles on what it was given
	again, _, err := c.Get(ctx(), p+"k")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != "hello" {
		t.Fatalf("a reader corrupted the entry for everybody behind it: %q", again)
	}
}

func expiry(t *testing.T, c cache.Cache, p string) {
	// A real TTL, short enough that the test stays quick and long enough
	// that a slow machine does not make it flap.
	if err := c.Set(ctx(), p+"short", []byte("x"), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := c.Get(ctx(), p+"short"); !hit {
		t.Fatal("the entry was gone immediately")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if _, hit, err := c.Get(ctx(), p+"short"); err != nil {
			t.Fatal(err)
		} else if !hit {
			return
		}
	}
	t.Fatal("the entry had not expired after five seconds")
}

func cancelled(t *testing.T, c cache.Cache, p string) {
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if _, hit, err := c.Get(dead, p+"k"); err == nil && hit {
		t.Fatal("a cancelled context returned a hit")
	}
}

func concurrent(t *testing.T, c cache.Cache, p string) {
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers*20)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				k := fmt.Sprintf("%sc%d-%d", p, w, i)
				if err := c.Set(ctx(), k, []byte(k), time.Minute); err != nil {
					errs <- err
					return
				}
				got, hit, err := c.Get(ctx(), k)
				if err != nil {
					errs <- err
					return
				}
				if !hit || string(got) != k {
					errs <- fmt.Errorf("%s came back as %q hit=%v", k, got, hit)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
