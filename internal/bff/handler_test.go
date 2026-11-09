package bff_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/usmanvirk/mobile-bff/internal/bff"
	"github.com/usmanvirk/mobile-bff/internal/cache"
	"github.com/usmanvirk/mobile-bff/internal/shape"
	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func server(t *testing.T) (*http.ServeMux, *upstream.Fake, *cache.Memory) {
	t.Helper()
	cat := upstream.NewFake(60)
	c := cache.NewMemory()
	h := &bff.Handler{Catalogue: cat, Cache: c, Log: quiet(), EnforceBudget: true}
	return h.Routes(), cat, c
}

func get(t *testing.T, mux *http.ServeMux, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestListReturnsCards(t *testing.T) {
	mux, _, _ := server(t)
	rec := get(t, mux, "/v1/list?q=iphone", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var cards []shape.Card
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(cards) != bff.DefaultLimit {
		t.Fatalf("%d cards, want %d", len(cards), bff.DefaultLimit)
	}
	if cards[0].Image == "" || cards[0].Title == "" {
		t.Fatalf("a card is missing the parts a card needs: %+v", cards[0])
	}
}

// TestTheResponseFitsTheBudgetThroughTheHandler. The budget tests in the
// shape package measure the projection. This measures what actually goes out
// of the socket, which is the number that matters.
func TestTheResponseFitsTheBudgetThroughTheHandler(t *testing.T) {
	mux, _, _ := server(t)
	rec := get(t, mux, "/v1/list?q=", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if err := shape.Check(shape.ScreenList, rec.Body.Len(), bff.DefaultLimit); err != nil {
		t.Fatal(err)
	}
	t.Logf("list response on the wire is %d bytes", rec.Body.Len())
}

func TestSecondRequestIsServedFromCache(t *testing.T) {
	mux, cat, _ := server(t)

	first := get(t, mux, "/v1/list?q=pixel", nil)
	if first.Header().Get("X-Cache") != "miss" {
		t.Fatalf("the first request reported %s", first.Header().Get("X-Cache"))
	}
	after := cat.Calls()

	second := get(t, mux, "/v1/list?q=pixel", nil)
	if second.Header().Get("X-Cache") != "hit" {
		t.Fatalf("the second request reported %s", second.Header().Get("X-Cache"))
	}
	if cat.Calls() != after {
		t.Fatalf("the cached request still called upstream: %d then %d", after, cat.Calls())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatal("the cached body differs from the one that was cached")
	}
}

// TestTwoPhonesInOneBucketShareAnEntry is the reason buckets exist. Caching
// per exact device means the same page is stored a hundred ways and hit by
// almost nobody.
func TestTwoPhonesInOneBucketShareAnEntry(t *testing.T) {
	mux, cat, _ := server(t)

	a := map[string]string{"X-Viewport-Width": "390", "X-Viewport-Scale": "2"}
	b := map[string]string{"X-Viewport-Width": "393", "X-Viewport-Scale": "2"}

	get(t, mux, "/v1/list?q=x", a)
	calls := cat.Calls()
	rec := get(t, mux, "/v1/list?q=x", b)

	if rec.Header().Get("X-Cache") != "hit" {
		t.Fatalf("a 393pt phone missed an entry cached for a 390pt phone")
	}
	if cat.Calls() != calls {
		t.Fatal("the second phone caused another upstream call")
	}
}

// TestADifferentBucketDoesNotShareAnEntry is the other half. A tablet must
// not be served the image chosen for a small phone.
func TestADifferentBucketDoesNotShareAnEntry(t *testing.T) {
	mux, _, _ := server(t)

	small := map[string]string{"X-Viewport-Width": "320", "X-Viewport-Scale": "1"}
	large := map[string]string{"X-Viewport-Width": "430", "X-Viewport-Scale": "3"}

	first := get(t, mux, "/v1/list?q=y", small)
	second := get(t, mux, "/v1/list?q=y", large)

	if second.Header().Get("X-Cache") == "hit" {
		t.Fatal("a large tablet was served the entry cached for a small phone")
	}
	if first.Body.String() == second.Body.String() {
		t.Fatal("a small phone and a large one got identical image URLs, so the bucket is doing nothing")
	}
}

// TestFormatSupportIsInTheKey. Serving AVIF URLs to a client that cannot
// decode them shows up as missing images for a fraction of users and nothing
// in the logs.
func TestFormatSupportIsInTheCacheKey(t *testing.T) {
	mux, _, _ := server(t)

	modern := map[string]string{"X-Viewport-Width": "390", "X-Viewport-Scale": "2", "Accept": "image/avif,image/webp,*/*"}
	old := map[string]string{"X-Viewport-Width": "390", "X-Viewport-Scale": "2", "Accept": "*/*"}

	a := get(t, mux, "/v1/list?q=z", modern)
	b := get(t, mux, "/v1/list?q=z", old)

	if b.Header().Get("X-Cache") == "hit" {
		t.Fatal("a client that cannot decode avif was served the avif entry")
	}
	if a.Body.String() == b.Body.String() {
		t.Fatal("the two clients got identical URLs, so the format preference is not applied")
	}
}

func TestDifferentQueriesDoNotShareAnEntry(t *testing.T) {
	mux, _, _ := server(t)
	get(t, mux, "/v1/list?q=one", nil)
	if rec := get(t, mux, "/v1/list?q=two", nil); rec.Header().Get("X-Cache") == "hit" {
		t.Fatal("a different query hit the first query's entry")
	}
}

// TestACacheOutageIsNotAnOutage. Redis being down should make the app slow,
// not broken.
func TestACacheOutageIsNotAnOutage(t *testing.T) {
	cat := upstream.NewFake(30)
	h := &bff.Handler{Catalogue: cat, Cache: brokenCache{}, Log: quiet(), EnforceBudget: true}
	mux := h.Routes()

	rec := get(t, mux, "/v1/list?q=anything", nil)
	if rec.Code != 200 {
		t.Fatalf("a broken cache produced status %d: %s", rec.Code, rec.Body)
	}
	var cards []shape.Card
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil || len(cards) == 0 {
		t.Fatalf("a broken cache produced no usable body: %v", err)
	}
}

type brokenCache struct{}

func (brokenCache) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, errors.New("redis is down")
}
func (brokenCache) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("redis is down")
}
func (brokenCache) Close() error { return nil }

func TestNoCacheConfiguredStillWorks(t *testing.T) {
	cat := upstream.NewFake(30)
	h := &bff.Handler{Catalogue: cat, Log: quiet(), EnforceBudget: true}
	if rec := get(t, h.Routes(), "/v1/list?q=a", nil); rec.Code != 200 {
		t.Fatalf("status %d with no cache configured", rec.Code)
	}
}

func TestDetail(t *testing.T) {
	mux, _, _ := server(t)
	rec := get(t, mux, "/v1/listings/lst_000100000", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var d shape.Detail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Description == "" || len(d.Images) == 0 || len(d.Attributes) == 0 {
		t.Fatalf("the detail screen is missing something it shows: %+v", d)
	}
	if d.Seller.Name == "" {
		t.Fatal("no seller on the detail screen, where it is shown")
	}
}

func TestDetailOfSomethingThatIsNotThere(t *testing.T) {
	mux, _, _ := server(t)
	if rec := get(t, mux, "/v1/listings/lst_nope", nil); rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestUpstreamFailureIsABadGateway(t *testing.T) {
	cat := upstream.NewFake(10)
	cat.Fail = errors.New("catalogue exploded")
	h := &bff.Handler{Catalogue: cat, Cache: cache.NewMemory(), Log: quiet()}
	if rec := get(t, h.Routes(), "/v1/list?q=a", nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

// TestASlowUpstreamIsAGatewayTimeout, and it is distinguished from a failure,
// because the app can retry one of them sensibly.
func TestASlowUpstreamIsAGatewayTimeout(t *testing.T) {
	cat := upstream.NewFake(10)
	cat.Delay = bff.UpstreamTimeout + 500*time.Millisecond
	h := &bff.Handler{Catalogue: cat, Cache: cache.NewMemory(), Log: quiet()}

	start := time.Now()
	rec := get(t, h.Routes(), "/v1/list?q=a", nil)
	elapsed := time.Since(start)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504", rec.Code)
	}
	if elapsed > bff.UpstreamTimeout+time.Second {
		t.Fatalf("the handler waited %s, past its own %s timeout", elapsed, bff.UpstreamTimeout)
	}
}

func TestLimit(t *testing.T) {
	mux, _, _ := server(t)

	for _, tc := range []struct {
		q    string
		want int
	}{
		{"limit=5", 5},
		{"limit=1", 1},
		{"limit=999", bff.MaxLimit}, // clamped, not refused
	} {
		rec := get(t, mux, "/v1/list?q=a&"+tc.q, nil)
		if rec.Code != 200 {
			t.Fatalf("%s gave status %d: %s", tc.q, rec.Code, rec.Body)
		}
		var cards []shape.Card
		if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
			t.Fatal(err)
		}
		if len(cards) != tc.want {
			t.Errorf("%s returned %d cards, want %d", tc.q, len(cards), tc.want)
		}
	}

	for _, bad := range []string{"limit=0", "limit=-3", "limit=many"} {
		if rec := get(t, mux, "/v1/list?q=a&"+bad, nil); rec.Code != 400 {
			t.Errorf("%s gave status %d, want 400", bad, rec.Code)
		}
	}
}

func TestEmptyResultIsAnEmptyArray(t *testing.T) {
	cat := upstream.NewFake(0)
	h := &bff.Handler{Catalogue: cat, Cache: cache.NewMemory(), Log: quiet()}
	rec := get(t, h.Routes(), "/v1/list?q=nothing", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Fatalf("an empty result is %q, want []", rec.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	mux, _, _ := server(t)
	if rec := get(t, mux, "/healthz", nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestContentLengthIsSet(t *testing.T) {
	mux, _, _ := server(t)
	rec := get(t, mux, "/v1/list?q=a", nil)
	if rec.Header().Get("Content-Length") == "" {
		t.Fatal("no Content-Length, so the client cannot show a download progress or size a buffer")
	}
}

func TestConcurrentRequests(t *testing.T) {
	mux, _, _ := server(t)
	var wg sync.WaitGroup
	errs := make(chan string, 64)

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := get(t, mux, fmt.Sprintf("/v1/list?q=q%d", i%4), nil)
			if rec.Code != 200 {
				errs <- fmt.Sprintf("status %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}
